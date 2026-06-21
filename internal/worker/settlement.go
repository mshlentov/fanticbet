package worker

import (
	"context"
	"encoding/json"
	"log"

	"fanticbet/internal/domain"
	"fanticbet/internal/repository"
)

// SettlementEventsClient — то, что SettlementWorker берёт у клиента Odds-API:
// только чтение финального статуса и scores одного события по его external_id.
// Узкий интерфейс упрощает тестирование (подменяется фейком).
type SettlementEventsClient interface {
	GetEvent(ctx context.Context, externalID int64) (domain.Event, error)
}

// SettlementRunner — то, что SettlementWorker вызывает для расчёта события.
// Ему удовлетворует *service.SettlementService, но сам интерфейс живёт здесь,
// чтобы пакет worker не зависел от пакета service (тестируемость без цикла).
type SettlementRunner interface {
	SettleEvent(ctx context.Context, eventID int64, status domain.EventStatus, scores json.RawMessage) error
}

// SettlementWorker рассчитывает завершённые oddsapi-события. Каждую итерацию:
// выбирает начавшиеся, но ещё не расчитанные события (ListForSettlement), для
// каждого запрашивает у Odds-API финальный статус/scores и при settled/cancelled
// передаёт событие в сервис расчёта (см. architecture.md §5).
type SettlementWorker struct {
	events repository.EventRepository
	client SettlementEventsClient
	settle SettlementRunner
	logger *log.Logger
}

// NewSettlementWorker собирает воркер. logger=nil → log.Default().
func NewSettlementWorker(
	events repository.EventRepository,
	client SettlementEventsClient,
	settle SettlementRunner,
	logger *log.Logger,
) *SettlementWorker {
	if logger == nil {
		logger = log.Default()
	}
	return &SettlementWorker{
		events: events,
		client: client,
		settle: settle,
		logger: logger,
	}
}

// Run выполняет одну итерацию расчёта. Ошибка по одному событию (сбой API,
// невалидный scores и т.п.) не прерывает остальные — логируем и продолжаем,
// чтобы временный сбой по одному матчу не ронял всю итерацию (как в EventSync).
func (w *SettlementWorker) Run(ctx context.Context) error {
	evs, err := w.events.ListForSettlement(ctx)
	if err != nil {
		return err
	}
	if len(evs) == 0 {
		w.logger.Printf("Settlement: no events to check")
		return nil
	}

	var settled, cancelled int
	for _, ev := range evs {
		if err := ctx.Err(); err != nil {
			return err
		}
		// ListForSettlement возвращает только oddsapi-события, но на всякий случай
		// страхуемся: custom-события рассчитываются админом (M6), а не этим воркером.
		if ev.ExternalID == nil {
			continue
		}

		outcome, err := w.processEvent(ctx, ev)
		if err != nil {
			w.logger.Printf("Settlement: event_id=%d external_id=%d: %v", ev.ID, *ev.ExternalID, err)
			continue
		}
		switch outcome {
		case domain.EventSettled:
			settled++
		case domain.EventCancelled:
			cancelled++
		}
	}

	w.logger.Printf("Settlement: checked %d events (settled=%d cancelled=%d)",
		len(evs), settled, cancelled)
	return nil
}

// processEvent запрашивает финальное состояние события у API и при его
// завершении/отмене запускает расчёт. Возвращает итоговый статус, если расчёт
// был выполнен (для счётчика в логе), иначе пустую строку (live/upcoming).
func (w *SettlementWorker) processEvent(ctx context.Context, ev domain.Event) (domain.EventStatus, error) {
	api, err := w.client.GetEvent(ctx, *ev.ExternalID)
	if err != nil {
		return "", err
	}

	switch api.Status {
	case domain.EventSettled:
		// scores обязателен для settled; если сервис сочтёт его невалидным —
		// он вернёт ErrScoresUnavailable, мы залогируем и пропустим до след. прогона.
		if err := w.settle.SettleEvent(ctx, ev.ID, domain.EventSettled, api.Scores); err != nil {
			return "", err
		}
		w.logger.Printf("Settlement: event_id=%d settled", ev.ID)
		return domain.EventSettled, nil
	case domain.EventCancelled:
		// cancelled — scores не нужен, сервис возвращает всем ставки (void).
		if err := w.settle.SettleEvent(ctx, ev.ID, domain.EventCancelled, api.Scores); err != nil {
			return "", err
		}
		w.logger.Printf("Settlement: event_id=%d cancelled (refunded)", ev.ID)
		return domain.EventCancelled, nil
	default:
		// live/upcoming — матч ещё идёт или не начался; ждём следующий прогон.
		return "", nil
	}
}
