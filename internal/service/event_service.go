package service

import (
	"context"
	"fmt"

	"fanticbet/internal/domain"
	"fanticbet/internal/repository"
)

// MarketWithOutcomes — рынок вместе с его исходами (текущие коэффициенты).
type MarketWithOutcomes struct {
	Market   domain.Market
	Outcomes []domain.Outcome
}

// EventWithMarkets — событие вместе с рынками и исходами. Готовая агрегированная
// структура для отдачи в ленте и на странице события (handler маппит её в DTO).
type EventWithMarkets struct {
	Event   domain.Event
	Markets []MarketWithOutcomes
}

// EventService — чтение каталога событий: список видов спорта, лента событий с
// рынками/коэффициентами и одно событие. Только чтение из локальной БД (данные
// наполняют воркеры EventSync/OddsSync), бизнес-правил ставок здесь нет.
type EventService struct {
	events   repository.EventRepository
	markets  repository.MarketRepository
	outcomes repository.OutcomeRepository
}

func NewEventService(
	events repository.EventRepository,
	markets repository.MarketRepository,
	outcomes repository.OutcomeRepository,
) *EventService {
	return &EventService{events: events, markets: markets, outcomes: outcomes}
}

// ListSports возвращает виды спорта, по которым есть события в БД. 'custom'
// гарантированно присутствует в списке, даже если кастомных событий ещё нет —
// это всегда доступная категория (см. tasks.md:133).
func (s *EventService) ListSports(ctx context.Context) ([]string, error) {
	slugs, err := s.events.ListSports(ctx)
	if err != nil {
		return nil, fmt.Errorf("EventService.ListSports: %w", err)
	}

	hasCustom := false
	for _, slug := range slugs {
		if slug == string(domain.SourceCustom) {
			hasCustom = true
			break
		}
	}
	if !hasCustom {
		slugs = append(slugs, string(domain.SourceCustom))
	}
	return slugs, nil
}

// ListEvents возвращает страницу ленты событий с рынками и текущими исходами.
// Рынки и исходы догружаются батч-запросами (один на рынки, один на исходы),
// чтобы не делать N+1 на странице ленты.
func (s *EventService) ListEvents(ctx context.Context, f repository.EventFilter) ([]EventWithMarkets, error) {
	events, err := s.events.ListWithFilters(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("EventService.ListEvents: %w", err)
	}
	if len(events) == 0 {
		return nil, nil
	}

	eventIDs := make([]int64, 0, len(events))
	for _, e := range events {
		eventIDs = append(eventIDs, e.ID)
	}

	markets, err := s.markets.GetByEvents(ctx, eventIDs)
	if err != nil {
		return nil, fmt.Errorf("EventService.ListEvents markets: %w", err)
	}

	marketIDs := make([]int64, 0, len(markets))
	for _, m := range markets {
		marketIDs = append(marketIDs, m.ID)
	}

	outcomes, err := s.outcomes.GetByMarkets(ctx, marketIDs)
	if err != nil {
		return nil, fmt.Errorf("EventService.ListEvents outcomes: %w", err)
	}

	// Группируем исходы по market_id и рынки по event_id, затем собираем дерево.
	outcomesByMarket := make(map[int64][]domain.Outcome, len(marketIDs))
	for _, o := range outcomes {
		outcomesByMarket[o.MarketID] = append(outcomesByMarket[o.MarketID], o)
	}
	marketsByEvent := make(map[int64][]MarketWithOutcomes, len(eventIDs))
	for _, m := range markets {
		marketsByEvent[m.EventID] = append(marketsByEvent[m.EventID], MarketWithOutcomes{
			Market:   m,
			Outcomes: outcomesByMarket[m.ID],
		})
	}

	result := make([]EventWithMarkets, 0, len(events))
	for _, e := range events {
		result = append(result, EventWithMarkets{
			Event:   e,
			Markets: marketsByEvent[e.ID],
		})
	}
	return result, nil
}

// GetEvent возвращает одно событие с рынками и исходами. domain.ErrNotFound,
// если события с таким id нет.
func (s *EventService) GetEvent(ctx context.Context, id int64) (EventWithMarkets, error) {
	event, err := s.events.GetByID(ctx, id)
	if err != nil {
		return EventWithMarkets{}, fmt.Errorf("EventService.GetEvent: %w", err)
	}

	markets, err := s.markets.GetByEvent(ctx, id)
	if err != nil {
		return EventWithMarkets{}, fmt.Errorf("EventService.GetEvent markets: %w", err)
	}

	mwos := make([]MarketWithOutcomes, 0, len(markets))
	for _, m := range markets {
		outcomes, err := s.outcomes.GetByMarket(ctx, m.ID)
		if err != nil {
			return EventWithMarkets{}, fmt.Errorf("EventService.GetEvent outcomes market_id=%d: %w", m.ID, err)
		}
		mwos = append(mwos, MarketWithOutcomes{Market: m, Outcomes: outcomes})
	}

	return EventWithMarkets{Event: event, Markets: mwos}, nil
}
