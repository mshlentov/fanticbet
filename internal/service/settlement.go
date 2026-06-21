package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/repository"

	"github.com/shopspring/decimal"
)

// SettlementService рассчитывает завершённые события: помечает результаты
// исходов, статусы рынков/события и проводит по pending-ставкам выплаты/возвраты.
// Расчёт финансовой операции (выплата/возврат) проходит в одной транзакции с
// SELECT ... FOR UPDATE на кошельке (см. architecture.md §5, conventions §8).
type SettlementService struct {
	tx       TxRunner
	events   repository.EventRepository
	markets  repository.MarketRepository
	outcomes repository.OutcomeRepository
	bets     repository.BetRepository
	wallets  repository.WalletRepository
	walletTx repository.WalletTransactionRepository
	logger   *log.Logger
}

func NewSettlementService(
	tx TxRunner,
	events repository.EventRepository,
	markets repository.MarketRepository,
	outcomes repository.OutcomeRepository,
	bets repository.BetRepository,
	wallets repository.WalletRepository,
	walletTx repository.WalletTransactionRepository,
	logger *log.Logger,
) *SettlementService {
	if logger == nil {
		logger = log.Default()
	}
	return &SettlementService{
		tx:       tx,
		events:   events,
		markets:  markets,
		outcomes: outcomes,
		bets:     bets,
		wallets:  wallets,
		walletTx: walletTx,
		logger:   logger,
	}
}

// outcomePlan — результат расчёта одного исхода и его рынка.
type outcomePlan struct {
	outcomeID int64
	result    domain.Result // вычисленный результат исхода
}

// marketPlan — результат расчёта одного рынка: новый статус и результаты исходов.
type marketPlan struct {
	marketID int64
	status   domain.MarketStatus
	outcomes []outcomePlan
}

// settlementPlan — полностью подготовленные изменения события. Считается вне
// транзакции (чистый анализ + чтение), применяется внутри одной tx.
type settlementPlan struct {
	finalStatus domain.EventStatus
	markets     []marketPlan
}

// ErrScoresUnavailable — финальный scores события невалиден/отсутствует. Событие
// не расчитывается: воркер залогирует и пропустит, ожидая scores в следующем
// прогоне (консервативно — лучше дождаться счёта, чем считать неверно).
var ErrScoresUnavailable = errors.New("scores unavailable for settlement")

// SettleEvent рассчитывает одно событие по его финальному статусу и scores.
// finalStatus — статус из API (settled или cancelled); scores — сырой scores
// (нужен только для settled; для cancelled игнорируется).
//
// Идемпотентность: обрабатываем только pending-ставки (ListPendingByOutcomes),
// а воркер не выбирает уже settled/cancelled события. Повторный прогон по
// рассчитанному событию безопасен — pending-ставок в нём уже нет.
func (s *SettlementService) SettleEvent(ctx context.Context, eventID int64, finalStatus domain.EventStatus, scores json.RawMessage) error {
	plan, err := s.buildPlan(ctx, eventID, finalStatus, scores)
	if err != nil {
		return fmt.Errorf("SettlementService.SettleEvent event_id=%d: %w", eventID, err)
	}

	err = s.tx.RunInTx(ctx, func(ctx context.Context) error {
		return s.applyPlan(ctx, eventID, scores, plan)
	})
	if err != nil {
		return fmt.Errorf("SettlementService.SettleEvent event_id=%d: %w", eventID, err)
	}
	return nil
}

// SettleCustomEvent рассчитывает кастомное событие по выбранному админом
// победившему исходу (без scores — для custom его нет). Это ручной расчёт:
// админ вызывает POST /admin/events/:id/settle c winning_outcome_id.
//
// Логика по каждому рынку события:
//   - если winningOutcomeID входит в исходы рынка — этот исход won, прочие того
//     же рынка lost, рынок → settled;
//   - если выигрышного исхода в рынке нет — весь рынок → void (безопасный
//     fallback: ставки возвращаются). На практике у custom-события один рынок,
//     поэтому этот случай наступает только при ошибке админа (не тот id).
//
// Итоговый статус события — settled. Идемпотентность та же, что у SettleEvent:
// applyPlan обрабатывает только pending-ставки, повторный прогон безопасен.
func (s *SettlementService) SettleCustomEvent(ctx context.Context, eventID, winningOutcomeID int64) error {
	plan, err := s.buildCustomPlan(ctx, eventID, winningOutcomeID)
	if err != nil {
		return fmt.Errorf("SettlementService.SettleCustomEvent event_id=%d: %w", eventID, err)
	}

	err = s.tx.RunInTx(ctx, func(ctx context.Context) error {
		// scores для custom не нужен — передаём nil, applyPlan сохранит NULL.
		return s.applyPlan(ctx, eventID, nil, plan)
	})
	if err != nil {
		return fmt.Errorf("SettlementService.SettleCustomEvent event_id=%d: %w", eventID, err)
	}
	return nil
}

