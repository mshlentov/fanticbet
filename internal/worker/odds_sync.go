package worker

import (
	"context"
	"log"
	"strings"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/oddsapi"
	"fanticbet/internal/repository"

	"github.com/shopspring/decimal"
)

// oddsBatchSize — сколько событий уходит в один запрос /odds/multi (лимит API).
const oddsBatchSize = 10

// totalsTarget — «эталонная» котировка сбалансированной линии тотала (1.90/1.90).
// Из всех линий TOTALS выбираем ту, чьи over/under ближе всего к этому значению —
// это и есть основная линия букмекера.
var totalsTarget = decimal.RequireFromString("1.90")

// oneDecimal — порог CHECK odds > 1.0; используем для отсева некорректных котировок.
var oneDecimal = decimal.NewFromInt(1)

// OddsClient — то, что OddsSyncWorker берёт у клиента Odds-API.
type OddsClient interface {
	GetOddsMulti(ctx context.Context, eventIDs []int64, bookmakers []string) ([]oddsapi.EventOdds, error)
}

// OddsSyncWorker обновляет коэффициенты по ближайшим событиям. Берёт из БД
// upcoming-события, стартующие в окне within, пачками по 10 запрашивает их
// котировки у одного букмекера и наполняет/обновляет outcomes рынков ML и TOTALS.
// Снятый букмекером рынок переводит в suspended; начавшиеся события не трогает
// (их и так не возвращает ListForOddsSync).
type OddsSyncWorker struct {
	events    repository.EventRepository
	markets   repository.MarketRepository
	outcomes  repository.OutcomeRepository
	client    OddsClient
	bookmaker string
	within    time.Duration
	logger    *log.Logger
}

// NewOddsSyncWorker собирает воркер. bookmaker — единственный источник котировок
// (из конфига), within — горизонт выборки событий (например, 48ч).
func NewOddsSyncWorker(
	events repository.EventRepository,
	markets repository.MarketRepository,
	outcomes repository.OutcomeRepository,
	client OddsClient,
	bookmaker string,
	within time.Duration,
	logger *log.Logger,
) *OddsSyncWorker {
	if logger == nil {
		logger = log.Default()
	}
	return &OddsSyncWorker{
		events:    events,
		markets:   markets,
		outcomes:  outcomes,
		client:    client,
		bookmaker: bookmaker,
		within:    within,
		logger:    logger,
	}
}

// Run — одна итерация синхронизации котировок.
func (w *OddsSyncWorker) Run(ctx context.Context) error {
	evs, err := w.events.ListForOddsSync(ctx, w.within)
	if err != nil {
		return err
	}
	if len(evs) == 0 {
		w.logger.Printf("OddsSync: no upcoming events in next %s", w.within)
		return nil
	}

	var updated int
	for _, batch := range chunkEvents(evs, oddsBatchSize) {
		if err := ctx.Err(); err != nil {
			return err
		}

		ids := externalIDs(batch)
		if len(ids) == 0 {
			continue
		}

		oddsList, err := w.client.GetOddsMulti(ctx, ids, []string{w.bookmaker})
		if err != nil {
			w.logger.Printf("OddsSync: GetOddsMulti ids=%v: %v", ids, err)
			continue
		}

		byExt := make(map[int64]oddsapi.EventOdds, len(oddsList))
		for _, eo := range oddsList {
			byExt[eo.EventID] = eo
		}

		for _, ev := range batch {
			if ev.ExternalID == nil {
				continue
			}
			eo, ok := byExt[*ev.ExternalID]
			if err := w.applyEvent(ctx, ev, eo, ok); err != nil {
				w.logger.Printf("OddsSync: event_id=%d external_id=%d apply: %v", ev.ID, *ev.ExternalID, err)
				continue
			}
			updated++
		}
	}

	w.logger.Printf("OddsSync: processed %d events (window %s)", updated, w.within)
	return nil
}

