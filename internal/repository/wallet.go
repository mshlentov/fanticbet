package repository

import (
	"context"
	"fmt"

	"fanticbet/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WalletRepository — интерфейс работы с кошельком. Все методы, меняющие
// баланс, должны вызываться внутри транзакции (см. GetByUserIDForUpdate).
type WalletRepository interface {
	// Create создаёт кошелёк с балансом 0 для нового пользователя.
	Create(ctx context.Context, userID int64) error
	// GetByUserID возвращает кошелёк без блокировки (чтение).
	GetByUserID(ctx context.Context, userID int64) (domain.Wallet, error)
	// GetByUserIDForUpdate читает кошелёк и блокирует строку до конца транзакции
	// (SELECT ... FOR UPDATE). Обязателен перед любым изменением баланса.
	GetByUserIDForUpdate(ctx context.Context, userID int64) (domain.Wallet, error)
	// UpdateBalance атомарно меняет баланс на delta и возвращает новый баланс.
	// Не должен вызываться без предварительного GetByUserIDForUpdate в той же tx.
	UpdateBalance(ctx context.Context, userID int64, delta int64) (int64, error)
}

type WalletRepositoryImpl struct {
	pool *pgxpool.Pool
}

func NewWalletRepository(pool *pgxpool.Pool) *WalletRepositoryImpl {
	return &WalletRepositoryImpl{pool: pool}
}

func (r *WalletRepositoryImpl) Create(ctx context.Context, userID int64) error {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `INSERT INTO wallets (user_id) VALUES ($1)`

	_, err := q.Exec(ctx, sql, userID)
	if err != nil {
		return fmt.Errorf("WalletRepository.Create user_id=%d: %w", userID, mapErr(err))
	}
	return nil
}

func (r *WalletRepositoryImpl) GetByUserID(ctx context.Context, userID int64) (domain.Wallet, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		SELECT user_id, balance, updated_at
		FROM wallets
		WHERE user_id = $1`

	var w domain.Wallet
	err := q.QueryRow(ctx, sql, userID).Scan(&w.UserID, &w.Balance, &w.UpdatedAt)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("WalletRepository.GetByUserID user_id=%d: %w", userID, mapErr(err))
	}
	return w, nil
}

// GetByUserIDForUpdate — SELECT ... FOR UPDATE. Блокирует строку кошелька,
// чтобы параллельные транзакции не смогли одновременно изменить баланс.
// CHECK (balance >= 0) в схеме — последний рубеж, но FOR UPDATE — основной.
func (r *WalletRepositoryImpl) GetByUserIDForUpdate(ctx context.Context, userID int64) (domain.Wallet, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		SELECT user_id, balance, updated_at
		FROM wallets
		WHERE user_id = $1
		FOR UPDATE`

	var w domain.Wallet
	err := q.QueryRow(ctx, sql, userID).Scan(&w.UserID, &w.Balance, &w.UpdatedAt)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("WalletRepository.GetByUserIDForUpdate user_id=%d: %w", userID, mapErr(err))
	}
	return w, nil
}

// UpdateBalance меняет баланс на delta. Возвращает обновлённый баланс.
// Если CHECK (balance >= 0) нарушен — БД вернёт ошибку (23514 check_violation),
// которая дойдёт до сервиса как есть; сервис мапит её в insufficient_balance.
func (r *WalletRepositoryImpl) UpdateBalance(ctx context.Context, userID int64, delta int64) (int64, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		UPDATE wallets
		SET balance = balance + $2
		WHERE user_id = $1
		RETURNING balance`

	var newBalance int64
	err := q.QueryRow(ctx, sql, userID, delta).Scan(&newBalance)
	if err != nil {
		return 0, fmt.Errorf("WalletRepository.UpdateBalance user_id=%d delta=%d: %w", userID, delta, mapErr(err))
	}
	return newBalance, nil
}

var _ WalletRepository = (*WalletRepositoryImpl)(nil)
