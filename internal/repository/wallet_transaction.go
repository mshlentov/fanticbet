package repository

import (
	"context"
	"fmt"

	"fanticbet/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultPageSize — размер страницы по умолчанию для листингов (транзакции,
// ставки). Позже вынесем в конфиг, пока держим константой.
const DefaultPageSize = 50

// WalletTransactionRepository — интерфейс журнала движений фантиков.
type WalletTransactionRepository interface {
	// Create вставляет запись о движении. balance_after фиксирует итоговый
	// баланс после операции — это и есть «защита от багов»: сумма всех записей
	// пользователя должна сходиться с текущим балансом.
	Create(ctx context.Context, tx domain.WalletTransaction) (int64, error)
	// ListByUser возвращает страницу истории пользователя (новые — первые).
	// page начинается с 1.
	ListByUser(ctx context.Context, userID int64, page int) ([]domain.WalletTransaction, error)
}

type WalletTransactionRepositoryImpl struct {
	pool *pgxpool.Pool
}

func NewWalletTransactionRepository(pool *pgxpool.Pool) *WalletTransactionRepositoryImpl {
	return &WalletTransactionRepositoryImpl{pool: pool}
}

func (r *WalletTransactionRepositoryImpl) Create(ctx context.Context, t domain.WalletTransaction) (int64, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		INSERT INTO wallet_transactions (user_id, amount, type, bet_id, balance_after)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`

	var id int64
	err := q.QueryRow(ctx, sql, t.UserID, t.Amount, t.Type, t.BetID, t.BalanceAfter).
		Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("WalletTransactionRepository.Create: %w", mapErr(err))
	}
	return id, nil
}

func (r *WalletTransactionRepositoryImpl) ListByUser(ctx context.Context, userID int64, page int) ([]domain.WalletTransaction, error) {
	q := QuerierFromCtx(ctx, r.pool)

	if page < 1 {
		page = 1
	}
	offset := (page - 1) * DefaultPageSize

	const sql = `
		SELECT id, user_id, amount, type, bet_id, balance_after, created_at
		FROM wallet_transactions
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`

	rows, err := q.Query(ctx, sql, userID, DefaultPageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("WalletTransactionRepository.ListByUser user_id=%d: %w", userID, mapErr(err))
	}
	defer rows.Close()

	// Предварительно capacity не закладываем — реальный размер страницы заранее
	// неизвестен (может быть меньше pageSize на последней странице).
	var result []domain.WalletTransaction
	for rows.Next() {
		var t domain.WalletTransaction
		if err := rows.Scan(&t.ID, &t.UserID, &t.Amount, &t.Type, &t.BetID, &t.BalanceAfter, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("WalletTransactionRepository.ListByUser scan: %w", mapErr(err))
		}
		result = append(result, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("WalletTransactionRepository.ListByUser rows: %w", mapErr(err))
	}
	return result, nil
}

var _ WalletTransactionRepository = (*WalletTransactionRepositoryImpl)(nil)