// buildCustomPlan готовит settlementPlan по winningOutcomeID (аналог buildPlan,
// но без scores — победитель задаётся явно админом, а не выводится из счёта).
func (s *SettlementService) buildCustomPlan(ctx context.Context, eventID, winningOutcomeID int64) (settlementPlan, error) {
	plan := settlementPlan{finalStatus: domain.EventSettled}

	dbMarkets, err := s.markets.GetByEvent(ctx, eventID)
	if err != nil {
		return settlementPlan{}, fmt.Errorf("load markets: %w", err)
	}

	for _, m := range dbMarkets {
		ocs, err := s.outcomes.GetByMarket(ctx, m.ID)
		if err != nil {
			return settlementPlan{}, fmt.Errorf("load outcomes market_id=%d: %w", m.ID, err)
		}

		// Есть ли победивший исход в этом рынке?
		hasWinner := false
		for _, o := range ocs {
			if o.ID == winningOutcomeID {
				hasWinner = true
				break
			}
		}

		mp := marketPlan{marketID: m.ID}
		if hasWinner {
			mp.status = domain.MarketSettled
			for _, o := range ocs {
				res := domain.ResultLost
				if o.ID == winningOutcomeID {
					res = domain.ResultWon
				}
				mp.outcomes = append(mp.outcomes, outcomePlan{outcomeID: o.ID, result: res})
			}
		} else {
			// Победителя в этом рынке нет — скорее всего, ошибка в winning_outcome_id.
			// Возвращаем ставки этого рынка (void), чтобы не наказывать пользователей
			// за ошибку админа.
			mp.status = domain.MarketVoid
			for _, o := range ocs {
				mp.outcomes = append(mp.outcomes, outcomePlan{outcomeID: o.ID, result: domain.ResultVoid})
			}
		}
		plan.markets = append(plan.markets, mp)
	}
	return plan, nil
}

// buildPlan готовит settlementPlan вне транзакции: загружает рынки/исходы и
// вычисляет их результаты по scores. Только чтение из БД + чистый анализ.
func (s *SettlementService) buildPlan(ctx context.Context, eventID int64, finalStatus domain.EventStatus, scores json.RawMessage) (settlementPlan, error) {
	plan := settlementPlan{finalStatus: finalStatus}

	dbMarkets, err := s.markets.GetByEvent(ctx, eventID)
	if err != nil {
		return settlementPlan{}, fmt.Errorf("load markets: %w", err)
	}

	// Для отменённого события все исходы → void, рынки → void. scores не нужен.
	if finalStatus == domain.EventCancelled {
		for _, m := range dbMarkets {
			ocs, err := s.outcomes.GetByMarket(ctx, m.ID)
			if err != nil {
				return settlementPlan{}, fmt.Errorf("load outcomes market_id=%d: %w", m.ID, err)
			}
			mp := marketPlan{marketID: m.ID, status: domain.MarketVoid}
			for _, o := range ocs {
				mp.outcomes = append(mp.outcomes, outcomePlan{outcomeID: o.ID, result: domain.ResultVoid})
			}
			plan.markets = append(plan.markets, mp)
		}
		return plan, nil
	}

	// settled — нужен валидный scores для определения победителей.
	if finalStatus != domain.EventSettled {
		return settlementPlan{}, fmt.Errorf("unsupported final status %q: %w", finalStatus, domain.ErrMarketClosed)
	}

	home, away, ok := parseScores(scores)
	if !ok {
		return settlementPlan{}, ErrScoresUnavailable
	}

	for _, m := range dbMarkets {
		ocs, err := s.outcomes.GetByMarket(ctx, m.ID)
		if err != nil {
			return settlementPlan{}, fmt.Errorf("load outcomes market_id=%d: %w", m.ID, err)
		}
		mp := marketPlan{marketID: m.ID, status: domain.MarketSettled}

		switch m.Type {
		case domain.MarketML:
			applyMLPlan(&mp, ocs, home, away)
		case domain.MarketTotals:
			applyTotalsPlan(&mp, ocs, home, away, m.Line)
		default:
			// Неизвестный/кастомный рынок на этом этапе не расчитывается
			// (CUSTOM будет в M6). Помечаем как void — ставки вернутся.
			for _, o := range ocs {
				mp.outcomes = append(mp.outcomes, outcomePlan{outcomeID: o.ID, result: domain.ResultVoid})
			}
			mp.status = domain.MarketVoid
		}
		plan.markets = append(plan.markets, mp)
	}
	return plan, nil
}

