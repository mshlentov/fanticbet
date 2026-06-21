package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/repository"

	"github.com/shopspring/decimal"
)

// --- Фейки репозиториев каталога событий ---

// Поля *_calls / resultUpdates / statusUpdates добавлены для тестов settlement:
// они фиксируют, что воркер/сервис записал в репозиторий. Существующие тесты
// event_service их не проверяют, поэтому расширение обратно совместимо.

type fakeEventRepo struct {
	listFn            func(ctx context.Context, f repository.EventFilter) ([]domain.Event, error)
	getFn             func(ctx context.Context, id int64) (domain.Event, error)
	sportsFn          func(ctx context.Context) ([]string, error)
	listForSettlement func(ctx context.Context) ([]domain.Event, error)
	statusScoresCalls []eventStatusScoreCall // все вызовы UpdateStatusAndScores
}

// eventStatusScoreCall — запись одного вызова UpdateStatusAndScores.
type eventStatusScoreCall struct {
	ID     int64
	Status domain.EventStatus
	Scores []byte
}

func (m *fakeEventRepo) Upsert(context.Context, domain.Event) (int64, error) { return 0, nil }
func (m *fakeEventRepo) GetByID(ctx context.Context, id int64) (domain.Event, error) {
	return m.getFn(ctx, id)
}
func (m *fakeEventRepo) ListWithFilters(ctx context.Context, f repository.EventFilter) ([]domain.Event, error) {
	return m.listFn(ctx, f)
}
func (m *fakeEventRepo) ListSports(ctx context.Context) ([]string, error) { return m.sportsFn(ctx) }
func (m *fakeEventRepo) ListForOddsSync(context.Context, time.Duration) ([]domain.Event, error) {
	return nil, nil
}
func (m *fakeEventRepo) ListForSettlement(ctx context.Context) ([]domain.Event, error) {
	if m.listForSettlement != nil {
		return m.listForSettlement(ctx)
	}
	return nil, nil
}
func (m *fakeEventRepo) UpdateStatusAndScores(_ context.Context, id int64, status domain.EventStatus, scores []byte) error {
	m.statusScoresCalls = append(m.statusScoresCalls, eventStatusScoreCall{ID: id, Status: status, Scores: scores})
	return nil
}

type fakeMarketRepo struct {
	byEvent       map[int64][]domain.Market
	byEvents      func(ctx context.Context, ids []int64) ([]domain.Market, error)
	getByIDFn     func(ctx context.Context, id int64) (domain.Market, error)
	statusUpdates map[int64]domain.MarketStatus // market_id → новый статус (тесты settlement)
}

func (m *fakeMarketRepo) CreateForEvent(context.Context, domain.Market) (int64, error) { return 0, nil }
func (m *fakeMarketRepo) GetByID(ctx context.Context, id int64) (domain.Market, error) {
	return m.getByIDFn(ctx, id)
}
func (m *fakeMarketRepo) GetByEvent(_ context.Context, eventID int64) ([]domain.Market, error) {
	return m.byEvent[eventID], nil
}
func (m *fakeMarketRepo) GetByEvents(ctx context.Context, ids []int64) ([]domain.Market, error) {
	return m.byEvents(ctx, ids)
}
func (m *fakeMarketRepo) UpdateStatus(_ context.Context, id int64, status domain.MarketStatus) error {
	if m.statusUpdates == nil {
		m.statusUpdates = map[int64]domain.MarketStatus{}
	}
	m.statusUpdates[id] = status
	return nil
}
func (m *fakeMarketRepo) UpdateLine(context.Context, int64, *decimal.Decimal) error { return nil }

type fakeOutcomeRepo struct {
	byMarket      map[int64][]domain.Outcome
	byMarkets     func(ctx context.Context, ids []int64) ([]domain.Outcome, error)
	getByIDFn     func(ctx context.Context, id int64) (domain.Outcome, error)
	resultUpdates map[int64]domain.Result // outcome_id → результат (тесты settlement)
}

func (m *fakeOutcomeRepo) Upsert(context.Context, domain.Outcome) (int64, error) { return 0, nil }
func (m *fakeOutcomeRepo) GetByID(ctx context.Context, id int64) (domain.Outcome, error) {
	return m.getByIDFn(ctx, id)
}
func (m *fakeOutcomeRepo) GetByMarket(_ context.Context, marketID int64) ([]domain.Outcome, error) {
	return m.byMarket[marketID], nil
}
func (m *fakeOutcomeRepo) GetByMarkets(ctx context.Context, ids []int64) ([]domain.Outcome, error) {
	return m.byMarkets(ctx, ids)
}
func (m *fakeOutcomeRepo) UpdateOdds(context.Context, int64, decimal.Decimal) error { return nil }
func (m *fakeOutcomeRepo) UpdateResult(_ context.Context, id int64, result domain.Result) error {
	if m.resultUpdates == nil {
		m.resultUpdates = map[int64]domain.Result{}
	}
	m.resultUpdates[id] = result
	return nil
}

