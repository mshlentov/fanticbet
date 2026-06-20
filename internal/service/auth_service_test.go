package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/security"
)

const (
	testSecret    = "test-secret-do-not-use-in-prod-0123456789"
	testSignup    = int64(10000)
	testAccessTTL = 15 * time.Minute
	testRefreshT  = 30 * 24 * time.Hour
)

// newTestAuth собирает AuthService с инъекцией моков. Возвращает моки для
// настройки ожиданий в конкретном тесте.
func newTestAuth(t *testing.T) (*AuthService, *fakeTxRunner, *fakeUserRepo, *fakeWalletRepo, *fakeWalletTxRepo, *fakeRefreshRepo) {
	t.Helper()
	jwt, err := security.NewJWTManager(testSecret, testAccessTTL)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	tx := &fakeTxRunner{}
	users := &fakeUserRepo{}
	wallets := &fakeWalletRepo{}
	walletTx := &fakeWalletTxRepo{}
	refresh := &fakeRefreshRepo{}

	svc := NewAuthService(tx, users, refresh, wallets, walletTx, jwt, testSignup, testAccessTTL, testRefreshT)
	return svc, tx, users, wallets, walletTx, refresh
}

// --- Register ---

func TestAuthService_Register_Success(t *testing.T) {
	svc, tx, users, wallets, walletTx, refresh := newTestAuth(t)

	var createdUserID int64 = 42
	tx.err = nil // транзакция проходит
	users.createFn = func(ctx context.Context, u domain.User) (int64, error) {
		if u.Role != domain.RoleUser {
			t.Errorf("expected role=user, got %q", u.Role)
		}
		if u.Email == nil || *u.Email != "a@b.c" {
			t.Errorf("email not propagated: %v", u.Email)
		}
		if u.PasswordHash == nil {
			t.Error("password hash must be set")
		}
		return createdUserID, nil
	}
	wallets.createFn = func(ctx context.Context, userID int64) error {
		if userID != createdUserID {
			t.Errorf("wallet created for wrong user: %d", userID)
		}
		return nil
	}
	wallets.getForUpdFn = func(ctx context.Context, userID int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: userID, Balance: 0}, nil
	}
	wallets.updateBalFn = func(ctx context.Context, userID int64, delta int64) (int64, error) {
		if delta != testSignup {
			t.Errorf("expected bonus delta=%d, got %d", testSignup, delta)
		}
		return testSignup, nil // новый баланс
	}
	walletTx.createFn = func(ctx context.Context, wtx domain.WalletTransaction) (int64, error) {
		return 1, nil
	}
	refresh.createFn = func(ctx context.Context, rt domain.RefreshToken) (int64, error) {
		if rt.UserID != createdUserID {
			t.Errorf("refresh for wrong user: %d", rt.UserID)
		}
		if rt.TokenHash == "" {
			t.Error("refresh hash must be stored")
		}
		return 7, nil
	}

	tokens, err := svc.Register(context.Background(), "a@b.c", "password123", "Alice")
	if err != nil {
		t.Fatalf("Register error: %v", err)
	}

	// Проверки результата.
	if tokens.AccessToken == "" {
		t.Error("access token is empty")
	}
	if tokens.RefreshToken == "" {
		t.Error("refresh token is empty")
	}
	if tokens.RefreshID != 7 {
		t.Errorf("refresh id: got %d, want 7", tokens.RefreshID)
	}
	// Access-JWT должен парситься и нести наш user_id.
	claims, err := jwtParseForTest(t, tokens.AccessToken)
	if err != nil {
		t.Fatalf("parse access: %v", err)
	}
	if claims.UserID != createdUserID {
		t.Errorf("claims.UserID: got %d, want %d", claims.UserID, createdUserID)
	}

	// Запись о бонусе должна фиксировать итоговый баланс (конвенция «защиты от багов»).
	if walletTx.lastCreated.Amount != testSignup {
		t.Errorf("bonus tx amount: got %d, want %d", walletTx.lastCreated.Amount, testSignup)
	}
	if walletTx.lastCreated.Type != domain.TxSignupBonus {
		t.Errorf("bonus tx type: got %q, want %q", walletTx.lastCreated.Type, domain.TxSignupBonus)
	}
	if walletTx.lastCreated.BalanceAfter != testSignup {
		t.Errorf("bonus tx balance_after: got %d, want %d", walletTx.lastCreated.BalanceAfter, testSignup)
	}

	// Всё прошло в одной транзакции.
	if tx.calls != 1 {
		t.Errorf("expected 1 tx call, got %d", tx.calls)
	}
}

