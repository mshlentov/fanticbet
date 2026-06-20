package domain

import "github.com/shopspring/decimal"

// Market — рынок события (ML, TOTALS или CUSTOM). Line задан только для TOTALS
// (линия тотала), Question — только для CUSTOM (текст вопроса). Коэффициенты
// лежат не здесь, а в связанных Outcome.
type Market struct {
	ID       int64
	EventID  int64
	Type     MarketType
	Line     *decimal.Decimal // линия тотала; NULL для ML/CUSTOM
	Question *string          // текст вопроса; NULL кроме CUSTOM
	Status   MarketStatus
}
