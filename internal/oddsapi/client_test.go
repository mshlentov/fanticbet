package oddsapi

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"fanticbet/internal/domain"
)

// newTestClient поднимает httptest-сервер с заданным handler и возвращает
// клиент, настроенный на него: тихий логгер и короткий backoff, чтобы ретраи
// в тестах не тормозили.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return New("test-key",
		WithBaseURL(srv.URL),
		WithLogger(log.New(io.Discard, "", 0)),
		WithRetryBackoff(time.Millisecond),
	)
}

func TestGetSports(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("apiKey"); got != "test-key" {
			t.Errorf("apiKey = %q, want test-key", got)
		}
		_, _ = io.WriteString(w, `[{"name":"Football","slug":"football"},{"name":"Basketball","slug":"basketball"}]`)
	})

	sports, err := c.GetSports(context.Background())
	if err != nil {
		t.Fatalf("GetSports: %v", err)
	}
	if len(sports) != 2 {
		t.Fatalf("len(sports) = %d, want 2", len(sports))
	}
	if sports[0].Slug != "football" || sports[1].Name != "Basketball" {
		t.Errorf("unexpected sports: %+v", sports)
	}
}

func TestGetEvents_MappingAndParams(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/events" {
			t.Errorf("path = %q, want /events", got)
		}
		if got := r.URL.Query().Get("sport"); got != "football" {
			t.Errorf("sport = %q, want football", got)
		}
		if got := r.URL.Query().Get("status"); got != "pending,live" {
			t.Errorf("status = %q, want pending,live", got)
		}
		_, _ = io.WriteString(w, `[
			{
				"id": 123456,
				"home": "Manchester United",
				"away": "Liverpool",
				"date": "2025-10-15T15:00:00Z",
				"status": "pending",
				"sport": {"name":"Football","slug":"football"},
				"league": {"name":"England - Premier League","slug":"england-premier-league"},
				"scores": {"home":2,"away":1}
			}
		]`)
	})

	events, err := c.GetEvents(context.Background(), "football", []string{"pending", "live"})
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}

	ev := events[0]
	if ev.Source != domain.SourceOddsAPI {
		t.Errorf("Source = %q, want oddsapi", ev.Source)
	}
	if ev.ExternalID == nil || *ev.ExternalID != 123456 {
		t.Errorf("ExternalID = %v, want 123456", ev.ExternalID)
	}
	if ev.Status != domain.EventUpcoming {
		t.Errorf("Status = %q, want upcoming (mapped from pending)", ev.Status)
	}
	if ev.Title != "Manchester United — Liverpool" {
		t.Errorf("Title = %q", ev.Title)
	}
	if ev.LeagueName == nil || *ev.LeagueName != "England - Premier League" {
		t.Errorf("LeagueName = %v", ev.LeagueName)
	}
	wantStart := time.Date(2025, 10, 15, 15, 0, 0, 0, time.UTC)
	if !ev.StartsAt.Equal(wantStart) {
		t.Errorf("StartsAt = %v, want %v", ev.StartsAt, wantStart)
	}
	if len(ev.Scores) == 0 {
		t.Errorf("Scores not preserved")
	}
}

func TestGetEvents_NoStatusOmitsParam(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.URL.Query()["status"]; ok {
			t.Errorf("status param must be omitted when statuses is nil")
		}
		_, _ = io.WriteString(w, `[]`)
	})

	if _, err := c.GetEvents(context.Background(), "football", nil); err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
}

func TestGetEvent(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/events/777" {
			t.Errorf("path = %q, want /events/777", got)
		}
		_, _ = io.WriteString(w, `{
			"id": 777,
			"home": "A",
			"away": "B",
			"date": "2025-01-01T00:00:00Z",
			"status": "settled",
			"sport": {"name":"Football","slug":"football"},
			"scores": {"home":0,"away":0}
		}`)
	})

	ev, err := c.GetEvent(context.Background(), 777)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if ev.Status != domain.EventSettled {
		t.Errorf("Status = %q, want settled", ev.Status)
	}
	if ev.ExternalID == nil || *ev.ExternalID != 777 {
		t.Errorf("ExternalID = %v, want 777", ev.ExternalID)
	}
}

