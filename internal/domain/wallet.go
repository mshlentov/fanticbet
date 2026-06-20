package domain

import "time"

// Wallet — кошелёк пользователя. Balance хранится целым числом (фантики),
// никаких float. Контракт: баланс сходится с суммой wallet_transactions.
type Wallet struct {
	UserID    int64
	Balance   int64
	UpdatedAt time.Time
}
