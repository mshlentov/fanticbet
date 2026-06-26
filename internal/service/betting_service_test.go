package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"fanticbet/internal/domain"

	"github.com/shopspring/decimal"
)

const (
	testBetMin = int64(10)
	testBetMax = int64(10000)
)

// newTestBetting собирает BettingService с инъекцией моков. Возвращает моки для
// настройки ожиданий в конкретном тесте.
func newTestBetting(t *testing.T) (
	*BettingService,
	*fakeTxRunner,
	*fakeBetRepo,
	*fakeOutcomeRepo,
	*fakeMarketRepo,
	*fakeEventRepo,
	*fakeWalletRepo,
	*fakeWalletTxRepo,
) {
	t.Helper()
	tx := &fakeTxRunner{}
	bets := &fakeBetRepo{}
	outcomes := &fakeOutcomeRepo{}
	markets := &fakeMarketRepo{}
	events := &fakeEventRepo{}
	wallets := &fakeWalletRepo{}
	walletTx := &fakeWalletTxRepo{}

	svc := NewBettingService(tx, bets, outcomes, markets, events, wallets, walletTx, testBetMin, testBetMax)
	return svc, tx, bets, outcomes, markets, events, wallets, walletTx
}

// validPlaceSetup настраивает моки на «успешный» сценарий: открытый рынок,
// предстоящее событие, исход с коэффициентом 1.90, баланс 1000. Возвращает
// коэффициент, чтобы тест мог посчитать ожидаемый payout.
func validPlaceSetup(
	outcomes *fakeOutcomeRepo,
	markets *fakeMarketRepo,
	events *fakeEventRepo,
	wallets *fakeWalletRepo,
	bets *fakeBetRepo,
	walletTx *fakeWalletTxRepo,
) decimal.Decimal {
	odds := decimal.NewFromFloat(1.90)
	outcomes.getByIDFn = func(ctx context.Context, id int64) (domain.Outcome, error) {
		return domain.Outcome{ID: id, MarketID: 7, Code: domain.OutcomeHome, Odds: odds}, nil
	}
	markets.getByIDFn = func(ctx context.Context, id int64) (domain.Market, error) {
		return domain.Market{ID: id, EventID: 3, Type: domain.MarketML, Status: domain.MarketOpen}, nil
	}
	events.getFn = func(ctx context.Context, id int64) (domain.Event, error) {
		return domain.Event{
			ID:       id,
			Status:   domain.EventUpcoming,
			StartsAt: time.Now().Add(time.Hour), // событие ещё не началось
		}, nil
	}
	wallets.getForUpdFn = func(ctx context.Context, userID int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: userID, Balance: 1000}, nil
	}
	wallets.updateBalFn = func(ctx context.Context, userID int64, delta int64) (int64, error) {
		return 1000 + delta, nil
	}
	bets.createFn = func(ctx context.Context, b domain.Bet) (int64, error) { return 99, nil }
	walletTx.createFn = func(ctx context.Context, t domain.WalletTransaction) (int64, error) { return 1, nil }
	return odds
}

// --- PlaceBet: happy path ---

func TestBettingService_PlaceBet_Success(t *testing.T) {
	svc, tx, bets, outcomes, markets, events, wallets, walletTx := newTestBetting(t)
	odds := validPlaceSetup(outcomes, markets, events, wallets, bets, walletTx)

	const stake = int64(500)
	result, err := svc.PlaceBet(context.Background(), 42, 7, stake)
	if err != nil {
		t.Fatalf("PlaceBet error: %v", err)
	}

	// Всё прошло в одной транзакции.
	if tx.calls != 1 {
		t.Errorf("expected 1 tx call, got %d", tx.calls)
	}

	// Ставка зафиксировала коэффициент outcome и pending-статус.
	if result.Bet.ID != 99 {
		t.Errorf("bet id: got %d, want 99", result.Bet.ID)
	}
	if !result.Bet.Odds.Equal(odds) {
		t.Errorf("bet odds: got %s, want %s", result.Bet.Odds, odds)
	}
	if result.Bet.Status != domain.BetPending {
		t.Errorf("bet status: got %q, want pending", result.Bet.Status)
	}
	if result.Bet.UserID != 42 || result.Bet.OutcomeID != 7 || result.Bet.EventID != 3 {
		t.Errorf("bet refs wrong: %+v", result.Bet)
	}

	// potential_payout = floor(500 * 1.90) = floor(950.0) = 950.
	if result.Bet.PotentialPayout != 950 {
		t.Errorf("potential_payout: got %d, want 950", result.Bet.PotentialPayout)
	}

	// Списано ровно stake, итоговый баланс = 1000 - 500.
	if result.Balance != 500 {
		t.Errorf("balance after: got %d, want 500", result.Balance)
	}
	if wallets.updateBalArg != -stake {
		t.Errorf("wallet delta: got %d, want %d", wallets.updateBalArg, -stake)
	}

	// Запись в журнале: тип bet_stake, отрицательная сумма, ссылка на ставку,
	// balance_after сходится с новым балансом.
	if walletTx.lastCreated.Type != domain.TxBetStake {
		t.Errorf("tx type: got %q, want bet_stake", walletTx.lastCreated.Type)
	}
	if walletTx.lastCreated.Amount != -stake {
		t.Errorf("tx amount: got %d, want %d", walletTx.lastCreated.Amount, -stake)
	}
	if walletTx.lastCreated.BetID == nil || *walletTx.lastCreated.BetID != 99 {
		t.Errorf("tx bet_id: got %v, want 99", walletTx.lastCreated.BetID)
	}
	if walletTx.lastCreated.BalanceAfter != 500 {
		t.Errorf("tx balance_after: got %d, want 500", walletTx.lastCreated.BalanceAfter)
	}
}

