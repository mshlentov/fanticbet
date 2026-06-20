package domain

import (
	"encoding/json"
	"time"
)

// Event — спортивное (Source=oddsapi) или кастомное (Source=custom) событие.
// Единая модель: и реальные матчи из Odds-API, и созданные админом события
// живут в одной таблице и ссылаются на markets → outcomes.
type Event struct {
	ID         int64
	Source     EventSource
	ExternalID *int64 // id в Odds-API; NULL для кастомных
	SportSlug  string
	LeagueName *string
	Title      string
	Home       *string // NULL для кастомных
	Away       *string // NULL для кастомных
	StartsAt   time.Time
	Status     EventStatus
	Scores     json.RawMessage // сырой scores из API; NULL, пока счёта нет
	CreatedBy  *int64          // админ-автор кастомного события; NULL для oddsapi
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
