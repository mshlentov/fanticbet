package repository

import (
	"context"
	"fmt"

	"fanticbet/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// UserRepository — интерфейс работы с таблицей users. Сервисы зависят от него,
// а не от конкретной *userRepository; это упрощает тестирование с моками.
type UserRepository interface {
	Create(ctx context.Context, u domain.User) (int64, error)
	GetByID(ctx context.Context, id int64) (domain.User, error)
	GetByEmail(ctx context.Context, email string) (domain.User, error)
	Update(ctx context.Context, u domain.User) error
}

// UserRepositoryImpl — реализация поверх pgx. Один пул на инстанс.
type UserRepositoryImpl struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepositoryImpl {
	return &UserRepositoryImpl{pool: pool}
}

// Create вставляет пользователя и возвращает присвоенный id.
// Email/PasswordHash приходят как указатели: nil → NULL в БД (OAuth-аккаунты).
func (r *UserRepositoryImpl) Create(ctx context.Context, u domain.User) (int64, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		INSERT INTO users (email, password_hash, display_name, avatar_url, role)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`

	var id int64
	err := q.QueryRow(ctx, sql, u.Email, u.PasswordHash, u.DisplayName, u.AvatarURL, u.Role).
		Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("UserRepository.Create: %w", mapErr(err))
	}
	return id, nil
}

// GetByID возвращает пользователя по id. Если не найден — domain.ErrNotFound.
func (r *UserRepositoryImpl) GetByID(ctx context.Context, id int64) (domain.User, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		SELECT id, email, password_hash, display_name, avatar_url, role,
		       created_at, updated_at, last_login_at
		FROM users
		WHERE id = $1`

	var u domain.User
	err := q.QueryRow(ctx, sql, id).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.AvatarURL, &u.Role,
		&u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt,
	)
	if err != nil {
		return domain.User{}, fmt.Errorf("UserRepository.GetByID id=%d: %w", id, mapErr(err))
	}
	return u, nil
}

// GetByEmail возвращает пользователя по email. Если не найден — domain.ErrNotFound.
func (r *UserRepositoryImpl) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		SELECT id, email, password_hash, display_name, avatar_url, role,
		       created_at, updated_at, last_login_at
		FROM users
		WHERE email = $1`

	var u domain.User
	err := q.QueryRow(ctx, sql, email).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.AvatarURL, &u.Role,
		&u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt,
	)
	if err != nil {
		return domain.User{}, fmt.Errorf("UserRepository.GetByEmail: %w", mapErr(err))
	}
	return u, nil
}

// Update сохраняет изменяемые поля профиля (password_hash, display_name,
// avatar_url, role). updated_at обновится триггером в БД. ID неизменяем.
// Возвращает domain.ErrNotFound, если строки с таким id нет.
func (r *UserRepositoryImpl) Update(ctx context.Context, u domain.User) error {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		UPDATE users
		SET password_hash = $2,
		    display_name  = $3,
		    avatar_url    = $4,
		    role          = $5
		WHERE id = $1`

	tag, err := q.Exec(ctx, sql, u.ID, u.PasswordHash, u.DisplayName, u.AvatarURL, u.Role)
	if err != nil {
		return fmt.Errorf("UserRepository.Update id=%d: %w", u.ID, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("UserRepository.Update id=%d: %w", u.ID, domain.ErrNotFound)
	}
	return nil
}

// На этапе компиляции гарантируем, что реализация удовлетворяет интерфейсу.
var _ UserRepository = (*UserRepositoryImpl)(nil)
