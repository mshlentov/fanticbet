package service

import (
	"context"
	"errors"
	"testing"

	"fanticbet/internal/domain"
	"fanticbet/internal/security"
)

// newTestOAuth собирает OAuthService с инъекцией моков.
func newTestOAuth(t *testing.T) (*OAuthService, *fakeTxRunner, *fakeUserRepo, *fakeWalletRepo, *fakeWalletTxRepo, *fakeAuthIdentityRepo, *fakeRefreshRepo) {
	t.Helper()
	jwt, err := security.NewJWTManager(testSecret, testAccessTTL)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	tx := &fakeTxRunner{}
	users := &fakeUserRepo{}
	wallets := &fakeWalletRepo{}
	walletTx := &fakeWalletTxRepo{}
	identity := &fakeAuthIdentityRepo{}
	refresh := &fakeRefreshRepo{}

	svc := NewOAuthService(tx, users, wallets, walletTx, identity, refresh, jwt, testSignup, testAccessTTL, testRefreshT)
	return svc, tx, users, wallets, walletTx, identity, refresh
}

// email для тестов.
func strPtr(s string) *string { return &s }

// --- Сценарий 1: identity найдена → логин без регистрации ---

func TestOAuthService_LoginOrRegister_ExistingIdentity(t *testing.T) {
	svc, _, users, _, _, identity, refresh := newTestOAuth(t)

	identity.getByProviderFn = func(_ context.Context, p domain.Provider, id string) (domain.AuthIdentity, error) {
		return domain.AuthIdentity{ID: 1, UserID: 7, Provider: p, ProviderUserID: id}, nil
	}
	users.getByIDFn = func(_ context.Context, id int64) (domain.User, error) {
		return domain.User{ID: id, DisplayName: "Иван", Role: domain.RoleUser}, nil
	}
	refresh.createFn = func(_ context.Context, t domain.RefreshToken) (int64, error) { return 1, nil }

	info := OAuthUserInfo{ProviderUserID: "ya_123", Email: strPtr("ivan@yandex.ru"), DisplayName: "Иван"}
	tokens, err := svc.LoginOrRegister(context.Background(), domain.ProviderYandex, info)

	if err != nil {
		t.Fatalf("LoginOrRegister error: %v", err)
	}
	if tokens.AccessToken == "" {
		t.Error("expected access token")
	}
}

// --- Сценарий 2: identity не найдена, email совпал → привязка к существующему user ---

func TestOAuthService_LoginOrRegister_LinkByEmail(t *testing.T) {
	svc, _, users, _, _, identity, refresh := newTestOAuth(t)

	identity.getByProviderFn = func(_ context.Context, _ domain.Provider, _ string) (domain.AuthIdentity, error) {
		return domain.AuthIdentity{}, domain.ErrNotFound
	}
	users.getByEmailFn = func(_ context.Context, email string) (domain.User, error) {
		return domain.User{ID: 5, DisplayName: "Петров", Role: domain.RoleUser}, nil
	}
	var linkedIdentity domain.AuthIdentity
	identity.createFn = func(_ context.Context, a domain.AuthIdentity) (int64, error) {
		linkedIdentity = a
		return 2, nil
	}
	refresh.createFn = func(_ context.Context, t domain.RefreshToken) (int64, error) { return 1, nil }

	info := OAuthUserInfo{ProviderUserID: "vk_456", Email: strPtr("petrov@mail.ru"), DisplayName: "Петров"}
	tokens, err := svc.LoginOrRegister(context.Background(), domain.ProviderVK, info)

	if err != nil {
		t.Fatalf("LoginOrRegister error: %v", err)
	}
	if tokens.AccessToken == "" {
		t.Error("expected access token")
	}
	// Провайдер привязан к найденному user.
	if linkedIdentity.UserID != 5 {
		t.Errorf("linked to wrong user: got %d, want 5", linkedIdentity.UserID)
	}
	if linkedIdentity.Provider != domain.ProviderVK {
		t.Errorf("wrong provider: got %s", linkedIdentity.Provider)
	}
}

// --- Сценарий 3: новый пользователь → регистрация с бонусом ---

