package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/repository"
	"fanticbet/internal/security"
)

// OAuthUserInfo — данные пользователя, полученные от OAuth-провайдера.
// Email может быть nil: не все провайдеры возвращают его (VK — по желанию
// пользователя, Яндекс — только если есть хотя бы один привязанный адрес).
type OAuthUserInfo struct {
	ProviderUserID string
	Email          *string // nil → регистрируем без email (OAuth-only аккаунт)
	DisplayName    string
}

// OAuthService — бизнес-логика OAuth-callback. Не знает про HTTP, конкретных
// провайдеров и golang.org/x/oauth2 — это зона handler'а. Здесь только три
// сценария принятия решения и выдача токенов.
type OAuthService struct {
	tx           TxRunner
	users        repository.UserRepository
	wallets      repository.WalletRepository
	walletTx     repository.WalletTransactionRepository
	authIdentity repository.AuthIdentityRepository
	refresh      repository.RefreshTokenRepository
	jwt          *security.JWTManager
	signupBonus  int64
	accessTTL    time.Duration
	refreshTTL   time.Duration
}

func NewOAuthService(
	tx TxRunner,
	users repository.UserRepository,
	wallets repository.WalletRepository,
	walletTx repository.WalletTransactionRepository,
	authIdentity repository.AuthIdentityRepository,
	refresh repository.RefreshTokenRepository,
	jwt *security.JWTManager,
	signupBonus int64,
	accessTTL, refreshTTL time.Duration,
) *OAuthService {
	return &OAuthService{
		tx:           tx,
		users:        users,
		wallets:      wallets,
		walletTx:     walletTx,
		authIdentity: authIdentity,
		refresh:      refresh,
		jwt:          jwt,
		signupBonus:  signupBonus,
		accessTTL:    accessTTL,
		refreshTTL:   refreshTTL,
	}
}

// LoginOrRegister — центральная логика OAuth-callback. Три сценария:
//
//  1. auth_identity (provider, providerUserID) найдена → обычный логин.
//  2. identity не найдена, но email совпал с существующим user → привязываем
//     провайдера к аккаунту, логиним.
//  3. Ничего не найдено (или у провайдера нет email) → новая регистрация:
//     user + wallet + signup_bonus + auth_identity в одной транзакции.
func (s *OAuthService) LoginOrRegister(ctx context.Context, provider domain.Provider, info OAuthUserInfo) (AuthTokens, error) {
	// Сценарий 1: уже знакомый пользователь (ранее входил через этот провайдер).
	identity, err := s.authIdentity.GetByProvider(ctx, provider, info.ProviderUserID)
	if err == nil {
		user, err := s.users.GetByID(ctx, identity.UserID)
		if err != nil {
			return AuthTokens{}, fmt.Errorf("OAuthService.LoginOrRegister load user: %w", err)
		}
		return s.issueTokens(ctx, user.ID, user.Role)
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return AuthTokens{}, fmt.Errorf("OAuthService.LoginOrRegister get identity: %w", err)
	}

	// Сценарий 2: identity не найдена, но email провайдера совпадает с
	// существующим пользователем — привязываем провайдера к его аккаунту.
	if info.Email != nil {
		user, err := s.users.GetByEmail(ctx, *info.Email)
		if err == nil {
			if _, err := s.authIdentity.Create(ctx, domain.AuthIdentity{
				UserID:         user.ID,
				Provider:       provider,
				ProviderUserID: info.ProviderUserID,
			}); err != nil {
				return AuthTokens{}, fmt.Errorf("OAuthService.LoginOrRegister link identity: %w", err)
			}
			return s.issueTokens(ctx, user.ID, user.Role)
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return AuthTokens{}, fmt.Errorf("OAuthService.LoginOrRegister lookup email: %w", err)
		}
	}

	// Сценарий 3: совсем новый пользователь — регистрация с бонусом.
	// Всё в одной транзакции: user → wallet → FOR UPDATE → бонус → tx-запись
	// → auth_identity. Если что-то упадёт — откат без «висячих» данных.
	var userID int64
	err = s.tx.RunInTx(ctx, func(ctx context.Context) error {
		uid, err := s.users.Create(ctx, domain.User{
			Email:       info.Email, // nil для OAuth-only аккаунтов
			DisplayName: info.DisplayName,
			Role:        domain.RoleUser,
		})
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		userID = uid

		if err := s.wallets.Create(ctx, userID); err != nil {
			return fmt.Errorf("create wallet user_id=%d: %w", userID, err)
		}

		// SELECT ... FOR UPDATE перед изменением баланса — требование conventions.md.
		if _, err := s.wallets.GetByUserIDForUpdate(ctx, userID); err != nil {
			return fmt.Errorf("lock wallet user_id=%d: %w", userID, err)
		}

		newBalance, err := s.wallets.UpdateBalance(ctx, userID, s.signupBonus)
		if err != nil {
			return fmt.Errorf("signup bonus user_id=%d: %w", userID, err)
		}

		if _, err := s.walletTx.Create(ctx, domain.WalletTransaction{
			UserID:       userID,
			Amount:       s.signupBonus,
			Type:         domain.TxSignupBonus,
			BalanceAfter: newBalance,
		}); err != nil {
			return fmt.Errorf("signup bonus tx user_id=%d: %w", userID, err)
		}

		if _, err := s.authIdentity.Create(ctx, domain.AuthIdentity{
			UserID:         userID,
			Provider:       provider,
			ProviderUserID: info.ProviderUserID,
		}); err != nil {
			return fmt.Errorf("create identity user_id=%d: %w", userID, err)
		}

		return nil
	})
	if err != nil {
		return AuthTokens{}, fmt.Errorf("OAuthService.LoginOrRegister register: %w", err)
	}

	return s.issueTokens(ctx, userID, domain.RoleUser)
}

// issueTokens — выдача access+refresh. Намеренно дублирует AuthService.issueTokens:
// оба сервиса независимы по дизайну, чтобы изменения в email-логине не влияли
// на OAuth-логин и наоборот.
func (s *OAuthService) issueTokens(ctx context.Context, userID int64, role domain.Role) (AuthTokens, error) {
	plain, err := security.GenerateRefreshToken()
	if err != nil {
		return AuthTokens{}, fmt.Errorf("OAuthService.issueTokens gen refresh: %w", err)
	}

	now := time.Now()
	refreshID, err := s.refresh.Create(ctx, domain.RefreshToken{
		UserID:    userID,
		TokenHash: security.HashToken(plain),
		ExpiresAt: now.Add(s.refreshTTL),
	})
	if err != nil {
		return AuthTokens{}, fmt.Errorf("OAuthService.issueTokens store refresh user_id=%d: %w", userID, err)
	}

	access, err := s.jwt.Issue(userID, role, now)
	if err != nil {
		return AuthTokens{}, fmt.Errorf("OAuthService.issueTokens issue access user_id=%d: %w", userID, err)
	}

	return AuthTokens{
		AccessToken:  access,
		RefreshToken: plain,
		RefreshID:    refreshID,
	}, nil
}
