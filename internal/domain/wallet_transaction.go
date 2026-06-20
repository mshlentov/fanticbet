package domain

import "time"

// WalletTransaction — запись журнала движений фантиков (append-only).
// Amount: + начисление, − списание. BetID ссылается на ставку, когда движение
// связано со ставкой (NULL для signup_bonus/admin_adjust).
type WalletTransaction struct {
	ID           int64
	UserID       int64
	Amount       int64
	Type         TxType
	BetID        *int64 // NULL, если движение не связано со ставкой
	BalanceAfter int64
	CreatedAt    time.Time
}
