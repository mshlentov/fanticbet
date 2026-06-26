package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/repository"

	"github.com/shopspring/decimal"
)

// CustomOutcomeInput — один исход при создании/правке кастомного события.
// При создании оба поля обязательны; при правке id обязателен, а label/odds —
// опциональны (nil = «не менять»).
type CustomOutcomeInput struct {
	ID    *int64           // только при правке; nil при создании
	Label *string          // при правке nil = «не менять»
	Odds  *decimal.Decimal // при правке nil = «не менять»
}

// CustomMarketInput — рынок кастомного события (вопрос + исходы).
type CustomMarketInput struct {
	Question *string
	Outcomes []CustomOutcomeInput
}

// CustomEventInput — тело POST /admin/events: заголовок, время старта и рынок
// с вопросом и исходами.
type CustomEventInput struct {
	Title    string
	StartsAt time.Time
	Market   CustomMarketInput
}

// CreatedEvent — результат создания: событие, его рынок и исходы (с уже
// проставленными id из БД) для ответа хендлеру.
type CreatedEvent struct {
	Event    domain.Event
	Market   domain.Market
	Outcomes []domain.Outcome
}

// EditOutcomeInput — правка одного исхода: id обязателен, label/odds опциональны.
type EditOutcomeInput struct {
	ID    int64
	Label *string
	Odds  *decimal.Decimal
}

// EditEventInput — тело PATCH /admin/events/:id. Все поля опциональны (nil =
// «не менять»), кроме Cancel и Outcomes. Cancel=true → отмена события (void
// всех ставок с возвратом). Outcomes правит существующие исходы по id.
type EditEventInput struct {
	Title    *string
	StartsAt *time.Time
	Question *string
	Cancel   bool
	Outcomes []EditOutcomeInput
}

// minCustomOutcomes — минимальное число исходов в кастомном событии. Меньше двух
// бессмысленно: на что тогда ставить. Число вынесено константой — пока продукт
// не просит настраивать его.
const minCustomOutcomes = 2

// AdminService — операции администратора: создание/правка/отмена/расчёт
// кастомных событий, управление чемпионатами (leagues) и ручная корректировка
// баланса. Финансовые операции (корректировка баланса, отмена, расчёт) идут в
// транзакции с FOR UPDATE на кошельке (см. architecture.md §4, conventions §7-8).
//
// Сервис НЕ знает про HTTP: handler передаёт доменные структуры, сервис
// возвращает доменные ошибки, которые handler мапит на HTTP-коды.
type AdminService struct {
	tx         TxRunner
	events     repository.EventRepository
	markets    repository.MarketRepository
	outcomes   repository.OutcomeRepository
	leagues    repository.LeagueRepository
	wallets    repository.WalletRepository
	walletTx   repository.WalletTransactionRepository
	settlement *SettlementService // отмена и расчёт делегируются в него
	logger     *log.Logger
	nowFunc    func() time.Time // переопределяется в тестах (фиксированный now)
}

// NewAdminService собирает сервис. settlement — уже готовый *SettlementService
// (отмена/расчёт переиспользуют его идемпотентную логику выплат). logger=nil →
// log.Default().
func NewAdminService(
	tx TxRunner,
	events repository.EventRepository,
	markets repository.MarketRepository,
	outcomes repository.OutcomeRepository,
	leagues repository.LeagueRepository,
	wallets repository.WalletRepository,
	walletTx repository.WalletTransactionRepository,
	settlement *SettlementService,
	logger *log.Logger,
) *AdminService {
	if logger == nil {
		logger = log.Default()
	}
	return &AdminService{
		tx:         tx,
		events:     events,
		markets:    markets,
		outcomes:   outcomes,
		leagues:    leagues,
		wallets:    wallets,
		walletTx:   walletTx,
		settlement: settlement,
		logger:     logger,
		nowFunc:    time.Now,
	}
}

