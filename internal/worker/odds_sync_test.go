package worker

import (
	"context"
	"io"
	"log"
	"testing"

	"fanticbet/internal/domain"
	"fanticbet/internal/oddsapi"

	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func decp(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}
func strp(s string) *string { return &s }

// --- фейковые репозитории (реализуют интерфейсы repository) ---

type fakeMarketRepo struct {
	byEvent       map[int64][]domain.Market
	statusUpdates map[int64]domain.MarketStatus
	lineUpdates   map[int64]*decimal.Decimal
	created       []domain.Market
}

func newFakeMarketRepo() *fakeMarketRepo {
	return &fakeMarketRepo{
		byEvent:       map[int64][]domain.Market{},
		statusUpdates: map[int64]domain.MarketStatus{},
		lineUpdates:   map[int64]*decimal.Decimal{},
	}
}

func (f *fakeMarketRepo) CreateForEvent(_ context.Context, m domain.Market) (int64, error) {
	m.ID = int64(100 + len(f.created))
	f.created = append(f.created, m)
	f.byEvent[m.EventID] = append(f.byEvent[m.EventID], m)
	return m.ID, nil
}
func (f *fakeMarketRepo) GetByEvent(_ context.Context, eventID int64) ([]domain.Market, error) {
	return f.byEvent[eventID], nil
}
func (f *fakeMarketRepo) UpdateStatus(_ context.Context, id int64, status domain.MarketStatus) error {
	f.statusUpdates[id] = status
	return nil
}
func (f *fakeMarketRepo) UpdateLine(_ context.Context, id int64, line *decimal.Decimal) error {
	f.lineUpdates[id] = line
	return nil
}

type fakeOutcomeRepo struct {
	upserts []domain.Outcome
}

func (f *fakeOutcomeRepo) Upsert(_ context.Context, o domain.Outcome) (int64, error) {
	f.upserts = append(f.upserts, o)
	return int64(len(f.upserts)), nil
}
func (f *fakeOutcomeRepo) GetByMarket(context.Context, int64) ([]domain.Outcome, error) {
	return nil, nil
}
func (f *fakeOutcomeRepo) UpdateOdds(context.Context, int64, decimal.Decimal) error { return nil }
func (f *fakeOutcomeRepo) UpdateResult(context.Context, int64, domain.Result) error { return nil }

func (f *fakeOutcomeRepo) byCode(code domain.OutcomeCode) (domain.Outcome, bool) {
	for _, o := range f.upserts {
		if o.Code == code {
			return o, true
		}
	}
	return domain.Outcome{}, false
}

// quietLogger — логгер в /dev/null, чтобы не засорять вывод тестов.
func quietLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// --- тесты чистых помощников ---

func TestParseOdds(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"2.10", true},
		{" 1.90 ", true},
		{"", false},
		{"abc", false},
		{"1.0", false},  // не > 1.0
		{"1.00", false}, // не > 1.0
		{"0.50", false},
	}
	for _, tt := range tests {
		_, ok := parseOdds(tt.in)
		if ok != tt.want {
			t.Errorf("parseOdds(%q) ok=%v, want %v", tt.in, ok, tt.want)
		}
	}
}

func TestMarketNameDetection(t *testing.T) {
	if !isMLName("ML") || !isMLName("Moneyline") || !isMLName("1x2") {
		t.Error("isMLName: ожидалось распознавание ML-имён")
	}
	if isMLName("Totals") {
		t.Error("isMLName: Totals не должен распознаваться как ML")
	}
	if !isTotalsName("Totals") || !isTotalsName("Over/Under") {
		t.Error("isTotalsName: ожидалось распознавание тоталов")
	}
	if isTotalsName("ML") {
		t.Error("isTotalsName: ML не должен распознаваться как тотал")
	}
}

func TestPickMainTotalsLine(t *testing.T) {
	m := oddsapi.Market{Name: "Totals", Lines: []oddsapi.OddsLine{
		{Hdp: decp("2.0"), Over: "1.50", Under: "2.60"}, // несбалансированная
		{Hdp: decp("2.5"), Over: "1.90", Under: "1.92"}, // ближе всего к 1.90/1.90
		{Hdp: decp("3.0"), Over: "2.40", Under: "1.55"},
		{Hdp: nil, Over: "1.90", Under: "1.90"}, // без линии — пропускается
	}}

	line, over, under, ok := pickMainTotalsLine(m)
	if !ok {
		t.Fatal("ожидалось ok=true")
	}
	if line.String() != "2.5" {
		t.Errorf("line = %s, want 2.5", line.String())
	}
	if over.String() != "1.9" || under.String() != "1.92" {
		t.Errorf("over/under = %s/%s, want 1.9/1.92", over.String(), under.String())
	}
}

func TestPickMainTotalsLine_NoValidLines(t *testing.T) {
	m := oddsapi.Market{Name: "Totals", Lines: []oddsapi.OddsLine{
		{Hdp: decp("2.5"), Over: "", Under: ""},
		{Hdp: nil, Over: "1.90", Under: "1.90"},
	}}
	if _, _, _, ok := pickMainTotalsLine(m); ok {
		t.Error("ожидалось ok=false при отсутствии валидных линий")
	}
}

