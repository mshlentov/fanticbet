package repository

import (
	"context"
	"fmt"

	"fanticbet/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RefreshTokenRepository — интерфейс работы с refresh-токенами. В БД хранится
// только sha256-хэш токена, сам токен от клиента до БД никогда не доходит.
type RefreshTokenRepository interface {
	// Create сохраняет хэш нового refresh-токена.
	Create(ctx context.Context, t domain.RefreshToken) (int64, error)
	// GetByHash возвращает токен по его хэшу. Если не найден — domain.ErrNotFound.
	// Сервис проверяет expires_at и revoked_at отдельно — тут только данные.
	GetByHash(ctx context.Context, hash string) (domain.RefreshToken, error)
	// Revoke помечает токен отозванным (ставит revoked_at = now()).
	// Идемпотентен: повторный вызов не меняет уже отозванный токен.
	// Возвращает domain.ErrNotFound, если токена с таким id нет.
	Revoke(ctx context.Context, id int64) error
}

type RefreshTokenRepositoryImpl struct {
	pool *pgxpool.Pool
}

func NewRefreshTokenRepository(pool *pgxpool.Pool) *RefreshTokenRepositoryImpl {
	return &RefreshTokenRepositoryImpl{pool: pool}
}

func (r *RefreshTokenRepositoryImpl) Create(ctx context.Context, t domain.RefreshToken) (int64, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id`

	var id int64
	err := q.QueryRow(ctx, sql, t.UserID, t.TokenHash, t.ExpiresAt).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("RefreshTokenRepository.Create: %w", mapErr(err))
	}
	return id, nil
}

func (r *RefreshTokenRepositoryImpl) GetByHash(ctx context.Context, hash string) (domain.RefreshToken, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		SELECT id, user_id, token_hash, expires_at, revoked_at
		FROM refresh_tokens
		WHERE token_hash = $1`

	var t domain.RefreshToken
	err := q.QueryRow(ctx, sql, hash).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt)
	if err != nil {
		return domain.RefreshToken{}, fmt.Errorf("RefreshTokenRepository.GetByHash: %w", mapErr(err))
	}
	return t, nil
}

// Revoke — UPDATE revoked_at = now(). Идемпотентность: условие revoked_at IS NULL
// не ставим, чтобы повторный logout просто перезаписал timestamp (без ошибки).
// Если строки нет — domain.ErrNotFound.
func (r *RefreshTokenRepositoryImpl) Revoke(ctx context.Context, id int64) error {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		UPDATE refresh_tokens
		SET revoked_at = now()
		WHERE id = $1`

	tag, err := q.Exec(ctx, sql, id)
	if err != nil {
		return fmt.Errorf("RefreshTokenRepository.Revoke id=%d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("RefreshTokenRepository.Revoke id=%d: %w", id, domain.ErrNotFound)
	}
	return nil
}

var _ RefreshTokenRepository = (*RefreshTokenRepositoryImpl)(nil)