// CreateCustomEvent создаёт кастомное событие с одним CUSTOM-рынком и исходами
// в одной транзакции. Исходы получают коды opt_1..opt_N. Валидация (≥2 исхода,
// каждый odds > 1.0, непустые label/title, starts_at в будущем) — до вставок,
// чтобы не открывать tx ради невалидного запроса.
func (s *AdminService) CreateCustomEvent(ctx context.Context, adminID int64, input CustomEventInput) (CreatedEvent, error) {
	if err := validateCreate(input); err != nil {
		return CreatedEvent{}, fmt.Errorf("AdminService.CreateCustomEvent: %w", err)
	}

	var result CreatedEvent
	err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
		// Событие: source=custom, sport_slug='custom', без external_id/home/away.
		event := domain.Event{
			Source:    domain.SourceCustom,
			SportSlug: string(domain.SourceCustom),
			Title:     input.Title,
			StartsAt:  input.StartsAt,
			Status:    domain.EventUpcoming,
			CreatedBy: &adminID,
		}
		eventID, err := s.events.Create(ctx, event)
		if err != nil {
			return fmt.Errorf("create event: %w", err)
		}
		event.ID = eventID
		result.Event = event

		// Рынок CUSTOM со статусом open (готов принимать ставки сразу).
		market := domain.Market{
			EventID:  eventID,
			Type:     domain.MarketCustom,
			Question: input.Market.Question,
			Status:   domain.MarketOpen,
		}
		marketID, err := s.markets.CreateForEvent(ctx, market)
		if err != nil {
			return fmt.Errorf("create market event_id=%d: %w", eventID, err)
		}
		market.ID = marketID
		result.Market = market

		// Исходы: коды opt_1..opt_N по порядку. Upsert тут безопасен (рынок
		// новый, конфликтов по (market_id, code) быть не может), зато он
		// возвращает id созданной строки.
		result.Outcomes = make([]domain.Outcome, 0, len(input.Market.Outcomes))
		for i, oc := range input.Market.Outcomes {
			outcome := domain.Outcome{
				MarketID: marketID,
				Code:     customOutcomeCode(i + 1),
				Label:    *oc.Label,
				Odds:     *oc.Odds,
			}
			outcomeID, err := s.outcomes.Upsert(ctx, outcome)
			if err != nil {
				return fmt.Errorf("create outcome market_id=%d index=%d: %w", marketID, i, err)
			}
			outcome.ID = outcomeID
			result.Outcomes = append(result.Outcomes, outcome)
		}
		return nil
	})
	if err != nil {
		return CreatedEvent{}, fmt.Errorf("AdminService.CreateCustomEvent: %w", err)
	}
	return result, nil
}

