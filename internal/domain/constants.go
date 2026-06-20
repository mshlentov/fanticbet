package domain

// Роли пользователей. Совпадают со значениями колонки users.role.
// Типизированный тип Role защищает от опечаток и случайной передачи
// произвольной строки в колонку role.
type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

// Типы движений по кошельку. Совпадают со значениями колонки
// wallet_transactions.type.
type TxType string

const (
	TxSignupBonus TxType = "signup_bonus" // начисление бонуса при регистрации
	TxBetStake    TxType = "bet_stake"    // списание суммы ставки
	TxBetPayout   TxType = "bet_payout"   // выплата выигрыша
	TxBetRefund   TxType = "bet_refund"   // возврат ставки (void/cancelled)
	TxAdminAdjust TxType = "admin_adjust" // ручная корректировка баланса админом
)

// OAuth-провайдеры. Совпадают со значениями колонки auth_identities.provider.
// VK и Яндекс заведены заранее: добавление константы дешевле миграции позже.
type Provider string

const (
	ProviderGoogle Provider = "google"
	ProviderVK     Provider = "vk"
	ProviderYandex Provider = "yandex"
)
