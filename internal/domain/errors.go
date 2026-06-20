package domain

import "errors"

// Сентинельные ошибки домена. Слой service/repository возвращает их, а handler
// уже мапит на HTTP-коды. Так бизнес-логика не зависит от транспорта и от pgx.
var (
	// ErrNotFound — запрошенная сущность не существует.
	ErrNotFound = errors.New("not found")
	// ErrConflict — нарушение уникальности / гонка (дубликат email, уже занят и т.п.).
	ErrConflict = errors.New("conflict")
)