// EditEvent правит title/starts_at/question и/или коэффициенты исходов кастомного
// события. Только для source='custom' и status='upcoming' — править расчитанное
// или oddsapi-событие нельзя. При input.Cancel=true выполняется отмена (void
// всех ставок) — правки полей при этом игнорируются.
func (s *AdminService) EditEvent(ctx context.Context, eventID int64, input EditEventInput) error {
	// Валидация коэффициентов — до транзакции (чистая входная проверка).
	for _, oc := range input.Outcomes {
		if oc.Odds != nil && !(*oc.Odds).GreaterThan(decimal.NewFromInt(1)) {
			return fmt.Errorf("AdminService.EditEvent outcome_id=%d odds=%s: %w",
				oc.ID, *oc.Odds, domain.ErrBetOutOfRange)
		}
	}

	if input.Cancel {
		// Отмена приоритетнее правок полей: логичнее отменить «как есть».
		return s.CancelEvent(ctx, eventID)
	}

	event, err := s.events.GetByID(ctx, eventID)
	if err != nil {
		return fmt.Errorf("AdminService.EditEvent load event: %w", err)
	}
	if event.Source != domain.SourceCustom {
		return fmt.Errorf("AdminService.EditEvent event_id=%d source=%s: %w",
			eventID, event.Source, domain.ErrMarketClosed)
	}
	if event.Status != domain.EventUpcoming {
		return fmt.Errorf("AdminService.EditEvent event_id=%d status=%s: %w",
			eventID, event.Status, domain.ErrMarketClosed)
	}

	err = s.tx.RunInTx(ctx, func(ctx context.Context) error {
		if err := s.events.UpdateDetails(ctx, eventID, input.Title, input.StartsAt); err != nil {
			return fmt.Errorf("update event details: %w", err)
		}

		if input.Question != nil {
			// У custom-события ожидаем один рынок; берём первый. Если их
			// несколько — правим все по одному вопросу (на практике не случается).
			mkts, err := s.markets.GetByEvent(ctx, eventID)
			if err != nil {
				return fmt.Errorf("load markets: %w", err)
			}
			for _, m := range mkts {
				if err := s.markets.UpdateQuestion(ctx, m.ID, input.Question); err != nil {
					return fmt.Errorf("update question market_id=%d: %w", m.ID, err)
				}
			}
		}

		for _, oc := range input.Outcomes {
			if err := s.outcomes.UpdateLabelAndOdds(ctx, oc.ID, oc.Label, oc.Odds); err != nil {
				return fmt.Errorf("update outcome_id=%d: %w", oc.ID, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("AdminService.EditEvent: %w", err)
	}
	return nil
}

// CancelEvent отменяет событие: все ставки возвращаются (void). Делегирует в
// SettlementService.SettleEvent(cancelled) — там идемпотентный проход по
// pending-ставкам с возвратом через FOR UPDATE. Отменять уже settled/cancelled
// нельзя (ErrMarketClosed).
func (s *AdminService) CancelEvent(ctx context.Context, eventID int64) error {
	event, err := s.events.GetByID(ctx, eventID)
	if err != nil {
		return fmt.Errorf("AdminService.CancelEvent load event: %w", err)
	}
	if event.Status == domain.EventSettled || event.Status == domain.EventCancelled {
		return fmt.Errorf("AdminService.CancelEvent event_id=%d status=%s: %w",
			eventID, event.Status, domain.ErrMarketClosed)
	}

	if err := s.settlement.SettleEvent(ctx, eventID, domain.EventCancelled, nil); err != nil {
		return fmt.Errorf("AdminService.CancelEvent: %w", err)
	}
	return nil
}

// SettleCustom рассчитывает кастомное событие по выбранному победившему исходу.
// Делегирует в SettlementService.SettleCustomEvent. Рассчитывать можно только
// custom-событие, ещё не переведённое в settled/cancelled.
func (s *AdminService) SettleCustom(ctx context.Context, eventID, winningOutcomeID int64) error {
	event, err := s.events.GetByID(ctx, eventID)
	if err != nil {
		return fmt.Errorf("AdminService.SettleCustom load event: %w", err)
	}
	if event.Source != domain.SourceCustom {
		return fmt.Errorf("AdminService.SettleCustom event_id=%d source=%s: %w",
			eventID, event.Source, domain.ErrMarketClosed)
	}
	if event.Status == domain.EventSettled || event.Status == domain.EventCancelled {
		return fmt.Errorf("AdminService.SettleCustom event_id=%d status=%s: %w",
			eventID, event.Status, domain.ErrMarketClosed)
	}

	if err := s.settlement.SettleCustomEvent(ctx, eventID, winningOutcomeID); err != nil {
		return fmt.Errorf("AdminService.SettleCustom: %w", err)
	}
	return nil
}

// AdjustBalance вручную меняет баланс пользователя на amount (может быть
// отрицательным) и фиксирует движение admin_adjust. В одной транзакции с
// SELECT ... FOR UPDATE на кошельке (conventions §7-8). reason — только в лог
// (по решению продукта, без колонки в БД). Возвращает новый баланс.
//
// Уход баланса в минус запрещён: CHECK (balance >= 0) в схеме — последний рубеж,
// но проверка в сервисе даёт понятную ошибку (ErrInsufficientBalance) вместо
// сырой ошибки БД.
func (s *AdminService) AdjustBalance(ctx context.Context, userID, amount int64, reason string) (int64, error) {
	if amount == 0 {
		return 0, fmt.Errorf("AdminService.AdjustBalance amount=0: %w", domain.ErrBetOutOfRange)
	}

	var newBalance int64
	err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
		// Блокируем кошелёк до конца транзакции — без этого параллельная ставка
		// или выплата могут увести баланс в минус.
		wallet, err := s.wallets.GetByUserIDForUpdate(ctx, userID)
		if err != nil {
			return fmt.Errorf("lock wallet user_id=%d: %w", userID, err)
		}
		if wallet.Balance+amount < 0 {
			return fmt.Errorf("AdminService.AdjustBalance balance=%d amount=%d: %w",
				wallet.Balance, amount, domain.ErrInsufficientBalance)
		}

		bal, err := s.wallets.UpdateBalance(ctx, userID, amount)
		if err != nil {
			return fmt.Errorf("update balance user_id=%d amount=%d: %w", userID, amount, err)
		}
		newBalance = bal

		if _, err := s.walletTx.Create(ctx, domain.WalletTransaction{
			UserID:       userID,
			Amount:       amount,
			Type:         domain.TxAdminAdjust,
			BalanceAfter: newBalance,
		}); err != nil {
			return fmt.Errorf("admin_adjust tx user_id=%d: %w", userID, err)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("AdminService.AdjustBalance: %w", err)
	}

	// reason в лог, не в БД (решение продукта: колонки reason в схеме нет).
	s.logger.Printf("AdminService.AdjustBalance: user_id=%d amount=%d reason=%q balance_after=%d",
		userID, amount, reason, newBalance)
	return newBalance, nil
}

// CreateLeagueInput — тело POST /admin/leagues: имя чемпионата и вид спорта.
type CreateLeagueInput struct {
	Name      string
	SportSlug string
}

// UpdateLeagueInput — тело PATCH /admin/leagues/:id. Оба поля опциональны (nil =
// «не менять»).
type UpdateLeagueInput struct {
	Name      *string
	SportSlug *string
}

// CreateLeague создаёт чемпионат после валидации непустых полей. Возвращает
// созданную лигу с проставленным id. Дубликаты (name, sport_slug) допустимы —
// схема не накладывает уникальности; они различаются по id.
func (s *AdminService) CreateLeague(ctx context.Context, input CreateLeagueInput) (domain.League, error) {
	if input.Name == "" {
		return domain.League{}, fmt.Errorf("AdminService.CreateLeague empty name: %w", domain.ErrBetOutOfRange)
	}
	if input.SportSlug == "" {
		return domain.League{}, fmt.Errorf("AdminService.CreateLeague empty sport_slug: %w", domain.ErrBetOutOfRange)
	}

	id, err := s.leagues.Create(ctx, domain.League{Name: input.Name, SportSlug: input.SportSlug})
	if err != nil {
		return domain.League{}, fmt.Errorf("AdminService.CreateLeague: %w", err)
	}
	// created_at/updated_at проставляет БД (DEFAULT now()); для ответа достаточно
	// вернуть поля запроса + id — handler использует только их.
	return domain.League{ID: id, Name: input.Name, SportSlug: input.SportSlug}, nil
}

// UpdateLeague правит name и/или sport_slug чемпионата. nil-поля оставляют
// текущее значение; непустое строковое поле заменяет старое. domain.ErrNotFound
// (404), если чемпионата нет.
func (s *AdminService) UpdateLeague(ctx context.Context, id int64, input UpdateLeagueInput) error {
	if input.Name != nil && *input.Name == "" {
		return fmt.Errorf("AdminService.UpdateLeague empty name: %w", domain.ErrBetOutOfRange)
	}
	if input.SportSlug != nil && *input.SportSlug == "" {
		return fmt.Errorf("AdminService.UpdateLeague empty sport_slug: %w", domain.ErrBetOutOfRange)
	}

	if err := s.leagues.Update(ctx, id, input.Name, input.SportSlug); err != nil {
		return fmt.Errorf("AdminService.UpdateLeague id=%d: %w", id, err)
	}
	return nil
}

// DeleteLeague удаляет чемпионат, но блокирует удаление, если к нему привязаны
// события (events.league_id) — в этом случае domain.ErrConflict (409). Иначе
// делегирует в репозиторий; domain.ErrNotFound (404), если чемпионата нет.
func (s *AdminService) DeleteLeague(ctx context.Context, id int64) error {
	count, err := s.leagues.CountEventsByLeague(ctx, id)
	if err != nil {
		return fmt.Errorf("AdminService.DeleteLeague count id=%d: %w", id, err)
	}
	if count > 0 {
		return fmt.Errorf("AdminService.DeleteLeague id=%d events=%d: %w", id, count, domain.ErrConflict)
	}

	if err := s.leagues.Delete(ctx, id); err != nil {
		return fmt.Errorf("AdminService.DeleteLeague id=%d: %w", id, err)
	}
	return nil
}

// ListLeagues возвращает чемпионаты, опционально отфильтрованные по sport_slug.
// Используется как админкой (GET /admin/leagues?sport_slug=), так и каталогом.
func (s *AdminService) ListLeagues(ctx context.Context, sportSlug string) ([]domain.League, error) {
	leagues, err := s.leagues.List(ctx, sportSlug)
	if err != nil {
		return nil, fmt.Errorf("AdminService.ListLeagues: %w", err)
	}
	return leagues, nil
}

// validateCreate проверяет вход создания кастомного события до транзакции.
// Возвращает доменную ошибку, чтобы handler мог её корректно отобразить.
func validateCreate(input CustomEventInput) error {
	if input.Title == "" {
		return fmt.Errorf("empty title: %w", domain.ErrBetOutOfRange)
	}
	if input.StartsAt.IsZero() {
		return fmt.Errorf("empty starts_at: %w", domain.ErrBetOutOfRange)
	}
	if len(input.Market.Outcomes) < minCustomOutcomes {
		return fmt.Errorf("outcomes=%d want>=%d: %w",
			len(input.Market.Outcomes), minCustomOutcomes, domain.ErrBetOutOfRange)
	}
	one := decimal.NewFromInt(1)
	for i, oc := range input.Market.Outcomes {
		if oc.Label == nil || *oc.Label == "" {
			return fmt.Errorf("outcome index=%d empty label: %w", i, domain.ErrBetOutOfRange)
		}
		if oc.Odds == nil || !(*oc.Odds).GreaterThan(one) {
			return fmt.Errorf("outcome index=%d odds invalid: %w", i, domain.ErrBetOutOfRange)
		}
	}
	return nil
}

// customOutcomeCode строит код исхода кастомного рынка по порядковому номеру:
// opt_1, opt_2, ... (см. domain.OutcomeCustomPrefix).
func customOutcomeCode(n int) domain.OutcomeCode {
	return domain.OutcomeCode(fmt.Sprintf("%s%d", domain.OutcomeCustomPrefix, n))
}
