package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"fanticbet/internal/domain"
)

// --- фейки для SettlementWorker ---

// settleCall — запись одного вызова SettleEvent в фейк-runner'е.
type settleCall struct {
	EventID int64
	Status  domain.EventStatus
	Scores  json.RawMessage
}

// fakeSettlementRunner записывает все вызовы SettleEvent и опционально возвращает
// ошибку по заданному event_id (для проверки обработки ошибок).
type fakeSettlementRunner struct {
	calls  []settleCall
	errOn  map[int64]error // event_id → возвращаемая ошибка (если задано)
}

func (f *fakeSettlementRunner) SettleEvent(_ context.Context, eventID int64, status domain.EventStatus, scores json.RawMessage) error {
	f.calls = append(f.calls, settleCall{EventID: eventID, Status: status, Scores: scores})
	if f.errOn != nil {
		if err, ok := f.errOn[eventID]; ok {
			return err
		}
	}
	return nil
}

// fakeSettlementClient возвращает заранее заданное событие по external_id,
// либо ошибку (для проверки устойчивости воркера к сбоям API).
type fakeSettlementClient struct {
	byExt map[int64]domain.Event // external_id → событие из API (со статусом/scores)
	errOn map[int64]error        // external_id → ошибка GetEvent
}

func (c *fakeSettlementClient) GetEvent(_ context.Context, externalID int64) (domain.Event, error) {
	if c.errOn != nil {
		if err, ok := c.errOn[externalID]; ok {
			return domain.Event{}, err
		}
	}
	if ev, ok := c.byExt[externalID]; ok {
		return ev, nil
	}
	return domain.Event{}, errors.New("not found")
}

func extIDPtr(v int64) *int64 { return &v }

// --- тесты ---

// TestSettlement_Run_SettlesFinishedEvents: API отдаёт settled/cancelled — воркер
// вызывает SettleEvent с правильным статусом и scores для каждого.
func TestSettlement_Run_SettlesFinishedEvents(t *testing.T) {
	repo := &fakeEventRepo{
		forSettlement: []domain.Event{
			{ID: 1, ExternalID: extIDPtr(100)},
			{ID: 2, ExternalID: extIDPtr(200)},
		},
	}
	scores100 := json.RawMessage(`{"home":2,"away":1}`)
	client := &fakeSettlementClient{byExt: map[int64]domain.Event{
		100: {Status: domain.EventSettled, Scores: scores100},
		200: {Status: domain.EventCancelled, Scores: nil},
	}}
	runner := &fakeSettlementRunner{}

	w := NewSettlementWorker(repo, client, runner, quietLogger())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(runner.calls) != 2 {
		t.Fatalf("SettleEvent calls = %d, want 2", len(runner.calls))
	}

	// Событие 1 → settled со scores.
	var c1, c2 *settleCall
	for i := range runner.calls {
		switch runner.calls[i].EventID {
		case 1:
			c1 = &runner.calls[i]
		case 2:
			c2 = &runner.calls[i]
		}
	}
	if c1 == nil || c1.Status != domain.EventSettled {
		t.Errorf("event 1 call = %+v, want settled", c1)
	}
	if c1 != nil && string(c1.Scores) != string(scores100) {
		t.Errorf("event 1 scores = %s, want %s", c1.Scores, scores100)
	}
	// Событие 2 → cancelled.
	if c2 == nil || c2.Status != domain.EventCancelled {
		t.Errorf("event 2 call = %+v, want cancelled", c2)
	}
}

// TestSettlement_Run_SkipsStillLive: API отдаёт live/upcoming — SettleEvent не
// вызывается (матч ещё идёт, ждём следующий прогон).
func TestSettlement_Run_SkipsStillLive(t *testing.T) {
	repo := &fakeEventRepo{
		forSettlement: []domain.Event{
			{ID: 1, ExternalID: extIDPtr(100)},
		},
	}
	client := &fakeSettlementClient{byExt: map[int64]domain.Event{
		100: {Status: domain.EventLive, Scores: json.RawMessage(`{"home":1,"away":0}`)},
	}}
	runner := &fakeSettlementRunner{}

	w := NewSettlementWorker(repo, client, runner, quietLogger())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Errorf("SettleEvent calls = %d, want 0 for live event", len(runner.calls))
	}
}

// TestSettlement_Run_APIErrorContinues: ошибка GetEvent по одному событию не
// роняет итерацию — остальные события обрабатываются.
func TestSettlement_Run_APIErrorContinues(t *testing.T) {
	repo := &fakeEventRepo{
		forSettlement: []domain.Event{
			{ID: 1, ExternalID: extIDPtr(100)}, // упадёт
			{ID: 2, ExternalID: extIDPtr(200)}, // обработается
		},
	}
	client := &fakeSettlementClient{
		byExt: map[int64]domain.Event{
			200: {Status: domain.EventSettled, Scores: json.RawMessage(`{"home":0,"away":0}`)},
		},
		errOn: map[int64]error{100: errors.New("api timeout")},
	}
	runner := &fakeSettlementRunner{}

	w := NewSettlementWorker(repo, client, runner, quietLogger())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run must not fail on single-event API error: %v", err)
	}
	// Второе событие должно было обработаться, несмотря на сбой по первому.
	if len(runner.calls) != 1 || runner.calls[0].EventID != 2 {
		t.Errorf("calls = %+v, want exactly one call for event 2", runner.calls)
	}
}

// TestSettlement_Run_SettleErrorContinues: ошибка из SettleEvent (например,
// ErrScoresUnavailable) по одному событию не роняет итерацию.
func TestSettlement_Run_SettleErrorContinues(t *testing.T) {
	repo := &fakeEventRepo{
		forSettlement: []domain.Event{
			{ID: 1, ExternalID: extIDPtr(100)},
			{ID: 2, ExternalID: extIDPtr(200)},
		},
	}
	client := &fakeSettlementClient{byExt: map[int64]domain.Event{
		100: {Status: domain.EventSettled, Scores: json.RawMessage(`{"home":1,"away":1}`)},
		200: {Status: domain.EventSettled, Scores: json.RawMessage(`{"home":2,"away":0}`)},
	}}
	runner := &fakeSettlementRunner{errOn: map[int64]error{1: errors.New("scores unavailable")}}

	w := NewSettlementWorker(repo, client, runner, quietLogger())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run must not fail on single SettleEvent error: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Errorf("SettleEvent should still be attempted for both events, calls = %d", len(runner.calls))
	}
}

// TestSettlement_Run_NoEvents: пустая выборка — SettleEvent не вызывается, nil-ошибка.
func TestSettlement_Run_NoEvents(t *testing.T) {
	repo := &fakeEventRepo{}
	client := &fakeSettlementClient{}
	runner := &fakeSettlementRunner{}

	w := NewSettlementWorker(repo, client, runner, quietLogger())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Errorf("SettleEvent calls = %d, want 0", len(runner.calls))
	}
}

// TestSettlement_Run_SkipsCustomEvents: событие без external_id (custom) —
// пропускается, SettleEvent не вызывается (custom-события рассчитывает админ в M6).
func TestSettlement_Run_SkipsCustomEvents(t *testing.T) {
	repo := &fakeEventRepo{
		forSettlement: []domain.Event{
			{ID: 1, ExternalID: nil, Source: domain.SourceCustom},
		},
	}
	client := &fakeSettlementClient{}
	runner := &fakeSettlementRunner{}

	w := NewSettlementWorker(repo, client, runner, quietLogger())
	if err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Errorf("custom event must be skipped, calls = %d", len(runner.calls))
	}
}
