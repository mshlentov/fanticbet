package domain

import "time"

// AuthIdentity — привязка внешнего OAuth-провайдера к пользователю.
// У одного User может быть несколько привязок (Google + VK).
type AuthIdentity struct {
	ID             int64
	UserID         int64
	Provider       Provider
	ProviderUserID string
	CreatedAt      time.Time
}