func TestOAuthService_LoginOrRegister_Register(t *testing.T) {
	svc, tx, users, wallets, walletTx, identity, refresh := newTestOAuth(t)

	identity.getByProviderFn = func(_ context.Context, _ domain.Provider, _ string) (domain.AuthIdentity, error) {
		return domain.AuthIdentity{}, domain.ErrNotFound
	}
	// email не найден среди пользователей
	users.getByEmailFn = func(_ context.Context, _ string) (domain.User, error) {
		return domain.User{}, domain.ErrNotFound
	}

	var createdUser domain.User
	users.createFn = func(_ context.Context, u domain.User) (int64, error) {
		createdUser = u
		return 99, nil
	}
	wallets.createFn = func(_ context.Context, _ int64) error { return nil }
	wallets.getForUpdFn = func(_ context.Context, _ int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: 99, Balance: 0}, nil
	}
	wallets.updateBalFn = func(_ context.Context, _ int64, delta int64) (int64, error) {
		return delta, nil // новый баланс = signup_bonus
	}
	walletTx.createFn = func(_ context.Context, _ domain.WalletTransaction) (int64, error) { return 1, nil }
	refresh.createFn = func(_ context.Context, _ domain.RefreshToken) (int64, error) { return 1, nil }

	info := OAuthUserInfo{ProviderUserID: "ya_new", Email: strPtr("new@yandex.ru"), DisplayName: "Новый"}
	tokens, err := svc.LoginOrRegister(context.Background(), domain.ProviderYandex, info)

	if err != nil {
		t.Fatalf("LoginOrRegister error: %v", err)
	}
	if tokens.AccessToken == "" {
		t.Error("expected access token")
	}
	// Пользователь создан без PasswordHash (OAuth-only).
	if createdUser.PasswordHash != nil {
		t.Error("PasswordHash must be nil for OAuth user")
	}
	if createdUser.DisplayName != "Новый" {
		t.Errorf("display_name: got %q, want %q", createdUser.DisplayName, "Новый")
	}
	// Транзакция вызвана ровно один раз.
	if tx.calls != 1 {
		t.Errorf("tx calls: got %d, want 1", tx.calls)
	}
	// auth_identity создана в рамках транзакции.
	if identity.lastCreated.ProviderUserID != "ya_new" {
		t.Errorf("identity not created: %+v", identity.lastCreated)
	}
}

// --- Сценарий 3 без email (VK не дал email) → регистрация без email ---

func TestOAuthService_LoginOrRegister_Register_NoEmail(t *testing.T) {
	svc, _, users, wallets, walletTx, identity, refresh := newTestOAuth(t)

	identity.getByProviderFn = func(_ context.Context, _ domain.Provider, _ string) (domain.AuthIdentity, error) {
		return domain.AuthIdentity{}, domain.ErrNotFound
	}
	// getByEmail не должен вызываться при nil-email
	users.getByEmailFn = func(_ context.Context, _ string) (domain.User, error) {
		return domain.User{}, errors.New("getByEmail must not be called when email is nil")
	}
	users.createFn = func(_ context.Context, u domain.User) (int64, error) {
		if u.Email != nil {
			return 0, errors.New("email must be nil")
		}
		return 77, nil
	}
	wallets.createFn = func(_ context.Context, _ int64) error { return nil }
	wallets.getForUpdFn = func(_ context.Context, _ int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: 77}, nil
	}
	wallets.updateBalFn = func(_ context.Context, _ int64, d int64) (int64, error) { return d, nil }
	walletTx.createFn = func(_ context.Context, _ domain.WalletTransaction) (int64, error) { return 1, nil }
	refresh.createFn = func(_ context.Context, _ domain.RefreshToken) (int64, error) { return 1, nil }

	info := OAuthUserInfo{ProviderUserID: "vk_noemail", Email: nil, DisplayName: "VK User"}
	tokens, err := svc.LoginOrRegister(context.Background(), domain.ProviderVK, info)

	if err != nil {
		t.Fatalf("LoginOrRegister error: %v", err)
	}
	if tokens.AccessToken == "" {
		t.Error("expected access token")
	}
}
