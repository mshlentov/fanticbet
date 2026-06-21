package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"fanticbet/internal/domain"

	"github.com/shopspring/decimal"
)

// newTestSettlement собирает SettlementService с инъекцией моков. Возвращает
// моки для настройки ожиданий конкретного теста. tx выполняет fn inline.
func newTestSettlement(t *testing.T) (
	*SettlementService,
	*fakeTxRunner,
	*fakeEventRepo,
	*fakeMarketRepo,
	*fakeOutcomeRepo,
	*fakeBetRepo,
	*fakeWalletRepo,
	*fakeWalletTxRepo,
) {
	t.Helper()
	tx := &fakeTxRunner{}
	events := &fakeEventRepo{}
	markets := &fakeMarketRepo{}
	outcomes := &fakeOutcomeRepo{}
	bets := &fakeBetRepo{}
	wallets := &fakeWalletRepo{}
	walletTx := &fakeWalletTxRepo{}

	svc := NewSettlementService(tx, events, markets, outcomes, bets, wallets, walletTx, nil)
	return svc, tx, events, markets, outcomes, bets, wallets, walletTx
}

// scoresJSON — хелпер, собирает валидный scores {"home":N,"away":N} как raw JSON.
func scoresJSON(t *testing.T, home, away int) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]int{"home": home, "away": away})
	if err != nil {
		t.Fatalf("marshal scores: %v", err)
	}
	return b
}

// --- Чистые функции ---

func TestParseScores(t *testing.T) {
	tests := []struct {
		name    string
		raw     json.RawMessage
		wantH   int
		wantA   int
		wantOk  bool
	}{
		{"валидный", scoresJSON(t, 2, 1), 2, 1, true},
		{"нули", scoresJSON(t, 0, 0), 0, 0, true},
		{"пустой", nil, 0, 0, false},
		{"сломанный json", json.RawMessage(`{not json`), 0, 0, false},
		{"отрицательный счёт", json.RawMessage(`{"home":-1,"away":0}`), 0, 0, false},
		{"без ключей", json.RawMessage(`{}`), 0, 0, true}, // оба нуля — валидно
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, a, ok := parseScores(tt.raw)
			if h != tt.wantH || a != tt.wantA || ok != tt.wantOk {
				t.Errorf("parseScores(%s) = (%d,%d,%v), want (%d,%d,%v)",
					tt.raw, h, a, ok, tt.wantH, tt.wantA, tt.wantOk)
			}
		})
	}
}

func TestSettleML(t *testing.T) {
	tests := []struct {
		name      string
		home      int
		away      int
		want      domain.OutcomeCode
	}{
		{"победа хозяев", 2, 1, domain.OutcomeHome},
		{"победа гостей", 0, 3, domain.OutcomeAway},
		{"ничья", 1, 1, domain.OutcomeDraw},
		{"нули — ничья", 0, 0, domain.OutcomeDraw},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := settleML(tt.home, tt.away); got != tt.want {
				t.Errorf("settleML(%d,%d) = %s, want %s", tt.home, tt.away, got, tt.want)
			}
		})
	}
}

func TestSettleTotals(t *testing.T) {
	line := decimal.RequireFromString("2.5")
	tests := []struct {
		name     string
		home     int
		away     int
		line     decimal.Decimal
		wantOver bool
		wantPush bool
	}{
		{"больше линии", 2, 1, line, true, false},  // total 3 > 2.5
		{"меньше линии", 1, 1, line, false, false}, // total 2 < 2.5
		{"push (равно линии)", 1, 2, decimal.RequireFromString("3.0"), false, true}, // total 3 == 3.0
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			over, push := settleTotals(tt.home, tt.away, tt.line)
			if over != tt.wantOver || push != tt.wantPush {
				t.Errorf("settleTotals(%d,%d,%s) = (over=%v,push=%v), want (over=%v,push=%v)",
					tt.home, tt.away, tt.line, over, push, tt.wantOver, tt.wantPush)
			}
		})
	}
}

// --- SettleEvent: полный цикл won/lost/void ---

