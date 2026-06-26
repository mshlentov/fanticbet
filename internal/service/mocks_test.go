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
	err    error // если задано — возвращается вместо вызова fn
	calls  int
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

// balanceUpdate — запись одного изменения баланса. balanceUpdates группирует
// вызовы по пользователю: тесты settlement проверяют суммы выплат/возвратов
// сразу по нескольким ставкам.
type balanceUpdate struct {
	Delta int64
}

type fakeWalletRepo struct {
	createFn      func(ctx context.Context, userID int64) error
	getByUserFn   func(ctx context.Context, userID int64) (domain.Wallet, error)
	getForUpdFn   func(ctx context.Context, userID int64) (domain.Wallet, error)
	updateBalFn   func(ctx context.Context, userID int64, delta int64) (int64, error)
	updateBalArg  int64                     // последний переданный delta (обратная совместимость)
	balanceByUser map[int64][]balanceUpdate // все изменения баланса по пользователю
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
	if m.balanceByUser == nil {
		m.balanceByUser = map[int64][]balanceUpdate{}
	}
	m.balanceByUser[userID] = append(m.balanceByUser[userID], balanceUpdate{Delta: delta})
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

// --- AuthIdentityRepository mock ---

type fakeAuthIdentityRepo struct {
	getByProviderFn func(ctx context.Context, provider domain.Provider, providerUserID string) (domain.AuthIdentity, error)
	createFn        func(ctx context.Context, identity domain.AuthIdentity) (int64, error)
	lastCreated     domain.AuthIdentity
}

func (m *fakeAuthIdentityRepo) GetByProvider(ctx context.Context, provider domain.Provider, providerUserID string) (domain.AuthIdentity, error) {
	return m.getByProviderFn(ctx, provider, providerUserID)
}

func (m *fakeAuthIdentityRepo) Create(ctx context.Context, identity domain.AuthIdentity) (int64, error) {
	m.lastCreated = identity
	if m.createFn != nil {
		return m.createFn(ctx, identity)
	}
	return 1, nil
}

// --- RefreshTokenRepository mock ---

type fakeRefreshRepo struct {
	createFn    func(ctx context.Context, t domain.RefreshToken) (int64, error)
	getByHashFn func(ctx context.Context, hash string) (domain.RefreshToken, error)
	revokeFn    func(ctx context.Context, id int64) error
	revokeCalls []int64 // все id, переданные в Revoke
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

// --- BetRepository mock ---

// settledCall — запись одного вызова UpdateStatusSettled: id ставки, итоговый
// статус и момент расчёта. Нужен тестам settlement для проверки, что каждая
// pending-ставка получила правильный статус.
type settledCall struct {
	ID        int64
	Status    domain.BetStatus
	SettledAt time.Time
}

type fakeBetRepo struct {
	createFn        func(ctx context.Context, b domain.Bet) (int64, error)
	getByIDFn       func(ctx context.Context, id int64) (domain.Bet, error)
	listByUserFn    func(ctx context.Context, userID int64, status domain.BetStatus, page int) ([]domain.BetWithDetails, error)
	listPendingFn   func(ctx context.Context, outcomeIDs []int64) ([]domain.Bet, error)
	updateSettledFn func(ctx context.Context, id int64, status domain.BetStatus, settledAt time.Time) error
	lastCreated     domain.Bet
	settledCalls    []settledCall // все вызовы UpdateStatusSettled (для тестов settlement)
}

func (m *fakeBetRepo) Create(ctx context.Context, b domain.Bet) (int64, error) {
	m.lastCreated = b
	return m.createFn(ctx, b)
}
func (m *fakeBetRepo) GetByID(ctx context.Context, id int64) (domain.Bet, error) {
	return m.getByIDFn(ctx, id)
}
func (m *fakeBetRepo) ListByUser(ctx context.Context, userID int64, status domain.BetStatus, page int) ([]domain.BetWithDetails, error) {
	return m.listByUserFn(ctx, userID, status, page)
}
func (m *fakeBetRepo) ListPendingByOutcomes(ctx context.Context, outcomeIDs []int64) ([]domain.Bet, error) {
	if m.listPendingFn != nil {
		return m.listPendingFn(ctx, outcomeIDs)
	}
	return nil, nil
}
func (m *fakeBetRepo) UpdateStatusSettled(ctx context.Context, id int64, status domain.BetStatus, settledAt time.Time) error {
	m.settledCalls = append(m.settledCalls, settledCall{ID: id, Status: status, SettledAt: settledAt})
	if m.updateSettledFn != nil {
		return m.updateSettledFn(ctx, id, status, settledAt)
	}
	return nil
}

// --- StatsRepository mock ---

// fakeStatsRepo — фейк аналитического репозитория. getUserStatsCalls и
// getLeaderboardCalls считают обращения к репозиторию: это позволяет тестам
// in-memory кэша StatsService проверять, что cache hit не доходит до БД.
type fakeStatsRepo struct {
	getUserStatsFn      func(ctx context.Context, userID int64) (domain.UserStats, error)
	getLeaderboardFn    func(ctx context.Context, f domain.LeaderboardFilter) ([]domain.LeaderboardRow, error)
	getUserStatsCalls   int
	getLeaderboardCalls int
	lastFilter          domain.LeaderboardFilter
}

func (m *fakeStatsRepo) GetUserStats(ctx context.Context, userID int64) (domain.UserStats, error) {
	m.getUserStatsCalls++
	return m.getUserStatsFn(ctx, userID)
}

func (m *fakeStatsRepo) GetLeaderboard(ctx context.Context, f domain.LeaderboardFilter) ([]domain.LeaderboardRow, error) {
	m.getLeaderboardCalls++
	m.lastFilter = f
	return m.getLeaderboardFn(ctx, f)
}
