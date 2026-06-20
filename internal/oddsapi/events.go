package oddsapi

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fanticbet/internal/domain"
)

// Sport — вид спорта из GET /sports. Доменного аналога нет (виды спорта мы не
// храним отдельной таблицей), поэтому возвращаем локальный тип.
type Sport struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// apiRef — вложенный объект {name, slug} (sport, league в ответах событий).
type apiRef struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// apiEvent — событие в ответах /events и /events/{id}. Поля, не нужные на этапе
// M2 (homeId/awayId, clock), сознательно опущены — JSON-декодер их игнорирует.
type apiEvent struct {
	ID     int64           `json:"id"`
	Home   *string         `json:"home"`
	Away   *string         `json:"away"`
	Date   time.Time       `json:"date"`
	Status string          `json:"status"`
	Sport  apiRef          `json:"sport"`
	League *apiRef         `json:"league"`
	Scores json.RawMessage `json:"scores"`
}

// toDomain переводит событие API в доменную модель. ID из API кладём в
// ExternalID (наш собственный ID присвоит БД при Upsert), Source — oddsapi.
func (e apiEvent) toDomain() domain.Event {
	extID := e.ID
	ev := domain.Event{
		Source:     domain.SourceOddsAPI,
		ExternalID: &extID,
		SportSlug:  e.Sport.Slug,
		Home:       e.Home,
		Away:       e.Away,
		Title:      buildTitle(e.Home, e.Away),
		StartsAt:   e.Date,
		Status:     mapEventStatus(e.Status),
		Scores:     e.Scores,
	}
	if e.League != nil {
		name := e.League.Name
		ev.LeagueName = &name
	}
	return ev
}

// GetSports возвращает список видов спорта, поддерживаемых API.
func (c *Client) GetSports(ctx context.Context) ([]Sport, error) {
	var sports []Sport
	if err := c.do(ctx, "/sports", nil, &sports); err != nil {
		return nil, err
	}
	return sports, nil
}

// GetEvents возвращает события указанного вида спорта, опционально отфильтрованные
// по статусам (pending/live/settled). statuses=nil — без фильтра по статусу.
// Горизонт по умолчанию — 14 дней (поведение API при отсутствии параметра to).
func (c *Client) GetEvents(ctx context.Context, sport string, statuses []string) ([]domain.Event, error) {
	q := url.Values{}
	q.Set("sport", sport)
	if len(statuses) > 0 {
		q.Set("status", strings.Join(statuses, ","))
	}

	var raw []apiEvent
	if err := c.do(ctx, "/events", q, &raw); err != nil {
		return nil, err
	}

	events := make([]domain.Event, 0, len(raw))
	for _, e := range raw {
		events = append(events, e.toDomain())
	}
	return events, nil
}

// GetEvent возвращает одно событие по его внешнему id (id в Odds-API).
// Используется settlement-воркером для чтения финального scores.
func (c *Client) GetEvent(ctx context.Context, externalID int64) (domain.Event, error) {
	path := "/events/" + strconv.FormatInt(externalID, 10)

	var raw apiEvent
	if err := c.do(ctx, path, nil, &raw); err != nil {
		return domain.Event{}, err
	}
	return raw.toDomain(), nil
}

// mapEventStatus переводит статус события из API в доменный. API оперирует
// pending/live/settled; домен — upcoming/live/settled/cancelled. Неизвестный
// статус трактуем как upcoming (консервативно — ставки остаются открытыми,
// а воркер перепроверит позже).
func mapEventStatus(s string) domain.EventStatus {
	switch strings.ToLower(s) {
	case "pending":
		return domain.EventUpcoming
	case "live":
		return domain.EventLive
	case "settled":
		return domain.EventSettled
	case "cancelled", "canceled":
		return domain.EventCancelled
	default:
		return domain.EventUpcoming
	}
}

// buildTitle собирает заголовок события вида "Home — Away". Для oddsapi-событий
// обе команды всегда заданы; на случай отсутствия одной из них — деградируем мягко.
func buildTitle(home, away *string) string {
	h, a := "", ""
	if home != nil {
		h = *home
	}
	if away != nil {
		a = *away
	}
	switch {
	case h != "" && a != "":
		return h + " — " + a
	case h != "":
		return h
	default:
		return a
	}
}