// applyEvent обновляет рынки одного события по полученным котировкам. Если
// событие/букмекер не вернулись (present=false или нет нужного букмекера) —
// рынки переводятся в suspended.
func (w *OddsSyncWorker) applyEvent(ctx context.Context, ev domain.Event, eo oddsapi.EventOdds, present bool) error {
	dbMarkets, err := w.markets.GetByEvent(ctx, ev.ID)
	if err != nil {
		return err
	}
	mlMarket := findDBMarket(dbMarkets, domain.MarketML)
	totalsMarket := findDBMarket(dbMarkets, domain.MarketTotals)

	// Котировки букмекера из конфига (имя в ответе API может отличаться регистром).
	var bkMarkets []oddsapi.Market
	if present {
		bkMarkets, present = findBookmakerMarkets(eo, w.bookmaker)
	}
	if !present {
		// Нет данных по событию/букмекеру — снимаем рынки.
		return w.suspendMarkets(ctx, mlMarket, totalsMarket)
	}

	// ML.
	if mlMarket != nil {
		if apiML, ok := findMarket(bkMarkets, isMLName); ok {
			if err := w.applyML(ctx, *mlMarket, apiML, ev); err != nil {
				return err
			}
		} else if err := w.ensureStatus(ctx, *mlMarket, domain.MarketSuspended); err != nil {
			return err
		}
	}

	// TOTALS.
	if totalsMarket != nil {
		if apiTot, ok := findMarket(bkMarkets, isTotalsName); ok {
			if err := w.applyTotals(ctx, *totalsMarket, apiTot); err != nil {
				return err
			}
		} else if err := w.ensureStatus(ctx, *totalsMarket, domain.MarketSuspended); err != nil {
			return err
		}
	}
	return nil
}

// applyML наполняет рынок ML исходами home/draw/away. draw опционален (нет в
// двухисходных видах спорта). Если валидных home/away нет — рынок снимаем.
func (w *OddsSyncWorker) applyML(ctx context.Context, m domain.Market, apiM oddsapi.Market, ev domain.Event) error {
	line, ok := pickMLLine(apiM)
	if !ok {
		return w.ensureStatus(ctx, m, domain.MarketSuspended)
	}

	type cand struct {
		code domain.OutcomeCode
		raw  string
	}
	for _, c := range []cand{
		{domain.OutcomeHome, line.Home},
		{domain.OutcomeDraw, line.Draw},
		{domain.OutcomeAway, line.Away},
	} {
		odds, ok := parseOdds(c.raw)
		if !ok {
			continue // невалидный/отсутствующий исход (например, draw) пропускаем
		}
		if err := w.upsertOutcome(ctx, m.ID, c.code, mlLabel(c.code, ev), odds); err != nil {
			return err
		}
	}
	return w.ensureStatus(ctx, m, domain.MarketOpen)
}

// applyTotals выбирает основную линию тотала, сохраняет её в markets.line и
// наполняет исходы over/under. Если валидной линии нет — рынок снимаем.
func (w *OddsSyncWorker) applyTotals(ctx context.Context, m domain.Market, apiM oddsapi.Market) error {
	line, over, under, ok := pickMainTotalsLine(apiM)
	if !ok {
		return w.ensureStatus(ctx, m, domain.MarketSuspended)
	}

	if m.Line == nil || !m.Line.Equal(line) {
		l := line
		if err := w.markets.UpdateLine(ctx, m.ID, &l); err != nil {
			return err
		}
	}
	if err := w.upsertOutcome(ctx, m.ID, domain.OutcomeOver, totalsLabel(domain.OutcomeOver, line), over); err != nil {
		return err
	}
	if err := w.upsertOutcome(ctx, m.ID, domain.OutcomeUnder, totalsLabel(domain.OutcomeUnder, line), under); err != nil {
		return err
	}
	return w.ensureStatus(ctx, m, domain.MarketOpen)
}

// suspendMarkets переводит оба рынка события в suspended (данных нет).
func (w *OddsSyncWorker) suspendMarkets(ctx context.Context, ml, totals *domain.Market) error {
	if ml != nil {
		if err := w.ensureStatus(ctx, *ml, domain.MarketSuspended); err != nil {
			return err
		}
	}
	if totals != nil {
		if err := w.ensureStatus(ctx, *totals, domain.MarketSuspended); err != nil {
			return err
		}
	}
	return nil
}

// ensureStatus меняет статус рынка только если он отличается (экономим запись).
func (w *OddsSyncWorker) ensureStatus(ctx context.Context, m domain.Market, want domain.MarketStatus) error {
	if m.Status == want {
		return nil
	}
	return w.markets.UpdateStatus(ctx, m.ID, want)
}

// upsertOutcome вставляет/обновляет исход рынка.
func (w *OddsSyncWorker) upsertOutcome(ctx context.Context, marketID int64, code domain.OutcomeCode, label string, odds decimal.Decimal) error {
	_, err := w.outcomes.Upsert(ctx, domain.Outcome{
		MarketID: marketID,
		Code:     code,
		Label:    label,
		Odds:     odds,
	})
	return err
}

// --- чистые помощники (без побочных эффектов, удобно тестировать) ---