func TestPickMLLine_SkipsInvalid(t *testing.T) {
	m := oddsapi.Market{Name: "ML", Lines: []oddsapi.OddsLine{
		{Home: "", Away: "1.50"},                   // нет home — пропуск
		{Home: "2.10", Draw: "3.40", Away: "3.20"}, // валидная
	}}
	l, ok := pickMLLine(m)
	if !ok || l.Home != "2.10" {
		t.Fatalf("pickMLLine = %+v ok=%v", l, ok)
	}
}

// --- тесты OddsSyncWorker.applyEvent с фейками ---

// makeEvent создаёт событие с ML и TOTALS рынками (как после EventSync).
func makeEvent(mr *fakeMarketRepo) domain.Event {
	ev := domain.Event{ID: 1, ExternalID: func() *int64 { v := int64(555); return &v }(), Home: strp("Home FC"), Away: strp("Away FC")}
	mr.byEvent[1] = []domain.Market{
		{ID: 10, EventID: 1, Type: domain.MarketML, Status: domain.MarketOpen},
		{ID: 11, EventID: 1, Type: domain.MarketTotals, Status: domain.MarketOpen},
	}
	return ev
}

func newOddsWorker(mr *fakeMarketRepo, or *fakeOutcomeRepo) *OddsSyncWorker {
	return NewOddsSyncWorker(nil, mr, or, nil, "pinnacle", 0, quietLogger())
}

func TestApplyEvent_FullOdds(t *testing.T) {
	mr := newFakeMarketRepo()
	or := &fakeOutcomeRepo{}
	w := newOddsWorker(mr, or)
	ev := makeEvent(mr)

	eo := oddsapi.EventOdds{EventID: 555, Bookmakers: map[string][]oddsapi.Market{
		"Pinnacle": { // регистр отличается от конфига "pinnacle" — должен найтись
			{Name: "ML", Lines: []oddsapi.OddsLine{{Home: "2.10", Draw: "3.40", Away: "3.20"}}},
			{Name: "Totals", Lines: []oddsapi.OddsLine{{Hdp: decp("2.5"), Over: "1.90", Under: "1.92"}}},
		},
	}}

	if err := w.applyEvent(context.Background(), ev, eo, true); err != nil {
		t.Fatalf("applyEvent: %v", err)
	}

	// ML: home/draw/away.
	for _, code := range []domain.OutcomeCode{domain.OutcomeHome, domain.OutcomeDraw, domain.OutcomeAway} {
		if _, ok := or.byCode(code); !ok {
			t.Errorf("ожидался upsert исхода %s", code)
		}
	}
	if o, _ := or.byCode(domain.OutcomeHome); o.Label != "Home FC" {
		t.Errorf("label home = %q, want Home FC", o.Label)
	}
	// TOTALS: линия выставлена, over/under добавлены.
	if l := mr.lineUpdates[11]; l == nil || l.String() != "2.5" {
		t.Errorf("totals line update = %v, want 2.5", l)
	}
	if _, ok := or.byCode(domain.OutcomeOver); !ok {
		t.Error("ожидался upsert over")
	}
	if _, ok := or.byCode(domain.OutcomeUnder); !ok {
		t.Error("ожидался upsert under")
	}
	// Рынки были open и остаются open → лишних UpdateStatus быть не должно.
	if len(mr.statusUpdates) != 0 {
		t.Errorf("неожиданные изменения статуса: %v", mr.statusUpdates)
	}
}

func TestApplyEvent_BookmakerMissing_Suspends(t *testing.T) {
	mr := newFakeMarketRepo()
	or := &fakeOutcomeRepo{}
	w := newOddsWorker(mr, or)
	ev := makeEvent(mr)

	// present=false (событие не вернулось вовсе).
	if err := w.applyEvent(context.Background(), ev, oddsapi.EventOdds{}, false); err != nil {
		t.Fatalf("applyEvent: %v", err)
	}
	if mr.statusUpdates[10] != domain.MarketSuspended || mr.statusUpdates[11] != domain.MarketSuspended {
		t.Errorf("ожидался suspend обоих рынков, got %v", mr.statusUpdates)
	}
	if len(or.upserts) != 0 {
		t.Errorf("исходы не должны апсёртиться при отсутствии данных, got %d", len(or.upserts))
	}
}

func TestApplyEvent_NoTotalsMarket_SuspendsTotalsOnly(t *testing.T) {
	mr := newFakeMarketRepo()
	or := &fakeOutcomeRepo{}
	w := newOddsWorker(mr, or)
	ev := makeEvent(mr)

	eo := oddsapi.EventOdds{EventID: 555, Bookmakers: map[string][]oddsapi.Market{
		"pinnacle": {
			{Name: "ML", Lines: []oddsapi.OddsLine{{Home: "1.80", Away: "2.00"}}}, // без draw — ок
		},
	}}

	if err := w.applyEvent(context.Background(), ev, eo, true); err != nil {
		t.Fatalf("applyEvent: %v", err)
	}
	// TOTALS снят, ML остаётся open.
	if mr.statusUpdates[11] != domain.MarketSuspended {
		t.Errorf("ожидался suspend TOTALS, got %v", mr.statusUpdates)
	}
	if _, ok := mr.statusUpdates[10]; ok {
		t.Errorf("ML не должен менять статус, got %v", mr.statusUpdates[10])
	}
	// ML: home/away есть, draw нет.
	if _, ok := or.byCode(domain.OutcomeDraw); ok {
		t.Error("draw не должен апсёртиться, когда его нет в котировках")
	}
	if _, ok := or.byCode(domain.OutcomeHome); !ok {
		t.Error("ожидался upsert home")
	}
}