// Проверка floor для «неудобного» произведения: 333 * 1.85 = 616.05 → 616.
func TestBettingService_PlaceBet_PotentialPayoutFloors(t *testing.T) {
	svc, _, bets, outcomes, markets, events, wallets, walletTx := newTestBetting(t)
	validPlaceSetup(outcomes, markets, events, wallets, bets, walletTx)
	// Переопределяем коэффициент исхода на «неудобный» для проверки floor.
	odds := decimal.NewFromFloat(1.85)
	outcomes.getByIDFn = func(ctx context.Context, id int64) (domain.Outcome, error) {
		return domain.Outcome{ID: id, MarketID: 7, Odds: odds}, nil
	}

	result, err := svc.PlaceBet(context.Background(), 1, 7, 333)
	if err != nil {
		t.Fatalf("PlaceBet error: %v", err)
	}
	if result.Bet.PotentialPayout != 616 {
		t.Errorf("potential_payout: got %d, want 616 (floor of 616.05)", result.Bet.PotentialPayout)
	}
	// И ставка фиксирует именно этот коэффициент.
	if !result.Bet.Odds.Equal(odds) {
		t.Errorf("bet odds: got %s, want %s", result.Bet.Odds, odds)
	}
}

// --- PlaceBet: ставка вне диапазона ---

func TestBettingService_PlaceBet_BelowMin(t *testing.T) {
	svc, tx, _, _, _, _, _, _ := newTestBetting(t)

	_, err := svc.PlaceBet(context.Background(), 1, 7, testBetMin-1)
	if !errors.Is(err, domain.ErrBetOutOfRange) {
		t.Errorf("expected ErrBetOutOfRange, got %v", err)
	}
	// Заведомо невалидная ставка не должна открывать транзакцию.
	if tx.calls != 0 {
		t.Errorf("expected 0 tx calls for invalid stake, got %d", tx.calls)
	}
}

func TestBettingService_PlaceBet_AboveMax(t *testing.T) {
	svc, _, _, _, _, _, _, _ := newTestBetting(t)

	_, err := svc.PlaceBet(context.Background(), 1, 7, testBetMax+1)
	if !errors.Is(err, domain.ErrBetOutOfRange) {
		t.Errorf("expected ErrBetOutOfRange, got %v", err)
	}
}

// --- PlaceBet: недостаточно баланса ---

func TestBettingService_PlaceBet_InsufficientBalance(t *testing.T) {
	svc, _, bets, outcomes, markets, events, wallets, _ := newTestBetting(t)
	validPlaceSetup(outcomes, markets, events, wallets, bets, &fakeWalletTxRepo{})
	// Баланс меньше ставки.
	wallets.getForUpdFn = func(ctx context.Context, userID int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: userID, Balance: 100}, nil
	}
	betCreated := false
	bets.createFn = func(ctx context.Context, b domain.Bet) (int64, error) {
		betCreated = true
		return 1, nil
	}

	_, err := svc.PlaceBet(context.Background(), 1, 7, 500)
	if !errors.Is(err, domain.ErrInsufficientBalance) {
		t.Errorf("expected ErrInsufficientBalance, got %v", err)
	}
	// Ставка не должна создаться, если баланс недостаточен.
	if betCreated {
		t.Error("bet must not be created when balance is insufficient")
	}
}

// --- PlaceBet: рынок закрыт (событие не upcoming) ---

