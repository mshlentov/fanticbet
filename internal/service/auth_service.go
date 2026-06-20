package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/repository"
	"fanticbet/internal/security"
)

// AuthTokens — результат выдачи access+refresh. RefreshToken (открытый) попадает
// в httpOnly-cookie силами handler'а, RefreshID нужен handler'у для logout,
// AccessToken возвращается в теле ответа.
type AuthTokens struct {
	AccessToken  string
	RefreshToken string // открытый токен; в БД лежит только его sha256
	RefreshID    int64
}

// AuthService инкапсулирует бизнес-логику аутентификации по email+паролю.
// Не знает про HTTP (gin, статусы, cookie) — только про domain-операции.
type AuthService struct {
	tx          TxRunner
	users       repository.UserRepository
	refresh     repository.RefreshTokenRepository
	wallets     repository.WalletRepository
	walletTx    repository.WalletTransactionRepository
	jwt         *security.JWTManager
	signupBonus int64
	accessTTL   time.Duration
	refreshTTL  time.Duration
}

// NewAuthService собирает сервис. signupBonus, accessTTL, refreshTTL приходят из
// конфига в main; jwt — уже готовый *security.JWTManager.
func NewAuthService(
	tx TxRunner,
	users repository.UserRepository,
	refresh repository.RefreshTokenRepository,
	wallets repository.WalletRepository,
	walletTx repository.WalletTransactionRepository,
	jwt *security.JWTManager,
	signupBonus int64,
	accessTTL, refreshTTL time.Duration,
) *AuthService {
	return &AuthService{
		tx:          tx,
		users:       users,
		refresh:     refresh,
		wallets:     wallets,
		walletTx:    walletTx,
		jwt:         jwt,
		signupBonus: signupBonus,
		accessTTL:   accessTTL,
		refreshTTL:  refreshTTL,
	}
}

// Register создаёт пользователя и его кошелёк с signup_bonus в одной транзакции.
// Шаги внутри tx: создать user → создать wallet → FOR UPDATE на кошельке →
// зачислить бонус → запись в wallet_transactions (balance_after = новый баланс).
// Токены выдаются уже после коммита: если транзакция откатилась, не должно
// остаться «живых» токенов для несуществующего пользователя.
func (s *AuthService) Register(ctx context.Context, email, password, displayName string) (AuthTokens, error) {
	// Хэшируем пароль до транзакции: bcrypt долгий, незачем держать tx открытой.
	passwordHash, err := security.HashPassword(password)
	if err != nil {
		return AuthTokens{}, fmt.Errorf("AuthService.Register: %w", err)
	}

	var userID int64
	err = s.tx.RunInTx(ctx, func(ctx context.Context) error {
		uid, err := s.users.Create(ctx, domain.User{
			Email:        &email,
			PasswordHash: &passwordHash,
			DisplayName:  displayName,
			Role:         domain.RoleUser,
		})
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		userID = uid

		if err := s.wallets.Create(ctx, userID); err != nil {
			return fmt.Errorf("create wallet user_id=%d: %w", userID, err)
		}

		// Блокируем строку кошелька перед изменением баланса — конвенция о
		// деньгах (SELECT ... FOR UPDATE обязателен).
		if _, err := s.wallets.GetByUserIDForUpdate(ctx, userID); err != nil {
			return fmt.Errorf("lock wallet user_id=%d: %w", userID, err)
		}

		// Зачисляем бонус. UpdateBalance атомарно меняет баланс и возвращает новый.
		newBalance, err := s.wallets.UpdateBalance(ctx, userID, s.signupBonus)
		if err != nil {
			return fmt.Errorf("signup bonus user_id=%d: %w", userID, err)
		}

		// Журнал движений: balance_after должен сходиться с реальным балансом —
		// это «защита от багов» (см. docs/architecture.md, раздел 3).
		if _, err := s.walletTx.Create(ctx, domain.WalletTransaction{
			UserID:       userID,
			Amount:       s.signupBonus,
			Type:         domain.TxSignupBonus,
			BalanceAfter: newBalance,
		}); err != nil {
			return fmt.Errorf("signup bonus tx user_id=%d: %w", userID, err)
		}
		return nil
	})
	if err != nil {
		// Дубликат email придёт как ErrConflict из репозитория — пробрасываем
		// как есть, handler отобразит 409.
		return AuthTokens{}, fmt.Errorf("AuthService.Register: %w", err)
	}

	// Токены — после успешного коммита. Если здесь упадёт, БД останется
	// консистентной (user создан, бонус начислен), клиент просто повторит логин.
	return s.issueTokens(ctx, userID, domain.RoleUser)
}