// setupSettledFixture готовит событие с двумя рынками и настраивает моки на
// «успешный» сценарий:
//   - ML-рынок (id 10): home/draw/away, счёт 2:1 → home выиграл.
//   - TOTALS-рынок (id 11): over/under по линии 2.5, счёт 2:1 → total 3, over.
// Возвращает исходы и рынки, чтобы тест мог собрать ставки на нужные из них.
func setupSettledFixture(
	markets *fakeMarketRepo,
	outcomes *fakeOutcomeRepo,
) (mlOutcomes, totalsOutcomes []domain.Outcome) {
	mlOutcomes = []domain.Outcome{
		{ID: 100, MarketID: 10, Code: domain.OutcomeHome, Odds: decimal.RequireFromString("2.00")},
		{ID: 101, MarketID: 10, Code: domain.OutcomeDraw, Odds: decimal.RequireFromString("3.40")},
		{ID: 102, MarketID: 10, Code: domain.OutcomeAway, Odds: decimal.RequireFromString("3.20")},
	}
	totalsOutcomes = []domain.Outcome{
		{ID: 110, MarketID: 11, Code: domain.OutcomeOver, Odds: decimal.RequireFromString("1.90")},
		{ID: 111, MarketID: 11, Code: domain.OutcomeUnder, Odds: decimal.RequireFromString("1.90")},
	}
	line := decimal.RequireFromString("2.5")
	if markets.byEvent == nil {
		markets.byEvent = map[int64][]domain.Market{}
	}
	if outcomes.byMarket == nil {
		outcomes.byMarket = map[int64][]domain.Outcome{}
	}
	markets.byEvent[1] = []domain.Market{
		{ID: 10, EventID: 1, Type: domain.MarketML, Status: domain.MarketOpen},
		{ID: 11, EventID: 1, Type: domain.MarketTotals, Line: &line, Status: domain.MarketOpen},
	}
	outcomes.byMarket[10] = mlOutcomes
	outcomes.byMarket[11] = totalsOutcomes
	return mlOutcomes, totalsOutcomes
}