func TestBettingService_PlaceBet_EventNotUpcoming(t *testing.T) {
	svc, _, bets, outcomes, markets, events, wallets, _ := newTestBetting(t)
	validPlaceSetup(outcomes, markets, events, wallets, bets, &fakeWalletTxRepo{})
	events.getFn = func(ctx context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Status: domain.EventLive, StartsAt: time.Now().Add(-time.Hour)}, nil
	}

	_, err := svc.PlaceBet(context.Background(), 1, 7, 500)
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("expected ErrMarketClosed for live event, got %v", err)
	}
}

// --- PlaceBet: рынок закрыт (событие уже началось) ---

func TestBettingService_PlaceBet_EventAlreadyStarted(t *testing.T) {
	svc, _, bets, outcomes, markets, events, wallets, _ := newTestBetting(t)
	validPlaceSetup(outcomes, markets, events, wallets, bets, &fakeWalletTxRepo{})
	// Статус upcoming, но starts_at в прошлом — ставки уже закрыты.
	events.getFn = func(ctx context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Status: domain.EventUpcoming, StartsAt: time.Now().Add(-time.Minute)}, nil
	}

	_, err := svc.PlaceBet(context.Background(), 1, 7, 500)
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("expected ErrMarketClosed for started event, got %v", err)
	}
}

// --- PlaceBet: рынок suspended ---

func TestBettingService_PlaceBet_MarketSuspended(t *testing.T) {
	svc, _, bets, outcomes, markets, events, wallets, _ := newTestBetting(t)
	validPlaceSetup(outcomes, markets, events, wallets, bets, &fakeWalletTxRepo{})
	markets.getByIDFn = func(ctx context.Context, id int64) (domain.Market, error) {
		return domain.Market{ID: id, EventID: 3, Status: domain.MarketSuspended}, nil
	}

	_, err := svc.PlaceBet(context.Background(), 1, 7, 500)
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("expected ErrMarketClosed for suspended market, got %v", err)
	}
}

// --- PlaceBet: исход не найден ---

func TestBettingService_PlaceBet_OutcomeNotFound(t *testing.T) {
	svc, _, bets, outcomes, markets, events, wallets, _ := newTestBetting(t)
	validPlaceSetup(outcomes, markets, events, wallets, bets, &fakeWalletTxRepo{})
	outcomes.getByIDFn = func(ctx context.Context, id int64) (domain.Outcome, error) {
		return domain.Outcome{}, domain.ErrNotFound
	}

	_, err := svc.PlaceBet(context.Background(), 1, 999, 500)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// --- ListBets ---

func TestBettingService_ListBets(t *testing.T) {
	svc, _, bets, _, _, _, _, _ := newTestBetting(t)
	want := []domain.BetWithDetails{
		{Bet: domain.Bet{ID: 1, UserID: 5, Stake: 100, Status: domain.BetPending}},
		{Bet: domain.Bet{ID: 2, UserID: 5, Stake: 200, Status: domain.BetWon}},
	}
	bets.listByUserFn = func(ctx context.Context, userID int64, status domain.BetStatus, page int) ([]domain.BetWithDetails, error) {
		if userID != 5 {
			t.Errorf("queried wrong user: %d", userID)
		}
		if status != domain.BetPending {
			t.Errorf("queried wrong status: %q", status)
		}
		if page != 1 {
			t.Errorf("queried wrong page: %d", page)
		}
		return want, nil
	}

	got, err := svc.ListBets(context.Background(), 5, domain.BetPending, 1)
	if err != nil {
		t.Fatalf("ListBets error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d bets, want 2", len(got))
	}
	if got[0].ID != 1 || got[1].ID != 2 {
		t.Errorf("unexpected bets: %+v", got)
	}
}

// --- computePotentialPayout: отдельные проверки округления ---

func TestComputePotentialPayout(t *testing.T) {
	tests := []struct {
		name  string
		stake int64
		odds  string // строка, чтобы избежать float-неточности в тесте
		want  int64
	}{
		{"целое произведение", 500, "1.90", 950},
		{"floor дробного", 333, "1.85", 616},              // 616.05 → 616
		{"минимальный коэффициент", 1000, "1.001", 1001},  // 1001.000 → 1001
		{"округление вниз на границе", 100, "1.999", 199}, // 199.9 → 199
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			odds, err := decimal.NewFromString(tt.odds)
			if err != nil {
				t.Fatalf("bad odds %q: %v", tt.odds, err)
			}
			got := computePotentialPayout(tt.stake, odds)
			if got != tt.want {
				t.Errorf("computePotentialPayout(%d, %s) = %d, want %d", tt.stake, tt.odds, got, tt.want)
			}
		})
	}
}