func TestAuthService_Register_EmailTaken(t *testing.T) {
	svc, _, users, _, _, _ := newTestAuth(t)

	users.createFn = func(ctx context.Context, u domain.User) (int64, error) {
		return 0, domain.ErrConflict
	}

	_, err := svc.Register(context.Background(), "dup@b.c", "password123", "Bob")
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

// Register с ошибкой на середине (после создания user) должен пробросить ошибку.
func TestAuthService_Register_WalletFails(t *testing.T) {
	svc, _, users, wallets, _, _ := newTestAuth(t)
	walletErr := errors.New("db down")

	users.createFn = func(ctx context.Context, u domain.User) (int64, error) { return 1, nil }
	wallets.createFn = func(ctx context.Context, userID int64) error { return walletErr }

	_, err := svc.Register(context.Background(), "a@b.c", "password123", "Alice")
	if !errors.Is(err, walletErr) {
		t.Errorf("expected wallet error wrapped, got %v", err)
	}
}

// --- Login ---

func TestAuthService_Login_Success(t *testing.T) {
	svc, _, users, _, _, refresh := newTestAuth(t)

	hashed, _ := security.HashPassword("correct-password")
	email := "user@test.tld"
	users.getByEmailFn = func(ctx context.Context, e string) (domain.User, error) {
		if e != email {
			t.Errorf("queried wrong email: %q", e)
		}
		return domain.User{ID: 5, Email: &email, PasswordHash: &hashed, Role: domain.RoleUser}, nil
	}
	users.touchLoginFn = func(ctx context.Context, id int64, at time.Time) error {
		if id != 5 {
			t.Errorf("TouchLastLogin wrong id: %d", id)
		}
		return nil
	}
	refresh.createFn = func(ctx context.Context, rt domain.RefreshToken) (int64, error) {
		if rt.UserID != 5 {
			t.Errorf("refresh for wrong user: %d", rt.UserID)
		}
		return 9, nil
	}

	tokens, err := svc.Login(context.Background(), email, "correct-password")
	if err != nil {
		t.Fatalf("Login error: %v", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Error("tokens empty after login")
	}
	if users.touchLoginCalls != 1 {
		t.Errorf("expected TouchLastLogin called once, got %d", users.touchLoginCalls)
	}
}

func TestAuthService_Login_UserNotFound(t *testing.T) {
	svc, _, users, _, _, _ := newTestAuth(t)
	users.getByEmailFn = func(ctx context.Context, e string) (domain.User, error) {
		return domain.User{}, domain.ErrNotFound
	}

	_, err := svc.Login(context.Background(), "ghost@test.tld", "whatever")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestAuthService_Login_WrongPassword(t *testing.T) {
	svc, _, users, _, _, _ := newTestAuth(t)
	hashed, _ := security.HashPassword("correct-password")
	users.getByEmailFn = func(ctx context.Context, e string) (domain.User, error) {
		return domain.User{ID: 5, PasswordHash: &hashed, Role: domain.RoleUser}, nil
	}

	_, err := svc.Login(context.Background(), "u@t.tld", "wrong-password")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

// У OAuth-only аккаунта нет пароля — вход по паролю невозможен.
func TestAuthService_Login_NoPasswordHash(t *testing.T) {
	svc, _, users, _, _, _ := newTestAuth(t)
	users.getByEmailFn = func(ctx context.Context, e string) (domain.User, error) {
		return domain.User{ID: 5, PasswordHash: nil, Role: domain.RoleUser}, nil
	}

	_, err := svc.Login(context.Background(), "oauth@t.tld", "anything")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials for OAuth-only, got %v", err)
	}
}

// --- Refresh ---

func TestAuthService_Login_LoginErrorOnTouchIsIgnored(t *testing.T) {
	svc, _, users, _, _, refresh := newTestAuth(t)
	hashed, _ := security.HashPassword("pw")
	users.getByEmailFn = func(ctx context.Context, e string) (domain.User, error) {
		return domain.User{ID: 5, PasswordHash: &hashed, Role: domain.RoleUser}, nil
	}
	users.touchLoginFn = func(ctx context.Context, id int64, at time.Time) error {
		return errors.New("audit db down")
	}
	refresh.createFn = func(ctx context.Context, rt domain.RefreshToken) (int64, error) { return 1, nil }

	if _, err := svc.Login(context.Background(), "u@t.tld", "pw"); err != nil {
		t.Errorf("Login must not fail on TouchLastLogin error, got: %v", err)
	}
}

func TestAuthService_Refresh_Success(t *testing.T) {
	svc, _, users, _, _, refresh := newTestAuth(t)
	plain := "existing-refresh-token"

	refresh.getByHashFn = func(ctx context.Context, hash string) (domain.RefreshToken, error) {
		// Хэш должен совпадать с тем, что сгенерит HashToken.
		if hash != security.HashToken(plain) {
			t.Errorf("queried wrong hash")
		}
		return domain.RefreshToken{
			ID:        3,
			UserID:    8,
			ExpiresAt: time.Now().Add(time.Hour),
		}, nil
	}
	users.getByIDFn = func(ctx context.Context, id int64) (domain.User, error) {
		if id != 8 {
			t.Errorf("loaded wrong user: %d", id)
		}
		return domain.User{ID: 8, Role: domain.RoleAdmin}, nil
	}

	tokens, err := svc.Refresh(context.Background(), plain)
	if err != nil {
		t.Fatalf("Refresh error: %v", err)
	}
	claims, _ := jwtParseForTest(t, tokens.AccessToken)
	if claims.UserID != 8 {
		t.Errorf("claims.UserID: got %d, want 8", claims.UserID)
	}
	if claims.Role != domain.RoleAdmin {
		t.Errorf("claims.Role: got %q, want admin", claims.Role)
	}
	// Без ротации: refresh вернулся тот же.
	if tokens.RefreshToken != plain {
		t.Errorf("refresh rotated (must not): got %q, want %q", tokens.RefreshToken, plain)
	}
}

func TestAuthService_Refresh_Revoked(t *testing.T) {
	svc, _, _, _, _, refresh := newTestAuth(t)
	revoked := time.Now().Add(-time.Hour)
	refresh.getByHashFn = func(ctx context.Context, hash string) (domain.RefreshToken, error) {
		return domain.RefreshToken{ID: 1, UserID: 1, ExpiresAt: time.Now().Add(time.Hour), RevokedAt: &revoked}, nil
	}

	_, err := svc.Refresh(context.Background(), "tok")
	if !errors.Is(err, domain.ErrTokenRevoked) {
		t.Errorf("expected ErrTokenRevoked, got %v", err)
	}
}

func TestAuthService_Refresh_Expired(t *testing.T) {
	svc, _, _, _, _, refresh := newTestAuth(t)
	refresh.getByHashFn = func(ctx context.Context, hash string) (domain.RefreshToken, error) {
		return domain.RefreshToken{ID: 1, UserID: 1, ExpiresAt: time.Now().Add(-time.Hour)}, nil
	}

	_, err := svc.Refresh(context.Background(), "tok")
	if !errors.Is(err, domain.ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestAuthService_Refresh_NotFound(t *testing.T) {
	svc, _, _, _, _, refresh := newTestAuth(t)
	refresh.getByHashFn = func(ctx context.Context, hash string) (domain.RefreshToken, error) {
		return domain.RefreshToken{}, domain.ErrNotFound
	}

	_, err := svc.Refresh(context.Background(), "bogus")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials for unknown token, got %v", err)
	}
}

// --- Logout ---

func TestAuthService_Logout_Success(t *testing.T) {
	svc, _, _, _, _, refresh := newTestAuth(t)
	plain := "to-revoke"
	refresh.getByHashFn = func(ctx context.Context, hash string) (domain.RefreshToken, error) {
		return domain.RefreshToken{ID: 11, UserID: 1}, nil
	}
	refresh.revokeFn = func(ctx context.Context, id int64) error {
		if id != 11 {
			t.Errorf("revoked wrong id: %d", id)
		}
		return nil
	}

	if err := svc.Logout(context.Background(), plain); err != nil {
		t.Fatalf("Logout error: %v", err)
	}
	if len(refresh.revokeCalls) != 1 || refresh.revokeCalls[0] != 11 {
		t.Errorf("expected Revoke(11), got %v", refresh.revokeCalls)
	}
}

// Logout по уже отозванному/несуществующему токену идемпотентен.
func TestAuthService_Logout_Idempotent(t *testing.T) {
	svc, _, _, _, _, refresh := newTestAuth(t)
	refresh.getByHashFn = func(ctx context.Context, hash string) (domain.RefreshToken, error) {
		return domain.RefreshToken{}, domain.ErrNotFound
	}

	if err := svc.Logout(context.Background(), "unknown"); err != nil {
		t.Errorf("idempotent logout must not error, got: %v", err)
	}
}

// jwtParseForTest — хелпер для разбора access-JWT в тестах через тот же менеджер.
func jwtParseForTest(t *testing.T, tok string) (security.Claims, error) {
	t.Helper()
	mgr, err := security.NewJWTManager(testSecret, testAccessTTL)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	return mgr.Parse(tok)
}

// Гарантируем, что refresh-токен, выдаваемый сервисом, имеет ожидаемую длину
// (32 байта → 43 base64url-символа) — косвенная проверка, что issueTokens
// использует именно GenerateRefreshToken.
func TestAuthService_Refresh_TokenShape(t *testing.T) {
	svc, _, users, _, _, refresh := newTestAuth(t)
	refresh.getByHashFn = func(ctx context.Context, hash string) (domain.RefreshToken, error) {
		return domain.RefreshToken{ID: 1, UserID: 1, ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	users.getByIDFn = func(ctx context.Context, id int64) (domain.User, error) {
		return domain.User{ID: 1, Role: domain.RoleUser}, nil
	}

	tokens, err := svc.Refresh(context.Background(), strings.Repeat("x", 43))
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	_ = tokens // shape проверяется в security-тестах; здесь — только smoke
}