// --- Тесты ---

func TestEventService_ListSports_AppendsCustomWhenMissing(t *testing.T) {
	svc := NewEventService(
		&fakeEventRepo{sportsFn: func(context.Context) ([]string, error) {
			return []string{"basketball", "football"}, nil
		}},
		&fakeMarketRepo{}, &fakeOutcomeRepo{},
	)

	sports, err := svc.ListSports(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"basketball", "football", "custom"}
	if len(sports) != len(want) {
		t.Fatalf("got %v, want %v", sports, want)
	}
	for i := range want {
		if sports[i] != want[i] {
			t.Fatalf("got %v, want %v", sports, want)
		}
	}
}

func TestEventService_ListSports_NoDuplicateCustom(t *testing.T) {
	svc := NewEventService(
		&fakeEventRepo{sportsFn: func(context.Context) ([]string, error) {
			return []string{"custom", "football"}, nil
		}},
		&fakeMarketRepo{}, &fakeOutcomeRepo{},
	)

	sports, err := svc.ListSports(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for _, s := range sports {
		if s == "custom" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("custom must appear once, got %d in %v", count, sports)
	}
}

func TestEventService_ListEvents_AssemblesTree(t *testing.T) {
	events := []domain.Event{{ID: 1}, {ID: 2}}
	markets := []domain.Market{
		{ID: 10, EventID: 1, Type: domain.MarketML},
		{ID: 11, EventID: 1, Type: domain.MarketTotals},
		{ID: 20, EventID: 2, Type: domain.MarketML},
	}
	outcomes := []domain.Outcome{
		{ID: 100, MarketID: 10, Code: domain.OutcomeHome},
		{ID: 101, MarketID: 10, Code: domain.OutcomeAway},
		{ID: 110, MarketID: 11, Code: domain.OutcomeOver},
		{ID: 200, MarketID: 20, Code: domain.OutcomeHome},
	}

	svc := NewEventService(
		&fakeEventRepo{listFn: func(context.Context, repository.EventFilter) ([]domain.Event, error) {
			return events, nil
		}},
		&fakeMarketRepo{byEvents: func(context.Context, []int64) ([]domain.Market, error) {
			return markets, nil
		}},
		&fakeOutcomeRepo{byMarkets: func(context.Context, []int64) ([]domain.Outcome, error) {
			return outcomes, nil
		}},
	)

	got, err := svc.ListEvents(context.Background(), repository.EventFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	if len(got[0].Markets) != 2 {
		t.Fatalf("event 1: got %d markets, want 2", len(got[0].Markets))
	}
	if len(got[0].Markets[0].Outcomes) != 2 {
		t.Fatalf("event 1 market 10: got %d outcomes, want 2", len(got[0].Markets[0].Outcomes))
	}
	if len(got[1].Markets) != 1 || len(got[1].Markets[0].Outcomes) != 1 {
		t.Fatalf("event 2 assembled wrong: %+v", got[1].Markets)
	}
}

func TestEventService_ListEvents_EmptyShortCircuits(t *testing.T) {
	marketCalled := false
	svc := NewEventService(
		&fakeEventRepo{listFn: func(context.Context, repository.EventFilter) ([]domain.Event, error) {
			return nil, nil
		}},
		&fakeMarketRepo{byEvents: func(context.Context, []int64) ([]domain.Market, error) {
			marketCalled = true
			return nil, nil
		}},
		&fakeOutcomeRepo{},
	)

	got, err := svc.ListEvents(context.Background(), repository.EventFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
	if marketCalled {
		t.Fatal("markets must not be queried for empty event list")
	}
}

func TestEventService_GetEvent_NotFound(t *testing.T) {
	svc := NewEventService(
		&fakeEventRepo{getFn: func(context.Context, int64) (domain.Event, error) {
			return domain.Event{}, domain.ErrNotFound
		}},
		&fakeMarketRepo{}, &fakeOutcomeRepo{},
	)

	_, err := svc.GetEvent(context.Background(), 42)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestEventService_GetEvent_Assembles(t *testing.T) {
	svc := NewEventService(
		&fakeEventRepo{getFn: func(_ context.Context, id int64) (domain.Event, error) {
			return domain.Event{ID: id, Title: "Match"}, nil
		}},
		&fakeMarketRepo{byEvent: map[int64][]domain.Market{
			1: {{ID: 10, EventID: 1, Type: domain.MarketML}},
		}},
		&fakeOutcomeRepo{byMarket: map[int64][]domain.Outcome{
			10: {{ID: 100, MarketID: 10, Code: domain.OutcomeHome}},
		}},
	)

	got, err := svc.GetEvent(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Event.Title != "Match" {
		t.Fatalf("got title %q, want Match", got.Event.Title)
	}
	if len(got.Markets) != 1 || len(got.Markets[0].Outcomes) != 1 {
		t.Fatalf("assembled wrong: %+v", got.Markets)
	}
}
