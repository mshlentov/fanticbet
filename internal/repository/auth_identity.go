package repository

import (
	"context"
	"fmt"

	"fanticbet/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AuthIdentityRepository — интерфейс работы с OAuth-привязками пользователей.
// Используется на OAuth-callback: ищем привязку по (provider, provider_user_id),
// при отсутствии — создаём.
type AuthIdentityRepository interface {
	// GetByProvider возвращает привязку внешнего аккаунта к пользователю.
	// Если не найдена — domain.ErrNotFound.
	GetByProvider(ctx context.Context, provider domain.Provider, providerUserID string) (domain.AuthIdentity, error)
	// Create привязывает внешний аккаунт к существующему user_id.
	// UNIQUE (provider, provider_user_id) → domain.ErrConflict при дубле.
	Create(ctx context.Context, identity domain.AuthIdentity) (int64, error)
}

type AuthIdentityRepositoryImpl struct {
	pool *pgxpool.Pool
}

func NewAuthIdentityRepository(pool *pgxpool.Pool) *AuthIdentityRepositoryImpl {
	return &AuthIdentityRepositoryImpl{pool: pool}
}

func (r *AuthIdentityRepositoryImpl) GetByProvider(ctx context.Context, provider domain.Provider, providerUserID string) (domain.AuthIdentity, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		SELECT id, user_id, provider, provider_user_id, created_at
		FROM auth_identities
		WHERE provider = $1 AND provider_user_id = $2`

	var a domain.AuthIdentity
	err := q.QueryRow(ctx, sql, provider, providerUserID).
		Scan(&a.ID, &a.UserID, &a.Provider, &a.ProviderUserID, &a.CreatedAt)
	if err != nil {
		return domain.AuthIdentity{}, fmt.Errorf("AuthIdentityRepository.GetByProvider provider=%s: %w", provider, mapErr(err))
	}
	return a, nil
}

func (r *AuthIdentityRepositoryImpl) Create(ctx context.Context, identity domain.AuthIdentity) (int64, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		INSERT INTO auth_identities (user_id, provider, provider_user_id)
		VALUES ($1, $2, $3)
		RETURNING id`

	var id int64
	err := q.QueryRow(ctx, sql, identity.UserID, identity.Provider, identity.ProviderUserID).
		Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("AuthIdentityRepository.Create provider=%s: %w", identity.Provider, mapErr(err))
	}
	return id, nil
}

var _ AuthIdentityRepository = (*AuthIdentityRepositoryImpl)(nil)