func TestSettlementService_SettleEvent_WonLostVoid(t *testing.T) {
	svc, _, _, markets, outcomes, bets, wallets, walletTx := newTestSettlement(t)
	ml, totals := setupSettledFixture(markets, outcomes)

	// pending-ставки: на winning-home (won), losing-away (lost), push-totals (void).
	// Несколько ставок для проверки выплат по нескольким пользователям.
	pendingBets := []domain.Bet{
		{ID: 1, UserID: 5, OutcomeID: ml[0].ID, EventID: 1, Stake: 100, PotentialPayout: 200, Status: domain.BetPending}, // home won
		{ID: 2, UserID: 5, OutcomeID: ml[2].ID, EventID: 1, Stake: 50, PotentialPayout: 160, Status: domain.BetPending},  // away lost
		{ID: 3, UserID: 7, OutcomeID: totals[0].ID, EventID: 1, Stake: 80, PotentialPayout: 152, Status: domain.BetPending}, // over (3 > 2.5) won
		{ID: 4, UserID: 7, OutcomeID: totals[1].ID, EventID: 1, Stake: 80, PotentialPayout: 152, Status: domain.BetPending}, // under (3 > 2.5) lost
	}
	bets.listPendingFn = func(_ context.Context, _ []int64) ([]domain.Bet, error) {
		return pendingBets, nil
	}
	// Кошелёк: блокировка возвращает пустой кошелёк, UpdateBalance — накапливаемо.
	wallets.getForUpdFn = func(_ context.Context, userID int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: userID, Balance: 1000}, nil
	}
	wallets.updateBalFn = func(_ context.Context, _ int64, delta int64) (int64, error) { return delta, nil }
	walletTx.createFn = func(_ context.Context, _ domain.WalletTransaction) (int64, error) { return 1, nil }

	scores := scoresJSON(t, 2, 1) // home 2 : 1 away → home победил, total 3 > 2.5
	if err := svc.SettleEvent(context.Background(), 1, domain.EventSettled, scores); err != nil {
		t.Fatalf("SettleEvent: %v", err)
	}

	// Результаты исходов: home/over → won, draw/away/under → lost.
	if outcomes.resultUpdates[100] != domain.ResultWon {
		t.Errorf("outcome 100 (home) result = %v, want won", outcomes.resultUpdates[100])
	}
	if outcomes.resultUpdates[101] != domain.ResultLost {
		t.Errorf("outcome 101 (draw) result = %v, want lost", outcomes.resultUpdates[101])
	}
	if outcomes.resultUpdates[102] != domain.ResultLost {
		t.Errorf("outcome 102 (away) result = %v, want lost", outcomes.resultUpdates[102])
	}
	if outcomes.resultUpdates[110] != domain.ResultWon {
		t.Errorf("outcome 110 (over) result = %v, want won", outcomes.resultUpdates[110])
	}
	if outcomes.resultUpdates[111] != domain.ResultLost {
		t.Errorf("outcome 111 (under) result = %v, want lost", outcomes.resultUpdates[111])
	}

	// Статусы рынков → settled.
	if markets.statusUpdates[10] != domain.MarketSettled || markets.statusUpdates[11] != domain.MarketSettled {
		t.Errorf("market statuses = %v, want both settled", markets.statusUpdates)
	}

	// Выплаты: user 5 получил +200 (bet 1 won), bet 2 lost — ничего.
	// user 7 получил +152 (bet 3 won), bet 4 lost — ничего.
	assertBalanceSum := func(user int64, want int64) {
		t.Helper()
		var sum int64
		for _, u := range wallets.balanceByUser[user] {
			sum += u.Delta
		}
		if sum != want {
			t.Errorf("user %d balance sum = %d, want %d", user, sum, want)
		}
	}
	assertBalanceSum(5, 200)
	assertBalanceSum(7, 152)

	// Типы транзакций: bet_payout (а не refund), суммы совпадают с выплатами.
	// walletTx.lastCreated — последняя созданная tx; проверим, что был payout.
	if walletTx.lastCreated.Type != domain.TxBetPayout {
		t.Errorf("last tx type = %q, want bet_payout", walletTx.lastCreated.Type)
	}
	if walletTx.lastCreated.Amount != 152 {
		t.Errorf("last tx amount = %d, want 152", walletTx.lastCreated.Amount)
	}
	// Никаких bet_refund в этом сценарии не должно быть — проверим косвенно:
	// refund-ветка не сработала, т.к. push не произошёл (3 ≠ 2.5).

	// Статусы ставок: 4 вызова UpdateStatusSettled с правильными статусами.
	wantStatuses := map[int64]domain.BetStatus{
		1: domain.BetWon, 2: domain.BetLost, 3: domain.BetWon, 4: domain.BetLost,
	}
	if len(bets.settledCalls) != 4 {
		t.Fatalf("settledCalls = %d, want 4", len(bets.settledCalls))
	}
	for _, c := range bets.settledCalls {
		if wantStatuses[c.ID] != c.Status {
			t.Errorf("bet %d status = %q, want %q", c.ID, c.Status, wantStatuses[c.ID])
		}
		if c.SettledAt.IsZero() {
			t.Errorf("bet %d settled_at is zero", c.ID)
		}
	}

	// Событие переведено в settled + scores сохранены — проверяется отдельным
	// тестом TestSettlementService_SettleEvent_UpdatesEvent (там есть events).
}

// Дополнительно проверим, что eventRepo получил правильный вызов. Сервис держит
// ссылку на events, но newTestSettlement возвращает его вторым. Сделаем отдельный
// тест, где явно используем events для проверки UpdateStatusAndScores.
func TestSettlementService_SettleEvent_UpdatesEvent(t *testing.T) {
	svc, _, events, markets, outcomes, bets, wallets, _ := newTestSettlement(t)
	setupSettledFixture(markets, outcomes)
	bets.listPendingFn = func(_ context.Context, _ []int64) ([]domain.Bet, error) { return nil, nil }
	wallets.updateBalFn = func(_ context.Context, _ int64, d int64) (int64, error) { return d, nil }

	scores := scoresJSON(t, 0, 0)
	if err := svc.SettleEvent(context.Background(), 1, domain.EventSettled, scores); err != nil {
		t.Fatalf("SettleEvent: %v", err)
	}
	if len(events.statusScoresCalls) != 1 {
		t.Fatalf("event updates = %d, want 1", len(events.statusScoresCalls))
	}
	c := events.statusScoresCalls[0]
	if c.ID != 1 || c.Status != domain.EventSettled {
		t.Errorf("event update = %+v, want id=1 status=settled", c)
	}
	if string(c.Scores) != string(scores) {
		t.Errorf("event scores = %s, want %s", c.Scores, scores)
	}
}

