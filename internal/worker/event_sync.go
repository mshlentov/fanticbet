package worker

import (
	"context"
	"log"

	"fanticbet/internal/domain"
	"fanticbet/internal/repository"
)

// EventsClient — то, что EventSyncWorker берёт у клиента Odds-API. Узкий
// интерфейс упрощает тестирование (подменяется фейком).
type EventsClient interface {
	GetEvents(ctx context.Context, sport string, statuses []string) ([]domain.Event, error)
}

// eventSyncStatuses — статусы, которые запрашиваем у API: ещё не начавшиеся
// (pending) и идущие (live). Settled/cancelled обрабатывает SettlementWorker (M3).
var eventSyncStatuses = []string{"pending", "live"}

// EventSyncWorker синхронизирует список событий из Odds-API в локальную БД.
// Для каждого спорта из конфига: тянет события, upsert-ит их (попутно переводя
// upcoming→live по статусу API), а для новых заводит рынки ML и TOTALS с пустыми
// исходами — наполнит их позже OddsSyncWorker.
type EventSyncWorker struct {
	events  repository.EventRepository
	markets repository.MarketRepository
	client  EventsClient
	sports  []string
	logger  *log.Logger
}

// NewEventSyncWorker собирает воркер. sports — список sport_slug из конфига.
func NewEventSyncWorker(
	events repository.EventRepository,
	markets repository.MarketRepository,
	client EventsClient,
	sports []string,
	logger *log.Logger,
) *EventSyncWorker {
	if logger == nil {
		logger = log.Default()
	}
	return &EventSyncWorker{
		events:  events,
		markets: markets,
		client:  client,
		sports:  sports,
		logger:  logger,
	}
}

// Run выполняет одну итерацию синхронизации событий. Ошибка по одному спорту или
// событию не прерывает остальные — логируем и продолжаем, чтобы временный сбой
// API по одному виду спорта не ронял всю итерацию.
func (w *EventSyncWorker) Run(ctx context.Context) error {
	var upserted, created int
	for _, sport := range w.sports {
		if err := ctx.Err(); err != nil {
			return err
		}

		evs, err := w.client.GetEvents(ctx, sport, eventSyncStatuses)
		if err != nil {
			w.logger.Printf("EventSync: sport=%s GetEvents: %v", sport, err)
			continue
		}

		for _, ev := range evs {
			id, err := w.events.Upsert(ctx, ev)
			if err != nil {
				w.logger.Printf("EventSync: sport=%s external_id=%v upsert: %v", sport, ev.ExternalID, err)
				continue
			}
			upserted++

			n, err := w.ensureMarkets(ctx, id)
			if err != nil {
				w.logger.Printf("EventSync: event_id=%d ensureMarkets: %v", id, err)
				continue
			}
			created += n
		}
	}

	w.logger.Printf("EventSync: upserted=%d events, created=%d markets", upserted, created)
	return nil
}

// ensureMarkets гарантирует наличие рынков ML и TOTALS у события. Идемпотентно:
// если рынок уже есть — не трогаем (его статус/линию ведёт OddsSyncWorker).
// Возвращает число созданных рынков.
func (w *EventSyncWorker) ensureMarkets(ctx context.Context, eventID int64) (int, error) {
	existing, err := w.markets.GetByEvent(ctx, eventID)
	if err != nil {
		return 0, err
	}

	have := make(map[domain.MarketType]bool, len(existing))
	for _, m := range existing {
		have[m.Type] = true
	}

	created := 0
	for _, t := range []domain.MarketType{domain.MarketML, domain.MarketTotals} {
		if have[t] {
			continue
		}
		// line/question остаются NULL: линию TOTALS выставит OddsSyncWorker,
		// исходы тоже добавит он — пока рынок пустой.
		m := domain.Market{EventID: eventID, Type: t, Status: domain.MarketOpen}
		if _, err := w.markets.CreateForEvent(ctx, m); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}
