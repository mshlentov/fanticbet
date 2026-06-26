package service

import (
	"context"
	"fmt"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/repository"

	"github.com/shopspring/decimal"
)

// PlaceBetResult — результат размещения ставки: созданная ставка и баланс
// кошелька после списания суммы. Balance нужен фронту, чтобы сразу обновить
// отображаемый баланс без повторного GET /me.
type PlaceBetResult struct {
	Bet     domain.Bet
	Balance int64
}

// BettingService — размещение ставок и история. Размещение — финансовая
// операция (списание с кошелька), поэтому проходит в одной транзакции с
// SELECT ... FOR UPDATE на кошельке (см. architecture.md, раздел 4).
type BettingService struct {
	tx       TxRunner
	bets     repository.BetRepository
	outcomes repository.OutcomeRepository
	markets  repository.MarketRepository
	events   repository.EventRepository
	wallets  repository.WalletRepository
	walletTx repository.WalletTransactionRepository
	betMin   int64 // минимальная сумма ставки из конфига
	betMax   int64 // максимальная сумма ставки из конфига
}

func NewBettingService(
	tx TxRunner,
	bets repository.BetRepository,
	outcomes repository.OutcomeRepository,
	markets repository.MarketRepository,
	events repository.EventRepository,
	wallets repository.WalletRepository,
	walletTx repository.WalletTransactionRepository,
	betMin, betMax int64,
) *BettingService {
	return &BettingService{
		tx:       tx,
		bets:     bets,
		outcomes: outcomes,
		markets:  markets,
		events:   events,
		wallets:  wallets,
		walletTx: walletTx,
		betMin:   betMin,
		betMax:   betMax,
	}
}