// applyMLPlan наполняет план рынка ML по счёту матча. Победитель → won, прочие
// исходы этого рынка → lost. Если исхода-победителя нет среди outcomes (например,
// ничья в двухисходном спорте, где draw не завели) — все исходы ML теряют
// (корректно: ни одна ставка не выиграла).
func applyMLPlan(mp *marketPlan, ocs []domain.Outcome, home, away int) {
	winner := settleML(home, away)
	for _, o := range ocs {
		res := domain.ResultLost
		if o.Code == winner {
			res = domain.ResultWon
		}
		mp.outcomes = append(mp.outcomes, outcomePlan{outcomeID: o.ID, result: res})
	}
}

// applyTotalsPlan наполняет план рынка тоталов по счёту и линии. total == line →
// push: оба исхода (over/under) → void. Иначе over/under выигрывает в зависимости
// от того, больше total или меньше. Прочие исходы (если есть) → lost.
func applyTotalsPlan(mp *marketPlan, ocs []domain.Outcome, home, away int, line *decimal.Decimal) {
	overWon, isPush := false, false
	if line != nil {
		overWon, isPush = settleTotals(home, away, *line)
	}
	for _, o := range ocs {
		var res domain.Result
		switch {
		case isPush:
			res = domain.ResultVoid
		case o.Code == domain.OutcomeOver:
			if overWon {
				res = domain.ResultWon
			} else {
				res = domain.ResultLost
			}
		case o.Code == domain.OutcomeUnder:
			if overWon {
				res = domain.ResultLost
			} else {
				res = domain.ResultWon
			}
		default:
			res = domain.ResultLost
		}
		mp.outcomes = append(mp.outcomes, outcomePlan{outcomeID: o.ID, result: res})
	}
}

// applyPlan применяет подготовленный план в текущей транзакции:
//  1. проставить results исходов и статусы рынков;
//  2. по каждой pending-ставке: won → выплата, void → возврат, lost → только статус;
//  3. перевести событие в финальный статус + сохранить scores.
//
// Ставки обрабатываем по возрастанию ID — это детерминированный порядок блокировок
// кошельков, исключающий дедлок между параллельными транзакциями.
func (s *SettlementService) applyPlan(ctx context.Context, eventID int64, scores json.RawMessage, plan settlementPlan) error {
	// Карта outcome_id → result для быстрого определения результата ставки.
	results := make(map[int64]domain.Result)
	for _, m := range plan.markets {
		for _, op := range m.outcomes {
			results[op.outcomeID] = op.result
		}
		if err := s.markets.UpdateStatus(ctx, m.marketID, m.status); err != nil {
			return fmt.Errorf("update market_status market_id=%d: %w", m.marketID, err)
		}
	}
	// Статусы исходов проставляем после рынков, но в той же транзакции.
	for _, m := range plan.markets {
		for _, op := range m.outcomes {
			if err := s.outcomes.UpdateResult(ctx, op.outcomeID, op.result); err != nil {
				return fmt.Errorf("update outcome_result outcome_id=%d: %w", op.outcomeID, err)
			}
		}
	}

	// Собираем outcomeIDs и выбираем pending-ставки.
	outcomeIDs := make([]int64, 0, len(results))
	for id := range results {
		outcomeIDs = append(outcomeIDs, id)
	}
	pendingBets, err := s.bets.ListPendingByOutcomes(ctx, outcomeIDs)
	if err != nil {
		return fmt.Errorf("list pending bets: %w", err)
	}

	now := s.now()
	for _, b := range pendingBets {
		result, ok := results[b.OutcomeID]
		if !ok {
			// Ставка ссылается на исход вне расчитанного события — данных не
			// хватает. Такого быть не должно; пропускаем безопасно (без смены
			// статуса), оставляя ставку pending для отдельного разбора.
			s.logger.Printf("Settlement: bet_id=%d outcome_id=%d has no result in event_id=%d, skipped",
				b.ID, b.OutcomeID, eventID)
			continue
		}

		switch result {
		case domain.ResultWon:
			if err := s.payout(ctx, b); err != nil {
				return fmt.Errorf("payout bet_id=%d: %w", b.ID, err)
			}
		case domain.ResultVoid:
			if err := s.refund(ctx, b); err != nil {
				return fmt.Errorf("refund bet_id=%d: %w", b.ID, err)
			}
		}
		// lost — денег не трогаем, только статус.

		status := betStatusFromResult(result)
		if err := s.bets.UpdateStatusSettled(ctx, b.ID, status, now); err != nil {
			return fmt.Errorf("update bet_status bet_id=%d: %w", b.ID, err)
		}
	}

	if err := s.events.UpdateStatusAndScores(ctx, eventID, plan.finalStatus, scores); err != nil {
		return fmt.Errorf("update event status/scores: %w", err)
	}
	return nil
}

