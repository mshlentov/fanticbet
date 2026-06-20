package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier — минимум интерфейса pgx, общего для *pgxpool.Pool и pgx.Tx.
// Нужен, чтобы репозиторий работал и с пулом (вне транзакции),
// и с активной транзакцией (через context).
type Querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// txCtxKey — типизированный ключ для context, чтобы не collide с чужими значениями.
type txCtxKey struct{}

// QuerierFromCtx достаёт активную транзакцию из контекста. Если её нет —
// возвращает пул. Репозитории вызывают именно эту функцию: один и тот же код
// работает и в транзакции, и без неё.
func QuerierFromCtx(ctx context.Context, pool *pgxpool.Pool) Querier {
	if tx, ok := ctx.Value(txCtxKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}

// TxManager управляет транзакциями. Сервисы зависят от него (интерфейс ниже),
// что упрощает тестирование.
type TxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

// RunInTx выполняет fn внутри одной транзакции. При возврате ошибки — rollback,
// при успехе — commit. Транзакция прокидывается в fn через context, откуда её
// подхватывают репозитории (QuerierFromCtx).
//
// Важно: pgx.Commit/Rollback при уже закрытой транзакции возвращают ErrTxClosed —
// мы это игнорируем, чтобы не маскировать исходную ошибку fn.
func (m *TxManager) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("TxManager.Begin: %w", err)
	}

	txCtx := context.WithValue(ctx, txCtxKey{}, tx)

	if err := fn(txCtx); err != nil {
		// Откатываем, ошибку отката не возвращаем — важна исходная ошибка fn.
		_ = tx.Rollback(ctx)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("TxManager.Commit: %w", err)
	}
	return nil
}