// PlaceBet размещает ставку на исход. Одна транзакция БД (см. architecture.md,
// флоу размещения ставки):
//  1. загрузить outcome→market→event, проверить event.status='upcoming',
//     starts_at>now() и market.status='open';
//  2. заблокировать кошелёк SELECT ... FOR UPDATE;
//  3. проверить balance >= stake и stake ∈ [min,max];
//  4. INSERT bet (odds = текущий outcome.odds, potential_payout = floor(stake*odds));
//  5. списать stake с кошелька + запись wallet_transactions(type='bet_stake').
//
// Возвращает доменные ошибки: ErrNotFound (исхода нет), ErrMarketClosed,
// ErrInsufficientBalance, ErrBetOutOfRange — handler мапит их на HTTP-коды.
func (s *BettingService) PlaceBet(ctx context.Context, userID, outcomeID, stake int64) (PlaceBetResult, error) {
	// Диапазон ставки проверяем до транзакции: это чистая входная валидация,
	// не зависящая от состояния кошелька/рынка. Так мы не открываем tx ради
	// заведомо невалидного запроса.
	if stake < s.betMin || stake > s.betMax {
		return PlaceBetResult{}, fmt.Errorf("BettingService.PlaceBet stake=%d range=[%d,%d]: %w",
			stake, s.betMin, s.betMax, domain.ErrBetOutOfRange)
	}

	var result PlaceBetResult
	err := s.tx.RunInTx(ctx, func(ctx context.Context) error {
		// Шаг 1: загружаем исход и его рынок. Без JOIN репозиториями — каждый
		// читает свою таблицу; в одной транзакции это по-прежнему атомарно.
		outcome, err := s.outcomes.GetByID(ctx, outcomeID)
		if err != nil {
			return fmt.Errorf("BettingService.PlaceBet load outcome_id=%d: %w", outcomeID, err)
		}

		market, err := s.markets.GetByID(ctx, outcome.MarketID)
		if err != nil {
			return fmt.Errorf("BettingService.PlaceBet load market_id=%d: %w", outcome.MarketID, err)
		}

		event, err := s.events.GetByID(ctx, market.EventID)
		if err != nil {
			return fmt.Errorf("BettingService.PlaceBet load event_id=%d: %w", market.EventID, err)
		}

		// Шаг 1 (проверки): событие должно быть предстоящим и ещё не начаться,
		// рынок — открытым. Иначе ставка невозможна (ErrMarketClosed).
		if event.Status != domain.EventUpcoming ||
			!event.StartsAt.After(s.now()) ||
			market.Status != domain.MarketOpen {
			return fmt.Errorf("BettingService.PlaceBet event_status=%s starts_at=%s market_status=%s: %w",
				event.Status, event.StartsAt.Format(time.RFC3339), market.Status, domain.ErrMarketClosed)
		}

		// Шаг 2: блокируем кошелёк до конца транзакции — без этого два
		// параллельных запроса могут увести баланс в минус.
		wallet, err := s.wallets.GetByUserIDForUpdate(ctx, userID)
		if err != nil {
			return fmt.Errorf("BettingService.PlaceBet lock wallet user_id=%d: %w", userID, err)
		}

		// Шаг 3: достаточно ли фантиков. CHECK (balance >= 0) в схеме —
		// последний рубеж, но проверка в сервисе даёт понятную ошибку.
		if wallet.Balance < stake {
			return fmt.Errorf("BettingService.PlaceBet balance=%d stake=%d: %w",
				wallet.Balance, stake, domain.ErrInsufficientBalance)
		}

		// Шаг 4: фиксируем коэффициент на момент ставки и считаем выплату.
		// potential_payout = floor(stake * odds) через decimal/big — без float,
		// иначе копим ошибку округления (conventions.md:7, Деньги и числа).
		payout := computePotentialPayout(stake, outcome.Odds)

		bet := domain.Bet{
			UserID:          userID,
			OutcomeID:       outcome.ID,
			EventID:         event.ID, // денормализация для выборок без JOIN
			Stake:           stake,
			Odds:            outcome.Odds,
			PotentialPayout: payout,
			Status:          domain.BetPending,
		}
		betID, err := s.bets.Create(ctx, bet)
		if err != nil {
			return fmt.Errorf("BettingService.PlaceBet create bet user_id=%d outcome_id=%d: %w",
				userID, outcomeID, err)
		}
		bet.ID = betID

		// Шаг 5: списываем сумму ставки. UpdateBalance атомарно меняет баланс
		// и возвращает новый — его и зафиксируем в журнале (balance_after).
		newBalance, err := s.wallets.UpdateBalance(ctx, userID, -stake)
		if err != nil {
			return fmt.Errorf("BettingService.PlaceBet debit wallet user_id=%d stake=%d: %w",
				userID, stake, err)
		}

		if _, err := s.walletTx.Create(ctx, domain.WalletTransaction{
			UserID:       userID,
			Amount:       -stake,
			Type:         domain.TxBetStake,
			BetID:        &betID,
			BalanceAfter: newBalance,
		}); err != nil {
			return fmt.Errorf("BettingService.PlaceBet bet_stake tx user_id=%d bet_id=%d: %w",
				userID, betID, err)
		}

		result.Bet = bet
		result.Balance = newBalance
		return nil
	})
	if err != nil {
		return PlaceBetResult{}, fmt.Errorf("BettingService.PlaceBet: %w", err)
	}
	return result, nil
}

// ListBets возвращает страницу ставок пользователя (новые — первыми), обогащённую
// названиями события и исхода. status="" означает «без фильтра»; page начинается
// с 1. Прокси к репозиторию, чтобы хендлер не зависел от repository напрямую
// (слои handler → service → repository).
func (s *BettingService) ListBets(ctx context.Context, userID int64, status domain.BetStatus, page int) ([]domain.BetWithDetails, error) {
	bets, err := s.bets.ListByUser(ctx, userID, status, page)
	if err != nil {
		return nil, fmt.Errorf("BettingService.ListBets user_id=%d status=%s: %w", userID, status, err)
	}
	return bets, nil
}

// computePotentialPayout считает floor(stake * odds) без потери точности.
// decimal.Mul → десятичное произведение; IntPart() возвращает целую часть,
// отбрасывая дробную (floor для положительных чисел). stake и odds всегда > 0
// (CHECK на bets.stake и outcomes.odds), поэтому округление всегда вниз.
func computePotentialPayout(stake int64, odds decimal.Decimal) int64 {
	stakeDec := decimal.NewFromInt(stake)
	product := stakeDec.Mul(odds)
	return product.IntPart()
}

// now обёрнут в метод, чтобы в тестах подменять время (фиксированный now).
func (s *BettingService) now() time.Time {
	return time.Now()
}