// chunkEvents разбивает срез событий на пачки заданного размера.
func chunkEvents(evs []domain.Event, size int) [][]domain.Event {
	var out [][]domain.Event
	for i := 0; i < len(evs); i += size {
		end := i + size
		if end > len(evs) {
			end = len(evs)
		}
		out = append(out, evs[i:end])
	}
	return out
}

// externalIDs собирает внешние id событий пачки (пропуская nil).
func externalIDs(evs []domain.Event) []int64 {
	ids := make([]int64, 0, len(evs))
	for _, e := range evs {
		if e.ExternalID != nil {
			ids = append(ids, *e.ExternalID)
		}
	}
	return ids
}

// findDBMarket возвращает первый рынок указанного типа (или nil).
func findDBMarket(markets []domain.Market, t domain.MarketType) *domain.Market {
	for i := range markets {
		if markets[i].Type == t {
			return &markets[i]
		}
	}
	return nil
}

// findBookmakerMarkets ищет рынки нужного букмекера без учёта регистра
// (API может вернуть "Pinnacle" на запрос "pinnacle").
func findBookmakerMarkets(eo oddsapi.EventOdds, bookmaker string) ([]oddsapi.Market, bool) {
	for name, markets := range eo.Bookmakers {
		if strings.EqualFold(name, bookmaker) {
			return markets, true
		}
	}
	return nil, false
}

// findMarket возвращает первый рынок, чьё имя удовлетворяет pred.
func findMarket(markets []oddsapi.Market, pred func(name string) bool) (oddsapi.Market, bool) {
	for _, m := range markets {
		if pred(m.Name) {
			return m, true
		}
	}
	return oddsapi.Market{}, false
}

// isMLName распознаёт рынок «исход матча» по имени из API.
func isMLName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ml", "moneyline", "money line", "1x2":
		return true
	default:
		return false
	}
}

// isTotalsName распознаёт рынок тотала по имени из API.
func isTotalsName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(n, "total") || n == "over/under" || n == "o/u"
}

// pickMLLine выбирает строку котировок ML: с валидными home и away.
func pickMLLine(m oddsapi.Market) (oddsapi.OddsLine, bool) {
	for _, l := range m.Lines {
		if _, ok := parseOdds(l.Home); !ok {
			continue
		}
		if _, ok := parseOdds(l.Away); !ok {
			continue
		}
		return l, true
	}
	return oddsapi.OddsLine{}, false
}

// pickMainTotalsLine выбирает основную линию тотала — с over/under, ближайшими к
// 1.90/1.90 (минимум суммы отклонений). Линии без Hdp или с невалидными
// котировками пропускаются.
func pickMainTotalsLine(m oddsapi.Market) (line, over, under decimal.Decimal, ok bool) {
	var bestCost decimal.Decimal
	for _, l := range m.Lines {
		if l.Hdp == nil {
			continue
		}
		o, ok1 := parseOdds(l.Over)
		u, ok2 := parseOdds(l.Under)
		if !ok1 || !ok2 {
			continue
		}
		cost := o.Sub(totalsTarget).Abs().Add(u.Sub(totalsTarget).Abs())
		if !ok || cost.LessThan(bestCost) {
			line, over, under, bestCost, ok = *l.Hdp, o, u, cost, true
		}
	}
	return line, over, under, ok
}

// parseOdds разбирает строковый коэффициент в decimal и проверяет > 1.0
// (требование CHECK в outcomes). Пустая/некорректная строка → ok=false.
func parseOdds(s string) (decimal.Decimal, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return decimal.Decimal{}, false
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Decimal{}, false
	}
	if !d.GreaterThan(oneDecimal) {
		return decimal.Decimal{}, false
	}
	return d, true
}

// mlLabel формирует отображаемое имя исхода ML по названиям команд события.
func mlLabel(code domain.OutcomeCode, ev domain.Event) string {
	switch code {
	case domain.OutcomeHome:
		if ev.Home != nil && *ev.Home != "" {
			return *ev.Home
		}
		return "П1"
	case domain.OutcomeAway:
		if ev.Away != nil && *ev.Away != "" {
			return *ev.Away
		}
		return "П2"
	case domain.OutcomeDraw:
		return "Ничья"
	default:
		return string(code)
	}
}

// totalsLabel формирует отображаемое имя исхода тотала с линией.
func totalsLabel(code domain.OutcomeCode, line decimal.Decimal) string {
	if code == domain.OutcomeOver {
		return "Больше " + line.String()
	}
	return "Меньше " + line.String()
}