// Login проверяет учётные данные и выдаёт токены. Неважно, чего именно не хватает
// (нет пользователя / неверный пароль) — всегда ErrInvalidCredentials, чтобы не
// подсказывать атакующему (user enumeration).
func (s *AuthService) Login(ctx context.Context, email, password string) (AuthTokens, error) {
	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		// ErrNotFound → ErrInvalidCredentials. Прочие ошибки — наружу как есть.
		if errors.Is(err, domain.ErrNotFound) {
			return AuthTokens{}, fmt.Errorf("AuthService.Login: %w", domain.ErrInvalidCredentials)
		}
		return AuthTokens{}, fmt.Errorf("AuthService.Login: %w", err)
	}

	// У OAuth-only аккаунтов PasswordHash == nil — вход по паролю невозможен.
	if user.PasswordHash == nil {
		return AuthTokens{}, fmt.Errorf("AuthService.Login: %w", domain.ErrInvalidCredentials)
	}
	if err := security.CheckPassword(*user.PasswordHash, password); err != nil {
		return AuthTokens{}, fmt.Errorf("AuthService.Login: %w", domain.ErrInvalidCredentials)
	}

	// last_login_at — аудит, не бизнес-валидация: ошибка не должна валить вход.
	loginAt := s.now()
	if err := s.users.TouchLastLogin(ctx, user.ID, loginAt); err != nil {
		log.Printf("AuthService.Login: TouchLastLogin user_id=%d: %v", user.ID, err)
	}

	return s.issueTokens(ctx, user.ID, user.Role)
}

// Refresh проверяет refresh-токен и выдаёт новый access. Сам refresh НЕ ротируется
// (решение в плане): живёт свои refreshTTL дней, аннулируется только явным Logout.
// Поэтому в ответе RefreshToken == пришедший токен.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (AuthTokens, error) {
	hash := security.HashToken(refreshToken)

	stored, err := s.refresh.GetByHash(ctx, hash)
	if err != nil {
		// Несуществующий хэш трактуем как «токен не валиден» — детали не раскрываем.
		if errors.Is(err, domain.ErrNotFound) {
			return AuthTokens{}, fmt.Errorf("AuthService.Refresh: %w", domain.ErrInvalidCredentials)
		}
		return AuthTokens{}, fmt.Errorf("AuthService.Refresh: %w", err)
	}
	if stored.RevokedAt != nil {
		return AuthTokens{}, fmt.Errorf("AuthService.Refresh: %w", domain.ErrTokenRevoked)
	}
	if s.now().After(stored.ExpiresAt) {
		return AuthTokens{}, fmt.Errorf("AuthService.Refresh: %w", domain.ErrTokenExpired)
	}

	// Роль на момент refresh может отличаться от исходной при регистрации
	// (админ понизил/повысил пользователя). Чтобы access-JWT нёс актуальную
	// роль, перечитываем пользователя. БД-запрос здесь уместен: refresh —
	// редкая операция (раз в 15 мин), а рассинхрон прав критичнее лишнего SELECT.
	user, err := s.users.GetByID(ctx, stored.UserID)
	if err != nil {
		return AuthTokens{}, fmt.Errorf("AuthService.Refresh load user: %w", err)
	}

	access, err := s.jwt.Issue(user.ID, user.Role, s.now())
	if err != nil {
		return AuthTokens{}, fmt.Errorf("AuthService.Refresh issue access: %w", err)
	}

	return AuthTokens{
		AccessToken:  access,
		RefreshToken: refreshToken, // без ротации
		RefreshID:    stored.ID,
	}, nil
}

// Logout отзывает refresh-токен (помечает revoked_at = now()). Идемпотентен на
// уровне сервиса: повторный logout по тому же токену вернёт ErrNotFound (токен
// уже отозван и... фактически нет — Revoke перезаписывает timestamp). Сервис
// трактует ErrNotFound от Revoke как уже-отозванный и не ошибается.
func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	hash := security.HashToken(refreshToken)

	stored, err := s.refresh.GetByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// Токена нет — значит, уже не валиден. Идемпотентный logout.
			return nil
		}
		return fmt.Errorf("AuthService.Logout: %w", err)
	}

	if err := s.refresh.Revoke(ctx, stored.ID); err != nil {
		return fmt.Errorf("AuthService.Logout revoke id=%d: %w", stored.ID, err)
	}
	return nil
}

// issueTokens — общий хелпер для Register/Login: создаёт refresh-запись в БД и
// подписывает access-JWT. Не в транзакции: refresh-запись независима от остального.
func (s *AuthService) issueTokens(ctx context.Context, userID int64, role domain.Role) (AuthTokens, error) {
	plain, err := security.GenerateRefreshToken()
	if err != nil {
		return AuthTokens{}, fmt.Errorf("AuthService.issueTokens gen refresh: %w", err)
	}

	now := s.now()
	refreshID, err := s.refresh.Create(ctx, domain.RefreshToken{
		UserID:    userID,
		TokenHash: security.HashToken(plain),
		ExpiresAt: now.Add(s.refreshTTL),
	})
	if err != nil {
		return AuthTokens{}, fmt.Errorf("AuthService.issueTokens store refresh user_id=%d: %w", userID, err)
	}

	access, err := s.jwt.Issue(userID, role, now)
	if err != nil {
		return AuthTokens{}, fmt.Errorf("AuthService.issueTokens issue access user_id=%d: %w", userID, err)
	}

	return AuthTokens{
		AccessToken:  access,
		RefreshToken: plain,
		RefreshID:    refreshID,
	}, nil
}

// now обёрнут в метод, чтобы в тестах подменять время (фиксированный now).
func (s *AuthService) now() time.Time {
	return time.Now()
}