// --- SettleEvent: TOTALS push → void ---

func TestSettlementService_SettleEvent_TotalsPush(t *testing.T) {
	svc, _, _, markets, outcomes, bets, wallets, walletTx := newTestSettlement(t)
	_, totals := setupSettledFixture(markets, outcomes)
	// Ставка на under по линии 2.5; счёт сделаем 1:1 → total 2 < 2.5 → under won.
	// Чтобы получить push, переопределим линию на 2.0 и счёт 1:1 (total 2 == 2.0).
	line2 := decimal.RequireFromString("2.0")
	markets.byEvent[1][1].Line = &line2
	bets.listPendingFn = func(_ context.Context, _ []int64) ([]domain.Bet, error) {
		return []domain.Bet{
			{ID: 9, UserID: 5, OutcomeID: totals[0].ID, Stake: 100, PotentialPayout: 190, Status: domain.BetPending}, // over
			{ID: 10, UserID: 5, OutcomeID: totals[1].ID, Stake: 100, PotentialPayout: 190, Status: domain.BetPending}, // under
		}, nil
	}
	wallets.getForUpdFn = func(_ context.Context, userID int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: userID, Balance: 1000}, nil
	}
	wallets.updateBalFn = func(_ context.Context, _ int64, d int64) (int64, error) { return d, nil }
	walletTx.createFn = func(_ context.Context, _ domain.WalletTransaction) (int64, error) { return 1, nil }

	scores := scoresJSON(t, 1, 1) // total 2 == line 2.0 → push
	if err := svc.SettleEvent(context.Background(), 1, domain.EventSettled, scores); err != nil {
		t.Fatalf("SettleEvent: %v", err)
	}
	// Оба исхода тотала → void (push).
	if outcomes.resultUpdates[totals[0].ID] != domain.ResultVoid || outcomes.resultUpdates[totals[1].ID] != domain.ResultVoid {
		t.Errorf("totals push results = %v/%v, want void/void",
			outcomes.resultUpdates[totals[0].ID], outcomes.resultUpdates[totals[1].ID])
	}
	// Возврат ставки: user 5 получает +100 + +100 = +200 (по stake каждой).
	var sum int64
	for _, u := range wallets.balanceByUser[5] {
		sum += u.Delta
	}
	if sum != 200 {
		t.Errorf("user 5 refund sum = %d, want 200 (2× stake 100)", sum)
	}
	if walletTx.lastCreated.Type != domain.TxBetRefund {
		t.Errorf("last tx type = %q, want bet_refund", walletTx.lastCreated.Type)
	}
	// Статусы обеих ставок → void.
	for _, c := range bets.settledCalls {
		if c.Status != domain.BetVoid {
			t.Errorf("bet %d status = %q, want void", c.ID, c.Status)
		}
	}
}

// --- SettleEvent: cancelled → всё void + возврат ---

func TestSettlementService_SettleEvent_Cancelled(t *testing.T) {
	svc, _, events, markets, outcomes, bets, wallets, walletTx := newTestSettlement(t)
	ml, totals := setupSettledFixture(markets, outcomes)
	bets.listPendingFn = func(_ context.Context, _ []int64) ([]domain.Bet, error) {
		return []domain.Bet{
			{ID: 1, UserID: 5, OutcomeID: ml[0].ID, Stake: 100, PotentialPayout: 200, Status: domain.BetPending},
			{ID: 2, UserID: 7, OutcomeID: totals[0].ID, Stake: 50, PotentialPayout: 95, Status: domain.BetPending},
		}, nil
	}
	wallets.getForUpdFn = func(_ context.Context, userID int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: userID, Balance: 1000}, nil
	}
	wallets.updateBalFn = func(_ context.Context, _ int64, d int64) (int64, error) { return d, nil }
	walletTx.createFn = func(_ context.Context, _ domain.WalletTransaction) (int64, error) { return 1, nil }

	// scores при cancel игнорируется.
	if err := svc.SettleEvent(context.Background(), 1, domain.EventCancelled, nil); err != nil {
		t.Fatalf("SettleEvent: %v", err)
	}

	// Все исходы → void.
	for _, id := range []int64{100, 101, 102, 110, 111} {
		if outcomes.resultUpdates[id] != domain.ResultVoid {
			t.Errorf("outcome %d result = %v, want void", id, outcomes.resultUpdates[id])
		}
	}
	// Все рынки → void.
	if markets.statusUpdates[10] != domain.MarketVoid || markets.statusUpdates[11] != domain.MarketVoid {
		t.Errorf("market statuses = %v, want both void", markets.statusUpdates)
	}
	// Возврат по stake: user 5 → +100, user 7 → +50.
	if sum := sumDeltas(wallets, 5); sum != 100 {
		t.Errorf("user 5 refund = %d, want 100", sum)
	}
	if sum := sumDeltas(wallets, 7); sum != 50 {
		t.Errorf("user 7 refund = %d, want 50", sum)
	}
	// Все ставки → void.
	for _, c := range bets.settledCalls {
		if c.Status != domain.BetVoid {
			t.Errorf("bet %d status = %q, want void", c.ID, c.Status)
		}
	}
	// Событие → cancelled.
	if len(events.statusScoresCalls) != 1 || events.statusScoresCalls[0].Status != domain.EventCancelled {
		t.Errorf("event updates = %+v, want 1 cancelled", events.statusScoresCalls)
	}
}

