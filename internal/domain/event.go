package domain

import (
	"encoding/json"
	"time"
)

// Event — спортивное (Source=oddsapi/manual) или кастомное (Source=custom) событие.
// Единая модель: реальные матчи из Odds-API, заведённые админом спортивные матчи и
// произвольные события живут в одной таблице и ссылаются на markets → outcomes.
type Event struct {
	ID         int64
	Source     EventSource
	ExternalID *int64 // id в Odds-API; NULL для manual/custom
	SportSlug  string
	LeagueID   *int64  // ссылка на чемпионат; NULL для custom и oddsapi-событий без лиги
	LeagueName *string // денормализованная копия leagues.name (или строка из API)
	Title      string
	Home       *string // NULL для кастомных
	Away       *string // NULL для кастомных
	StartsAt   time.Time
	Status     EventStatus
	Scores     json.RawMessage // сырой scores: {"home":N,"away":N}; NULL, пока счёта нет
	FeaturedAt *time.Time      // метка «популярное»: NULL = обычное, заполнено = популярное (M9)
	CreatedBy  *int64          // админ-автор manual/custom события; NULL для oddsapi
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
