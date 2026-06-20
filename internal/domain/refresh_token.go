package domain

import "time"

// RefreshToken — refresh-токен сессии. В БД хранится sha256-хэш (TokenHash),
// не сам токен. RevokedAt != nil означает инвалидированную (logout) сессию.
type RefreshToken struct {
	ID        int64
	UserID    int64
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time // NULL, пока токен активен
}