// --- SettleEvent: идемпотентность ---

func TestSettlementService_SettleEvent_Idempotent(t *testing.T) {
	svc, _, _, markets, outcomes, bets, wallets, walletTx := newTestSettlement(t)
	setupSettledFixture(markets, outcomes)
	scores := scoresJSON(t, 2, 1)

	// Первый прогон: одна pending-ставка.
	calls := 0
	bets.listPendingFn = func(_ context.Context, _ []int64) ([]domain.Bet, error) {
		calls++
		if calls == 1 {
			return []domain.Bet{{ID: 1, UserID: 5, OutcomeID: 100, Stake: 100, PotentialPayout: 200, Status: domain.BetPending}}, nil
		}
		// Второй прогон — pending-ставок уже нет (первая рассчитана).
		return nil, nil
	}
	wallets.getForUpdFn = func(_ context.Context, userID int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: userID, Balance: 1000}, nil
	}
	wallets.updateBalFn = func(_ context.Context, _ int64, d int64) (int64, error) { return d, nil }
	walletTx.createFn = func(_ context.Context, _ domain.WalletTransaction) (int64, error) { return 1, nil }

	if err := svc.SettleEvent(context.Background(), 1, domain.EventSettled, scores); err != nil {
		t.Fatalf("SettleEvent 1: %v", err)
	}
	firstUserDeltas := len(wallets.balanceByUser[5])

	if err := svc.SettleEvent(context.Background(), 1, domain.EventSettled, scores); err != nil {
		t.Fatalf("SettleEvent 2: %v", err)
	}
	// Второй прогон не должен добавлять движений по кошельку — pending-ставок нет.
	if got := len(wallets.balanceByUser[5]); got != firstUserDeltas {
		t.Errorf("second run added wallet movements: before=%d after=%d, want equal", firstUserDeltas, got)
	}
}

// --- SettleEvent: невалидный scores ---

func TestSettlementService_SettleEvent_InvalidScores(t *testing.T) {
	svc, _, _, markets, outcomes, bets, wallets, _ := newTestSettlement(t)
	setupSettledFixture(markets, outcomes)
	bets.listPendingFn = func(_ context.Context, _ []int64) ([]domain.Bet, error) { return nil, nil }
	wallets.updateBalFn = func(_ context.Context, _ int64, d int64) (int64, error) { return d, nil }

	// scores отсутствует — событие settled, но данных для расчёта нет.
	err := svc.SettleEvent(context.Background(), 1, domain.EventSettled, nil)
	if !errors.Is(err, ErrScoresUnavailable) {
		t.Fatalf("err = %v, want ErrScoresUnavailable", err)
	}
	// Ничего не должно было измениться: ни результатов, ни статусов, ни ставок.
	if len(outcomes.resultUpdates) != 0 {
		t.Errorf("no result updates expected, got %d", len(outcomes.resultUpdates))
	}
	if len(bets.settledCalls) != 0 {
		t.Errorf("no settled calls expected, got %d", len(bets.settledCalls))
	}
}

// sumDeltas — хелпер: сумма всех дельт баланса пользователя в фейке.
func sumDeltas(w *fakeWalletRepo, user int64) int64 {
	var sum int64
	for _, u := range w.balanceByUser[user] {
		sum += u.Delta
	}
	return sum
}