// payout выплачивает выигрыш: блокирует кошелёк, начисляет potential_payout,
// фиксирует движение bet_payout. Та же схема, что при списании ставки в
// BettingService.PlaceBet (conventions §7-8).
func (s *SettlementService) payout(ctx context.Context, b domain.Bet) error {
	if _, err := s.wallets.GetByUserIDForUpdate(ctx, b.UserID); err != nil {
		return fmt.Errorf("lock wallet user_id=%d: %w", b.UserID, err)
	}
	newBalance, err := s.wallets.UpdateBalance(ctx, b.UserID, b.PotentialPayout)
	if err != nil {
		return err
	}
	betID := b.ID
	if _, err := s.walletTx.Create(ctx, domain.WalletTransaction{
		UserID:       b.UserID,
		Amount:       b.PotentialPayout,
		Type:         domain.TxBetPayout,
		BetID:        &betID,
		BalanceAfter: newBalance,
	}); err != nil {
		return err
	}
	return nil
}

// refund возвращает сумму ставки при void/cancelled: блокирует кошелёк,
// возвращает stake, фиксирует движение bet_refund.
func (s *SettlementService) refund(ctx context.Context, b domain.Bet) error {
	if _, err := s.wallets.GetByUserIDForUpdate(ctx, b.UserID); err != nil {
		return fmt.Errorf("lock wallet user_id=%d: %w", b.UserID, err)
	}
	newBalance, err := s.wallets.UpdateBalance(ctx, b.UserID, b.Stake)
	if err != nil {
		return err
	}
	betID := b.ID
	if _, err := s.walletTx.Create(ctx, domain.WalletTransaction{
		UserID:       b.UserID,
		Amount:       b.Stake,
		Type:         domain.TxBetRefund,
		BetID:        &betID,
		BalanceAfter: newBalance,
	}); err != nil {
		return err
	}
	return nil
}

// betStatusFromResult переводит результат исхода в итоговый статус ставки.
// won/lost/void совпадают по значениям у Result и BetStatus (см. domain/constants.go).
func betStatusFromResult(r domain.Result) domain.BetStatus {
	switch r {
	case domain.ResultWon:
		return domain.BetWon
	case domain.ResultLost:
		return domain.BetLost
	default:
		return domain.BetVoid
	}
}

// now обёрнут в метод, чтобы в тестах подменять время (фиксированный now).
func (s *SettlementService) now() time.Time {
	return time.Now()
}

// --- чистые функции расчёта (без побочных эффектов, тестируются таблицей) ---

// parseScores разбирает сырой scores события вида {"home":N,"away":N} в целые
// счёта команд. ok=false при пустом/невалидном JSON или отрицательных значениях
// (счёт матча не может быть отрицательным).
func parseScores(raw json.RawMessage) (home, away int, ok bool) {
	if len(raw) == 0 {
		return 0, 0, false
	}
	var s struct {
		Home int `json:"home"`
		Away int `json:"away"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, 0, false
	}
	if s.Home < 0 || s.Away < 0 {
		return 0, 0, false
	}
	return s.Home, s.Away, true
}

// settleML определяет победителя матча по счёту. Ничья — при равенстве.
func settleML(home, away int) domain.OutcomeCode {
	switch {
	case home > away:
		return domain.OutcomeHome
	case away > home:
		return domain.OutcomeAway
	default:
		return domain.OutcomeDraw
	}
}

// settleTotals определяет исход тотала по счёту и линии. overWon=true, если
// home+away строго больше линии; isPush=true, если равно (возврат).
func settleTotals(home, away int, line decimal.Decimal) (overWon, isPush bool) {
	total := decimal.NewFromInt(int64(home + away))
	cmp := total.Cmp(line)
	switch {
	case cmp > 0:
		return true, false
	case cmp == 0:
		return false, true // push — возврат
	default:
		return false, false
	}
}
