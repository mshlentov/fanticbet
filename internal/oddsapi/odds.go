package oddsapi

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// maxMultiEvents — лимит API на число событий в одном запросе /odds/multi.
const maxMultiEvents = 10

// EventOdds — котировки одного события, сгруппированные по букмекерам.
// Маппинг в markets/outcomes делает OddsSyncWorker (выбор основной линии тотала,
// конвертация строковых коэффициентов в decimal) — это бизнес-логика, не транспорт.
type EventOdds struct {
	EventID    int64 // внешний id (id в Odds-API)
	Status     string
	Bookmakers map[string][]Market // ключ — имя букмекера ("Bet365", "Pinnacle")
}

// Market — рынок одного букмекера ("ML", "Totals", "Asian Handicap", …).
type Market struct {
	Name      string
	UpdatedAt time.Time
	Lines     []OddsLine
}

// OddsLine — одна строка котировок рынка. Для ML заполнены Home/Draw/Away,
// для Totals — Over/Under плюс Hdp (линия). Коэффициенты остаются строками,
// как их отдаёт API ("2.10"); пустая строка означает отсутствие исхода
// (например, Draw в двухисходном спорте). Воркер парсит их в decimal и
// проверяет > 1.0.
type OddsLine struct {
	Hdp   *decimal.Decimal // линия тотала/гандикапа; nil для ML
	Home  string
	Draw  string
	Away  string
	Over  string
	Under string
}

// apiOddsEvent — событие с котировками в ответах /odds и /odds/multi.
type apiOddsEvent struct {
	ID         int64                      `json:"id"`
	Status     string                     `json:"status"`
	Bookmakers map[string][]apiOddsMarket `json:"bookmakers"`
}

type apiOddsMarket struct {
	Name      string        `json:"name"`
	UpdatedAt time.Time     `json:"updatedAt"`
	Odds      []apiOddsLine `json:"odds"`
}

type apiOddsLine struct {
	Hdp   *decimal.Decimal `json:"hdp"`
	Home  string           `json:"home"`
	Draw  string           `json:"draw"`
	Away  string           `json:"away"`
	Over  string           `json:"over"`
	Under string           `json:"under"`
}

func (e apiOddsEvent) toEventOdds() EventOdds {
	out := EventOdds{
		EventID:    e.ID,
		Status:     e.Status,
		Bookmakers: make(map[string][]Market, len(e.Bookmakers)),
	}
	for bk, markets := range e.Bookmakers {
		ms := make([]Market, 0, len(markets))
		for _, m := range markets {
			lines := make([]OddsLine, 0, len(m.Odds))
			for _, o := range m.Odds {
				lines = append(lines, OddsLine{
					Hdp:   o.Hdp,
					Home:  o.Home,
					Draw:  o.Draw,
					Away:  o.Away,
					Over:  o.Over,
					Under: o.Under,
				})
			}
			ms = append(ms, Market{Name: m.Name, UpdatedAt: m.UpdatedAt, Lines: lines})
		}
		out.Bookmakers[bk] = ms
	}
	return out
}

// GetOdds возвращает котировки одного события от перечисленных букмекеров.
func (c *Client) GetOdds(ctx context.Context, eventID int64, bookmakers []string) (EventOdds, error) {
	if len(bookmakers) == 0 {
		return EventOdds{}, fmt.Errorf("oddsapi: GetOdds requires at least one bookmaker")
	}

	q := url.Values{}
	q.Set("eventId", strconv.FormatInt(eventID, 10))
	q.Set("bookmakers", strings.Join(bookmakers, ","))

	var raw apiOddsEvent
	if err := c.do(ctx, "/odds", q, &raw); err != nil {
		return EventOdds{}, err
	}
	return raw.toEventOdds(), nil
}

// GetOddsMulti возвращает котировки до 10 событий за один запрос (1 вызов лимита).
// eventIDs сверх maxMultiEvents отклоняются с ошибкой — батчинг по 10 делает воркер.
func (c *Client) GetOddsMulti(ctx context.Context, eventIDs []int64, bookmakers []string) ([]EventOdds, error) {
	if len(eventIDs) == 0 {
		return nil, nil
	}
	if len(eventIDs) > maxMultiEvents {
		return nil, fmt.Errorf("oddsapi: GetOddsMulti supports up to %d events, got %d", maxMultiEvents, len(eventIDs))
	}
	if len(bookmakers) == 0 {
		return nil, fmt.Errorf("oddsapi: GetOddsMulti requires at least one bookmaker")
	}

	ids := make([]string, len(eventIDs))
	for i, id := range eventIDs {
		ids[i] = strconv.FormatInt(id, 10)
	}

	q := url.Values{}
	q.Set("eventIds", strings.Join(ids, ","))
	q.Set("bookmakers", strings.Join(bookmakers, ","))

	var raw []apiOddsEvent
	if err := c.do(ctx, "/odds/multi", q, &raw); err != nil {
		return nil, err
	}

	out := make([]EventOdds, 0, len(raw))
	for _, e := range raw {
		out = append(out, e.toEventOdds())
	}
	return out, nil
}