func TestGetOddsMulti_Parsing(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/odds/multi" {
			t.Errorf("path = %q, want /odds/multi", got)
		}
		if got := r.URL.Query().Get("eventIds"); got != "1,2" {
			t.Errorf("eventIds = %q, want 1,2", got)
		}
		if got := r.URL.Query().Get("bookmakers"); got != "Pinnacle" {
			t.Errorf("bookmakers = %q, want Pinnacle", got)
		}
		_, _ = io.WriteString(w, `[
			{
				"id": 1,
				"status": "pending",
				"bookmakers": {
					"Pinnacle": [
						{"name":"ML","updatedAt":"2025-10-04T10:30:00Z","odds":[{"home":"2.10","draw":"3.40","away":"3.20"}]},
						{"name":"Totals","updatedAt":"2025-10-04T10:30:00Z","odds":[{"hdp":2.5,"over":"1.90","under":"1.90"}]}
					]
				}
			},
			{"id": 2, "status": "pending", "bookmakers": {}}
		]`)
	})

	res, err := c.GetOddsMulti(context.Background(), []int64{1, 2}, []string{"Pinnacle"})
	if err != nil {
		t.Fatalf("GetOddsMulti: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("len(res) = %d, want 2", len(res))
	}

	markets := res[0].Bookmakers["Pinnacle"]
	if len(markets) != 2 {
		t.Fatalf("len(markets) = %d, want 2", len(markets))
	}

	ml := markets[0]
	if ml.Name != "ML" || len(ml.Lines) != 1 {
		t.Fatalf("unexpected ML market: %+v", ml)
	}
	if ml.Lines[0].Home != "2.10" || ml.Lines[0].Away != "3.20" || ml.Lines[0].Draw != "3.40" {
		t.Errorf("ML odds = %+v", ml.Lines[0])
	}
	if ml.Lines[0].Hdp != nil {
		t.Errorf("ML Hdp should be nil, got %v", ml.Lines[0].Hdp)
	}

	totals := markets[1]
	if totals.Name != "Totals" || len(totals.Lines) != 1 {
		t.Fatalf("unexpected Totals market: %+v", totals)
	}
	line := totals.Lines[0]
	if line.Hdp == nil || line.Hdp.String() != "2.5" {
		t.Errorf("Totals Hdp = %v, want 2.5", line.Hdp)
	}
	if line.Over != "1.90" || line.Under != "1.90" {
		t.Errorf("Totals odds = %+v", line)
	}
}

func TestGetOddsMulti_TooManyEvents(t *testing.T) {
	c := New("k", WithLogger(log.New(io.Discard, "", 0)))
	ids := make([]int64, maxMultiEvents+1)
	if _, err := c.GetOddsMulti(context.Background(), ids, []string{"Pinnacle"}); err == nil {
		t.Fatal("expected error for >10 events, got nil")
	}
}

func TestGetOddsMulti_EmptyEventsNoCall(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("server must not be called for empty eventIDs")
	})
	res, err := c.GetOddsMulti(context.Background(), nil, []string{"Pinnacle"})
	if err != nil {
		t.Fatalf("GetOddsMulti: %v", err)
	}
	if res != nil {
		t.Errorf("res = %v, want nil", res)
	}
}

func TestDo_RetriesOn5xxThenSucceeds(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		w.Header().Set("x-ratelimit-remaining", "4999")
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, `[]`)
	})

	if _, err := c.GetSports(context.Background()); err != nil {
		t.Fatalf("GetSports: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls = %d, want 3 (2 failures + 1 success)", got)
	}
}

func TestDo_NoRetryOn4xx(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `bad request`)
	})

	_, err := c.GetSports(context.Background())
	if err == nil {
		t.Fatal("expected error on 400")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (no retry on 4xx)", got)
	}
}

func TestDo_ContextCancelled(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // отменяем заранее

	if _, err := c.GetSports(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
