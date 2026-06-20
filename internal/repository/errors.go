package repository

import (
	"errors"

	"fanticbet/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// mapErr переводит ошибку pgx в доменную. Применяется во всех репозиториях,
// чтобы слой service не знал про pgx.ErrNoRows / *pgconn.PgError.
//
// • pgx.ErrNoRows                       → domain.ErrNotFound
// • pgconn.PgError код 23505 (unique)   → domain.ErrConflict
// • context.Cancelled                   → как есть (не доменная, выйдет наверх)
// • прочее                              → как есть, с обёрткой у вызывающего
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 23505 — unique_violation: дубликат по UNIQUE-индексу.
		if pgErr.Code == "23505" {
			return domain.ErrConflict
		}
	}
	return err
}
