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

// Источник события. Совпадает со значениями колонки events.source.
// 'oddsapi' — событие из Odds-API (есть external_id); 'manual' — спортивный
// матч, заведённый админом вручную (веха M8, external_id = NULL, рынки ML/TOTALS
// с кэфами и расчёт по введённому счёту); 'custom' — произвольное событие,
// созданное админом (external_id = NULL, рынок CUSTOM).
type EventSource string

const (
	SourceOddsAPI EventSource = "oddsapi"
	SourceManual  EventSource = "manual" // спортивный матч, заведённый админом (M8)
	SourceCustom  EventSource = "custom"
)

// Статусы события. Совпадают со значениями колонки events.status.
// Жизненный цикл: upcoming → live → settled; cancelled — отмена (void ставок).
type EventStatus string

const (
	EventUpcoming  EventStatus = "upcoming"  // ещё не началось, ставки открыты
	EventLive      EventStatus = "live"      // идёт, ставки закрыты
	EventSettled   EventStatus = "settled"   // завершено и рассчитано
	EventCancelled EventStatus = "cancelled" // отменено, ставки возвращены
)

// Типы рынков. Совпадают со значениями колонки markets.type.
// ML — исход матча (1X2 / победитель); TOTALS — тотал (over/under по линии);
// CUSTOM — произвольный рынок кастомного события.
type MarketType string

const (
	MarketML     MarketType = "ML"
	MarketTotals MarketType = "TOTALS"
	MarketCustom MarketType = "CUSTOM"
)

// Статусы рынка. Совпадают со значениями колонки markets.status.
// open — приём ставок; suspended — букмекер снял рынок (временно закрыт);
// settled — рассчитан; void — аннулирован (возврат ставок).
type MarketStatus string

const (
	MarketOpen      MarketStatus = "open"
	MarketSuspended MarketStatus = "suspended"
	MarketSettled   MarketStatus = "settled"
	MarketVoid      MarketStatus = "void"
)

// Коды исходов. Совпадают со значениями колонки outcomes.code. Для CUSTOM-рынков
// коды формируются как opt_1, opt_2, … (см. OutcomeCustomPrefix).
type OutcomeCode string

const (
	OutcomeHome  OutcomeCode = "home"  // победа хозяев (ML)
	OutcomeDraw  OutcomeCode = "draw"  // ничья (ML)
	OutcomeAway  OutcomeCode = "away"  // победа гостей (ML)
	OutcomeOver  OutcomeCode = "over"  // тотал больше линии (TOTALS)
	OutcomeUnder OutcomeCode = "under" // тотал меньше линии (TOTALS)
)

// OutcomeCustomPrefix — префикс кодов исходов кастомного рынка (opt_1, opt_2, …).
const OutcomeCustomPrefix = "opt_"

// Результат исхода. Совпадает со значениями колонки outcomes.result.
// NULL в outcomes.result означает «ещё не рассчитан»; pending здесь быть не
// может — он относится только к ставке (см. BetStatus).
type Result string

const (
	ResultWon  Result = "won"  // выигрыш
	ResultLost Result = "lost" // проигрыш
	ResultVoid Result = "void" // возврат (push / отмена)
)

// Статусы ставки. Совпадают со значениями колонки bets.status. pending — ставка
// размещена, но исход ещё не рассчитан; won/lost/void — результат после settlement
// (значения совпадают с Result, т.к. ставка на winning-исход выигрывает и т.п.).
type BetStatus string

const (
	BetPending BetStatus = "pending" // ожидает расчёта
	BetWon     BetStatus = "won"     // рассчитана выигрышем
	BetLost    BetStatus = "lost"    // рассчитана проигрышем
	BetVoid    BetStatus = "void"    // возврат (исход void или событие cancelled)
)