// --- SettleCustomEvent: ручной расчёт по winning_outcome_id ---

// setupCustomFixture готовит кастомное событие с одним CUSTOM-рынком (id 20) и
// тремя исходами (opt_1/opt_2/opt_3). Возвращает исходы для сборки ставок.
func setupCustomFixture(markets *fakeMarketRepo, outcomes *fakeOutcomeRepo) []domain.Outcome {
	customOutcomes := []domain.Outcome{
		{ID: 200, MarketID: 20, Code: "opt_1", Odds: decimal.RequireFromString("2.00")},
		{ID: 201, MarketID: 20, Code: "opt_2", Odds: decimal.RequireFromString("3.00")},
		{ID: 202, MarketID: 20, Code: "opt_3", Odds: decimal.RequireFromString("4.00")},
	}
	if markets.byEvent == nil {
		markets.byEvent = map[int64][]domain.Market{}
	}
	if outcomes.byMarket == nil {
		outcomes.byMarket = map[int64][]domain.Outcome{}
	}
	markets.byEvent[1] = []domain.Market{
		{ID: 20, EventID: 1, Type: domain.MarketCustom, Status: domain.MarketOpen},
	}
	outcomes.byMarket[20] = customOutcomes
	return customOutcomes
}

func TestSettlementService_SettleCustomEvent_WonLost(t *testing.T) {
	svc, _, events, markets, outcomes, bets, wallets, walletTx := newTestSettlement(t)
	ocs := setupCustomFixture(markets, outcomes)

	// Ставки: на победителя (200), на проигравших (201, 202).
	pendingBets := []domain.Bet{
		{ID: 1, UserID: 5, OutcomeID: ocs[0].ID, Stake: 100, PotentialPayout: 200, Status: domain.BetPending}, // won
		{ID: 2, UserID: 7, OutcomeID: ocs[1].ID, Stake: 50, PotentialPayout: 150, Status: domain.BetPending},  // lost
		{ID: 3, UserID: 7, OutcomeID: ocs[2].ID, Stake: 30, PotentialPayout: 120, Status: domain.BetPending},  // lost
	}
	bets.listPendingFn = func(_ context.Context, _ []int64) ([]domain.Bet, error) {
		return pendingBets, nil
	}
	wallets.getForUpdFn = func(_ context.Context, userID int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: userID, Balance: 1000}, nil
	}
	wallets.updateBalFn = func(_ context.Context, _ int64, d int64) (int64, error) { return d, nil }
	walletTx.createFn = func(_ context.Context, _ domain.WalletTransaction) (int64, error) { return 1, nil }

	// Победил исход 200.
	if err := svc.SettleCustomEvent(context.Background(), 1, 200); err != nil {
		t.Fatalf("SettleCustomEvent: %v", err)
	}

	// Результаты исходов: 200 → won, 201/202 → lost.
	if outcomes.resultUpdates[200] != domain.ResultWon {
		t.Errorf("outcome 200 result = %v, want won", outcomes.resultUpdates[200])
	}
	if outcomes.resultUpdates[201] != domain.ResultLost || outcomes.resultUpdates[202] != domain.ResultLost {
		t.Errorf("losing outcomes = %v/%v, want lost/lost",
			outcomes.resultUpdates[201], outcomes.resultUpdates[202])
	}

	// Рынок → settled.
	if markets.statusUpdates[20] != domain.MarketSettled {
		t.Errorf("market status = %v, want settled", markets.statusUpdates[20])
	}

	// Выплата: user 5 → +200 (bet 1 won). user 7 — ничего (оба lost).
	if sum := sumDeltas(wallets, 5); sum != 200 {
		t.Errorf("user 5 payout = %d, want 200", sum)
	}
	if sum := sumDeltas(wallets, 7); sum != 0 {
		t.Errorf("user 7 payout = %d, want 0 (both lost)", sum)
	}

	// Статусы ставок: 1 → won, 2/3 → lost.
	wantStatuses := map[int64]domain.BetStatus{1: domain.BetWon, 2: domain.BetLost, 3: domain.BetLost}
	if len(bets.settledCalls) != 3 {
		t.Fatalf("settledCalls = %d, want 3", len(bets.settledCalls))
	}
	for _, c := range bets.settledCalls {
		if wantStatuses[c.ID] != c.Status {
			t.Errorf("bet %d status = %q, want %q", c.ID, c.Status, wantStatuses[c.ID])
		}
	}

	// Событие → settled, scores = nil (custom не имеет счёта).
	if len(events.statusScoresCalls) != 1 || events.statusScoresCalls[0].Status != domain.EventSettled {
		t.Fatalf("event updates = %+v, want 1 settled", events.statusScoresCalls)
	}
	if events.statusScoresCalls[0].Scores != nil {
		t.Errorf("custom event scores = %v, want nil", events.statusScoresCalls[0].Scores)
	}
}

