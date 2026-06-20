package service

import (
	"context"
	"time"

	"fanticbet/internal/domain"
)

// Моки репозиториев для тестов service-слоя. Намеренно простые: каждый метод —
// поле-функция, которое тест перекрывает под сценарий. Без mockgen — меньше
// зависимостей, а覆盖率 нужна небольшая (критичные сценарии).

// --- TxRunner mock ---

// fakeTxRunner выполняет fn сразу (без настоящей транзакции). Если fn вернул
// ошибку — пробрасывает. Так можно тестировать «транзакционную» логику сервиса
// без pgxpool.
type fakeTxRunner struct {
	err error // если задано — возвращается вместо вызова fn
	calls int
	lastFn func(ctx context.Context) error
}

func (m *fakeTxRunner) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	m.calls++
	m.lastFn = fn
	if m.err != nil {
		return m.err
	}
	return fn(ctx)
}

// --- UserRepository mock ---

type fakeUserRepo struct {
	createFn        func(ctx context.Context, u domain.User) (int64, error)
	getByIDFn       func(ctx context.Context, id int64) (domain.User, error)
	getByEmailFn    func(ctx context.Context, email string) (domain.User, error)
	updateFn        func(ctx context.Context, u domain.User) error
	touchLoginFn    func(ctx context.Context, id int64, at time.Time) error
	touchLoginCalls int
}

func (m *fakeUserRepo) Create(ctx context.Context, u domain.User) (int64, error) {
	return m.createFn(ctx, u)
}
func (m *fakeUserRepo) GetByID(ctx context.Context, id int64) (domain.User, error) {
	return m.getByIDFn(ctx, id)
}
func (m *fakeUserRepo) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	return m.getByEmailFn(ctx, email)
}
func (m *fakeUserRepo) Update(ctx context.Context, u domain.User) error {
	return m.updateFn(ctx, u)
}
func (m *fakeUserRepo) TouchLastLogin(ctx context.Context, id int64, at time.Time) error {
	m.touchLoginCalls++
	if m.touchLoginFn != nil {
		return m.touchLoginFn(ctx, id, at)
	}
	return nil
}

// --- WalletRepository mock ---

type fakeWalletRepo struct {
	createFn     func(ctx context.Context, userID int64) error
	getByUserFn  func(ctx context.Context, userID int64) (domain.Wallet, error)
	getForUpdFn  func(ctx context.Context, userID int64) (domain.Wallet, error)
	updateBalFn  func(ctx context.Context, userID int64, delta int64) (int64, error)
	updateBalArg int64 // последний переданный delta
}

func (m *fakeWalletRepo) Create(ctx context.Context, userID int64) error {
	return m.createFn(ctx, userID)
}
func (m *fakeWalletRepo) GetByUserID(ctx context.Context, userID int64) (domain.Wallet, error) {
	return m.getByUserFn(ctx, userID)
}
func (m *fakeWalletRepo) GetByUserIDForUpdate(ctx context.Context, userID int64) (domain.Wallet, error) {
	return m.getForUpdFn(ctx, userID)
}
func (m *fakeWalletRepo) UpdateBalance(ctx context.Context, userID int64, delta int64) (int64, error) {
	m.updateBalArg = delta
	return m.updateBalFn(ctx, userID, delta)
}

// --- WalletTransactionRepository mock ---

type fakeWalletTxRepo struct {
	createFn     func(ctx context.Context, tx domain.WalletTransaction) (int64, error)
	listByUserFn func(ctx context.Context, userID int64, page int) ([]domain.WalletTransaction, error)
	lastCreated  domain.WalletTransaction
}

func (m *fakeWalletTxRepo) Create(ctx context.Context, t domain.WalletTransaction) (int64, error) {
	m.lastCreated = t
	return m.createFn(ctx, t)
}
func (m *fakeWalletTxRepo) ListByUser(ctx context.Context, userID int64, page int) ([]domain.WalletTransaction, error) {
	if m.listByUserFn != nil {
		return m.listByUserFn(ctx, userID, page)
	}
	return nil, nil // не используется в большинстве тестов
}

// --- RefreshTokenRepository mock ---

type fakeRefreshRepo struct {
	createFn     func(ctx context.Context, t domain.RefreshToken) (int64, error)
	getByHashFn  func(ctx context.Context, hash string) (domain.RefreshToken, error)
	revokeFn     func(ctx context.Context, id int64) error
	revokeCalls  []int64 // все id, переданные в Revoke
}

func (m *fakeRefreshRepo) Create(ctx context.Context, t domain.RefreshToken) (int64, error) {
	return m.createFn(ctx, t)
}
func (m *fakeRefreshRepo) GetByHash(ctx context.Context, hash string) (domain.RefreshToken, error) {
	return m.getByHashFn(ctx, hash)
}
func (m *fakeRefreshRepo) Revoke(ctx context.Context, id int64) error {
	m.revokeCalls = append(m.revokeCalls, id)
	return m.revokeFn(ctx, id)
}
