package domain

import "time"

// User — профиль пользователя. Не содержит данных авторизации: пароль и
// OAuth-привязки вынесены в отдельные структуры (password_hash здесь только
// для удобства маппинга из строки users, email/password_hash — NULL для
// чисто-OAuth аккаунтов).
type User struct {
	ID           int64
	Email        *string // NULL для чисто-OAuth аккаунтов
	PasswordHash *string // NULL для чисто-OAuth аккаунтов
	DisplayName  string
	AvatarURL    *string
	Role         Role
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  *time.Time // NULL, пока не было ни одного входа
}
