package domain

import "github.com/shopspring/decimal"

// Outcome — исход рынка. Odds — ТЕКУЩИЙ коэффициент (обновляется воркером);
// при размещении ставки он фиксируется в Bet.Odds, поэтому дальнейшие изменения
// на уже сделанную ставку не влияют. Result заполняется при расчёте (NULL до).
type Outcome struct {
	ID       int64
	MarketID int64
	Code     OutcomeCode
	Label    string
	Odds     decimal.Decimal // текущий коэффициент, NUMERIC(8,3), всегда > 1.0
	Result   *Result         // NULL, пока исход не рассчитан
}
