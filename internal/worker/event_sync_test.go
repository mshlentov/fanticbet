package worker

import (
	"context"
	"testing"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/repository"
)

// fakeEventRepo — минимальный фейк EventRepository для EventSync (нужен Upsert).
type fakeEventRepo struct {
	upserts []domain.Event
	nextID  int64
	forOdds []domain.Event
}

func (f *fakeEventRepo) Upsert(_ context.Context, e domain.Event) (int64, error) {
	f.nextID++
	f.upserts = append(f.upserts, e)
	return f.nextID, nil
}
func (f *fakeEventRepo) GetByID(context.Context, int64) (domain.Event, error) {
	return domain.Event{}, nil
}
func (f *fakeEventRepo) ListWithFilters(context.Context, repository.EventFilter) ([]domain.Event, error) {
	return nil, nil
}
func (f *fakeEventRepo) ListForOddsSync(context.Context, time.Duration) ([]domain.Event, error) {
	return f.forOdds, nil
}
func (f *fakeEventRepo) ListForSettlement(context.Context) ([]domain.Event, error) { return nil, nil }
func (f *fakeEventRepo) UpdateStatusAndScores(context.Context, int64, domain.EventStatus, []byte) error {
	return nil
}

// fakeEventsClient возвращает заранее заданные события по спорту.
type fakeEventsClient struct {
	bySport map[string][]domain.Event
	calls   []string
}

func (c *fakeEventsClient) GetEvents(_ context.Context, sport string, _ []string) ([]domain.Event, error) {
	c.calls = append(c.calls, sport)
	return c.bySport[sport], nil
}

func extID(v int64) *int64 { return &v }

func TestEventSync_UpsertsAndCreatesMarkets(t *testing.T) {
	er := &fakeEventRepo{}
	mr := newFakeMarketRepo()
	client := &fakeEventsClient{bySport: map[string][]domain.Event{
		"football": {
			{ExternalID: extID(1), SportSlug: "football", Title: "A — B"},
			{ExternalID: extID(2), SportSlug: "football", Title: "C — D"},
		},
	}}

	w := NewEventSyncWorker(er, mr, client, []string{"football"}, quietLogger())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(er.upserts) != 2 {
		t.Fatalf("upserts = %d, want 2", len(er.upserts))
	}
	// На каждое из 2 событий — по 2 рынка (ML + TOTALS).
	if len(mr.created) != 4 {
		t.Fatalf("created markets = %d, want 4", len(mr.created))
	}
	var ml, totals int
	for _, m := range mr.created {
		switch m.Type {
		case domain.MarketML:
			ml++
		case domain.MarketTotals:
			totals++
		}
		if m.Status != domain.MarketOpen {
			t.Errorf("новый рынок должен быть open, got %s", m.Status)
		}
	}
	if ml != 2 || totals != 2 {
		t.Errorf("ml=%d totals=%d, want 2/2", ml, totals)
	}
}

func TestEventSync_ExistingMarketsNotDuplicated(t *testing.T) {
	er := &fakeEventRepo{}
	mr := newFakeMarketRepo()
	// Событие получит id=1 (nextID), у которого уже есть оба рынка.
	mr.byEvent[1] = []domain.Market{
		{ID: 10, EventID: 1, Type: domain.MarketML, Status: domain.MarketOpen},
		{ID: 11, EventID: 1, Type: domain.MarketTotals, Status: domain.MarketSuspended},
	}
	client := &fakeEventsClient{bySport: map[string][]domain.Event{
		"football": {{ExternalID: extID(1), SportSlug: "football"}},
	}}

	w := NewEventSyncWorker(er, mr, client, []string{"football"}, quietLogger())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(mr.created) != 0 {
		t.Errorf("рынки не должны пересоздаваться, created=%d", len(mr.created))
	}
}
