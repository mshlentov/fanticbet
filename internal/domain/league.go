package domain

import "time"

// League — чемпионат/лига (веха M8). Справочник, группирующий события (АПЛ, НБА,
// «Кубок двора» и т.д.). Заводится админом через /admin/leagues; событие ссылается
// на лигу через events.league_id (NULL для произвольных и oddsapi-событий без лиги).
// sport_slug фильтрует лиги по виду спорта (football, basketball, ...); у custom-
// событий league_id = NULL, поэтому значение 'custom' здесь не используется.
type League struct {
	ID        int64
	Name      string
	SportSlug string
	CreatedAt time.Time
	UpdatedAt time.Time
}