func TestSettlementService_SettleCustomEvent_WinnerNotInMarket(t *testing.T) {
	svc, _, _, markets, outcomes, bets, wallets, walletTx := newTestSettlement(t)
	ocs := setupCustomFixture(markets, outcomes)

	// Ставка на исход рынка.
	bets.listPendingFn = func(_ context.Context, _ []int64) ([]domain.Bet, error) {
		return []domain.Bet{
			{ID: 1, UserID: 5, OutcomeID: ocs[0].ID, Stake: 100, PotentialPayout: 200, Status: domain.BetPending},
		}, nil
	}
	wallets.getForUpdFn = func(_ context.Context, userID int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: userID, Balance: 1000}, nil
	}
	wallets.updateBalFn = func(_ context.Context, _ int64, d int64) (int64, error) { return d, nil }
	walletTx.createFn = func(_ context.Context, _ domain.WalletTransaction) (int64, error) { return 1, nil }

	// Победил несуществующий в рынке исход (999) — весь рынок → void, ставка возвращена.
	if err := svc.SettleCustomEvent(context.Background(), 1, 999); err != nil {
		t.Fatalf("SettleCustomEvent: %v", err)
	}

	for _, id := range []int64{200, 201, 202} {
		if outcomes.resultUpdates[id] != domain.ResultVoid {
			t.Errorf("outcome %d result = %v, want void (winner not in market)", id, outcomes.resultUpdates[id])
		}
	}
	if markets.statusUpdates[20] != domain.MarketVoid {
		t.Errorf("market status = %v, want void", markets.statusUpdates[20])
	}
	// Возврат stake.
	if sum := sumDeltas(wallets, 5); sum != 100 {
		t.Errorf("user 5 refund = %d, want 100", sum)
	}
	if bets.settledCalls[0].Status != domain.BetVoid {
		t.Errorf("bet status = %q, want void", bets.settledCalls[0].Status)
	}
}

// Идемпотентность SettleCustomEvent: второй прогон не добавляет выплат.
func TestSettlementService_SettleCustomEvent_Idempotent(t *testing.T) {
	svc, _, _, markets, outcomes, bets, wallets, walletTx := newTestSettlement(t)
	ocs := setupCustomFixture(markets, outcomes)

	calls := 0
	bets.listPendingFn = func(_ context.Context, _ []int64) ([]domain.Bet, error) {
		calls++
		if calls == 1 {
			return []domain.Bet{
				{ID: 1, UserID: 5, OutcomeID: ocs[0].ID, Stake: 100, PotentialPayout: 200, Status: domain.BetPending},
			}, nil
		}
		return nil, nil // второй прогон — pending нет
	}
	wallets.getForUpdFn = func(_ context.Context, userID int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: userID, Balance: 1000}, nil
	}
	wallets.updateBalFn = func(_ context.Context, _ int64, d int64) (int64, error) { return d, nil }
	walletTx.createFn = func(_ context.Context, _ domain.WalletTransaction) (int64, error) { return 1, nil }

	if err := svc.SettleCustomEvent(context.Background(), 1, 200); err != nil {
		t.Fatalf("SettleCustomEvent 1: %v", err)
	}
	firstDeltas := len(wallets.balanceByUser[5])

	if err := svc.SettleCustomEvent(context.Background(), 1, 200); err != nil {
		t.Fatalf("SettleCustomEvent 2: %v", err)
	}
	if got := len(wallets.balanceByUser[5]); got != firstDeltas {
		t.Errorf("second run added movements: before=%d after=%d, want equal", firstDeltas, got)
	}
}
