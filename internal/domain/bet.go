package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Bet — ставка пользователя на исход. Stake и PotentialPayout — целые фантики
// (int64), без float. Odds — коэффициент, ЗАФИКСИРОВАННЫЙ в момент размещения:
// это копия Outcome.Odds на тот момент, поэтому дальнейшие изменения
// коэффициента воркером на ставку не влияют. EventID — денормализация для
// выборок по событию (settlement, история) без JOIN к outcome→market→event.
// SettledAt заполнен только после расчёта (NULL, пока статус pending).
type Bet struct {
	ID              int64
	UserID          int64
	OutcomeID       int64
	EventID         int64
	Stake           int64
	Odds            decimal.Decimal // зафиксированный коэффициент, NUMERIC(8,3)
	PotentialPayout int64           // floor(stake * odds), считается при размещении
	Status          BetStatus
	SettledAt       *time.Time // NULL, пока ставка не рассчитана
	CreatedAt       time.Time
}

// BetWithDetails — ставка, обогащённая названиями события и исхода для истории
// ставок (GET /me/bets, GET /users/:id/bets). Поля денормализованы из
// events/outcomes/markets через JOIN в BetRepository.ListByUser — чтобы в
// истории показывать «Команда A — Команда B» и название исхода, а не голые
// «Событие #id» / «Исход #id». На саму ставку (Bet) эти поля не влияют.
type BetWithDetails struct {
	Bet
	EventTitle   string     // events.title
	EventHome    *string    // events.home; NULL для произвольных событий
	EventAway    *string    // events.away; NULL для произвольных событий
	OutcomeLabel string     // outcomes.label
	MarketType   MarketType // markets.type (ML/TOTALS/CUSTOM)
}
