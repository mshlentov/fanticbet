package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"fanticbet/internal/domain"

	"github.com/shopspring/decimal"
)

// newTestAdmin собирает AdminService с инъекцией моков и реальным
// SettlementService (на тех же моках репозиториев) — так можно тестировать
// делегирование cancel/settle end-to-end без отдельного интерфейса-заглушки.
func newTestAdmin(t *testing.T) (
	*AdminService,
	*fakeTxRunner,
	*fakeEventRepo,
	*fakeMarketRepo,
	*fakeOutcomeRepo,
	*fakeWalletRepo,
	*fakeWalletTxRepo,
) {
	t.Helper()
	tx := &fakeTxRunner{}
	events := &fakeEventRepo{}
	markets := &fakeMarketRepo{}
	outcomes := &fakeOutcomeRepo{}
	wallets := &fakeWalletRepo{}
	walletTx := &fakeWalletTxRepo{}

	settlement := NewSettlementService(tx, events, markets, outcomes,
		&fakeBetRepo{}, wallets, walletTx, nil)
	svc := NewAdminService(tx, events, markets, outcomes, wallets, walletTx, settlement, nil)
	return svc, tx, events, markets, outcomes, wallets, walletTx
}

// decPtr — хелпер для *decimal.Decimal в инпутах. strPtr уже объявлен в
// oauth_service_test.go (тот же пакет).
func decPtr(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}

// validCreateInput — корректное тело создания кастомного события (3 исхода).
func validCreateInput() CustomEventInput {
	return CustomEventInput{
		Title:    "Кто выиграет турнир?",
		StartsAt: time.Now().Add(24 * time.Hour),
		Market: CustomMarketInput{
			Question: strPtr("Выберите победителя"),
			Outcomes: []CustomOutcomeInput{
				{Label: strPtr("Вариант А"), Odds: decPtr("2.00")},
				{Label: strPtr("Вариант Б"), Odds: decPtr("3.00")},
				{Label: strPtr("Вариант В"), Odds: decPtr("4.00")},
			},
		},
	}
}

// --- CreateCustomEvent ---

func TestAdminService_CreateCustomEvent_Success(t *testing.T) {
	svc, tx, events, markets, outcomes, _, _ := newTestAdmin(t)
	input := validCreateInput()

	result, err := svc.CreateCustomEvent(context.Background(), 77, input)
	if err != nil {
		t.Fatalf("CreateCustomEvent: %v", err)
	}

	if tx.calls != 1 {
		t.Errorf("expected 1 tx, got %d", tx.calls)
	}

	// Событие: custom, sport_slug=custom, upcoming, created_by=77.
	if len(events.createCalls) != 1 {
		t.Fatalf("createCalls = %d, want 1", len(events.createCalls))
	}
	ev := events.createCalls[0]
	if ev.Source != domain.SourceCustom {
		t.Errorf("source = %s, want custom", ev.Source)
	}
	if ev.SportSlug != string(domain.SourceCustom) {
		t.Errorf("sport_slug = %s, want custom", ev.SportSlug)
	}
	if ev.Status != domain.EventUpcoming {
		t.Errorf("status = %s, want upcoming", ev.Status)
	}
	if ev.CreatedBy == nil || *ev.CreatedBy != 77 {
		t.Errorf("created_by = %v, want 77", ev.CreatedBy)
	}
	if ev.ExternalID != nil {
		t.Errorf("external_id = %v, want nil for custom", ev.ExternalID)
	}

	// Рынок CUSTOM, open, с вопросом.
	if len(markets.createCalls) != 1 {
		t.Fatalf("market createCalls = %d, want 1", len(markets.createCalls))
	}
	m := markets.createCalls[0]
	if m.Type != domain.MarketCustom {
		t.Errorf("market type = %s, want CUSTOM", m.Type)
	}
	if m.Status != domain.MarketOpen {
		t.Errorf("market status = %s, want open", m.Status)
	}

	// 3 исхода с кодами opt_1/opt_2/opt_3 и коэффициентами из инпута.
	if len(outcomes.upsertCalls) != 3 {
		t.Fatalf("outcome upsertCalls = %d, want 3", len(outcomes.upsertCalls))
	}
	wantCodes := []domain.OutcomeCode{"opt_1", "opt_2", "opt_3"}
	for i, oc := range outcomes.upsertCalls {
		if oc.Code != wantCodes[i] {
			t.Errorf("outcome[%d] code = %s, want %s", i, oc.Code, wantCodes[i])
		}
	}
	if !outcomes.upsertCalls[0].Odds.Equal(decimal.RequireFromString("2.00")) {
		t.Errorf("outcome[0] odds = %s, want 2.00", outcomes.upsertCalls[0].Odds)
	}

	// Возвращённый результат содержит проставленные id события/рынка/исходов.
	if result.Event.ID == 0 || result.Market.ID == 0 {
		t.Errorf("result ids not set: event=%d market=%d", result.Event.ID, result.Market.ID)
	}
	if len(result.Outcomes) != 3 || result.Outcomes[0].ID == 0 {
		t.Errorf("result outcomes = %+v, want 3 with ids", result.Outcomes)
	}
}

func TestAdminService_CreateCustomEvent_TooFewOutcomes(t *testing.T) {
	svc, tx, _, _, _, _, _ := newTestAdmin(t)
	input := validCreateInput()
	input.Market.Outcomes = input.Market.Outcomes[:1] // один исход

	_, err := svc.CreateCustomEvent(context.Background(), 1, input)
	if !errors.Is(err, domain.ErrBetOutOfRange) {
		t.Errorf("err = %v, want ErrBetOutOfRange", err)
	}
	// Невалидный запрос не должен открывать транзакцию.
	if tx.calls != 0 {
		t.Errorf("tx calls = %d, want 0 for invalid input", tx.calls)
	}
}

func TestAdminService_CreateCustomEvent_InvalidOdds(t *testing.T) {
	svc, tx, _, _, _, _, _ := newTestAdmin(t)
	input := validCreateInput()
	// odds = 1.0 (не > 1.0) — нарушает CHECK odds > 1.0.
	input.Market.Outcomes[0].Odds = decPtr("1.0")

	_, err := svc.CreateCustomEvent(context.Background(), 1, input)
	if !errors.Is(err, domain.ErrBetOutOfRange) {
		t.Errorf("err = %v, want ErrBetOutOfRange", err)
	}
	if tx.calls != 0 {
		t.Errorf("tx calls = %d, want 0 for invalid odds", tx.calls)
	}
}

// --- EditEvent ---

func TestAdminService_EditEvent_NotCustom(t *testing.T) {
	svc, _, events, _, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceOddsAPI, Status: domain.EventUpcoming}, nil
	}

	err := svc.EditEvent(context.Background(), 1, EditEventInput{Title: strPtr("new")})
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("err = %v, want ErrMarketClosed for non-custom", err)
	}
}

func TestAdminService_EditEvent_AlreadySettled(t *testing.T) {
	svc, _, events, _, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceCustom, Status: domain.EventSettled}, nil
	}

	err := svc.EditEvent(context.Background(), 1, EditEventInput{Title: strPtr("new")})
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("err = %v, want ErrMarketClosed for settled", err)
	}
}

func TestAdminService_EditEvent_UpdatesFields(t *testing.T) {
	svc, _, events, markets, outcomes, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceCustom, Status: domain.EventUpcoming}, nil
	}
	markets.byEvent = map[int64][]domain.Market{
		1: {{ID: 20, EventID: 1, Type: domain.MarketCustom}},
	}

	newTitle := "Новое название"
	newStart := time.Now().Add(48 * time.Hour)
	newQuestion := "Новый вопрос"
	err := svc.EditEvent(context.Background(), 1, EditEventInput{
		Title:    &newTitle,
		StartsAt: &newStart,
		Question: &newQuestion,
		Outcomes: []EditOutcomeInput{
			{ID: 100, Odds: decPtr("2.50")},
			{ID: 101, Label: strPtr("Переименован")},
		},
	})
	if err != nil {
		t.Fatalf("EditEvent: %v", err)
	}

	// Поля события обновлены.
	if len(events.updateDetailsCalls) != 1 {
		t.Fatalf("updateDetailsCalls = %d, want 1", len(events.updateDetailsCalls))
	}
	d := events.updateDetailsCalls[0]
	if d.Title == nil || *d.Title != newTitle {
		t.Errorf("title update = %v, want %q", d.Title, newTitle)
	}
	if d.StartsAt == nil || !d.StartsAt.Equal(newStart) {
		t.Errorf("starts_at update = %v, want %v", d.StartsAt, newStart)
	}

	// Вопрос обновлён.
	if markets.questionCalls[20] == nil || *markets.questionCalls[20] != newQuestion {
		t.Errorf("question update = %v, want %q", markets.questionCalls[20], newQuestion)
	}

	// Исходы обновлены: 100 → odds 2.50, 101 → label «Переименован».
	if outcomes.labelOddsCalls[100].Odds == nil || !outcomes.labelOddsCalls[100].Odds.Equal(decimal.RequireFromString("2.50")) {
		t.Errorf("outcome 100 odds = %v, want 2.50", outcomes.labelOddsCalls[100].Odds)
	}
	if outcomes.labelOddsCalls[101].Label == nil || *outcomes.labelOddsCalls[101].Label != "Переименован" {
		t.Errorf("outcome 101 label = %v, want Переименован", outcomes.labelOddsCalls[101].Label)
	}
}

func TestAdminService_EditEvent_InvalidOdds(t *testing.T) {
	svc, _, events, _, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceCustom, Status: domain.EventUpcoming}, nil
	}

	err := svc.EditEvent(context.Background(), 1, EditEventInput{
		Outcomes: []EditOutcomeInput{{ID: 100, Odds: decPtr("0.90")}},
	})
	if !errors.Is(err, domain.ErrBetOutOfRange) {
		t.Errorf("err = %v, want ErrBetOutOfRange", err)
	}
}

// --- CancelEvent ---

func TestAdminService_CancelEvent_DelegatesToSettlement(t *testing.T) {
	svc, _, events, markets, outcomes, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceCustom, Status: domain.EventUpcoming}, nil
	}
	// Рынок с исходами — settlement.buildPlan их загрузит.
	markets.byEvent = map[int64][]domain.Market{
		1: {{ID: 20, EventID: 1, Type: domain.MarketCustom, Status: domain.MarketOpen}},
	}
	outcomes.byMarket = map[int64][]domain.Outcome{
		20: {{ID: 200, MarketID: 20, Code: "opt_1"}},
	}

	err := svc.CancelEvent(context.Background(), 1)
	if err != nil {
		t.Fatalf("CancelEvent: %v", err)
	}
	// Settlement.SettleEvent(cancelled) помечает все исходы void и рынок void.
	if outcomes.resultUpdates[200] != domain.ResultVoid {
		t.Errorf("outcome result = %v, want void", outcomes.resultUpdates[200])
	}
	if markets.statusUpdates[20] != domain.MarketVoid {
		t.Errorf("market status = %v, want void", markets.statusUpdates[20])
	}
	// Событие → cancelled.
	if len(events.statusScoresCalls) != 1 || events.statusScoresCalls[0].Status != domain.EventCancelled {
		t.Errorf("event updates = %+v, want 1 cancelled", events.statusScoresCalls)
	}
}

func TestAdminService_CancelEvent_AlreadyCancelled(t *testing.T) {
	svc, _, events, _, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceCustom, Status: domain.EventCancelled}, nil
	}

	err := svc.CancelEvent(context.Background(), 1)
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("err = %v, want ErrMarketClosed", err)
	}
}

// --- SettleCustom ---

func TestAdminService_SettleCustom_DelegatesToSettlement(t *testing.T) {
	svc, _, events, markets, outcomes, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceCustom, Status: domain.EventUpcoming}, nil
	}
	markets.byEvent = map[int64][]domain.Market{
		1: {{ID: 20, EventID: 1, Type: domain.MarketCustom, Status: domain.MarketOpen}},
	}
	outcomes.byMarket = map[int64][]domain.Outcome{
		20: {
			{ID: 200, MarketID: 20, Code: "opt_1"},
			{ID: 201, MarketID: 20, Code: "opt_2"},
		},
	}

	err := svc.SettleCustom(context.Background(), 1, 200)
	if err != nil {
		t.Fatalf("SettleCustom: %v", err)
	}
	// Победитель → won, прочий → lost.
	if outcomes.resultUpdates[200] != domain.ResultWon {
		t.Errorf("winner result = %v, want won", outcomes.resultUpdates[200])
	}
	if outcomes.resultUpdates[201] != domain.ResultLost {
		t.Errorf("loser result = %v, want lost", outcomes.resultUpdates[201])
	}
	if len(events.statusScoresCalls) != 1 || events.statusScoresCalls[0].Status != domain.EventSettled {
		t.Errorf("event updates = %+v, want 1 settled", events.statusScoresCalls)
	}
}

func TestAdminService_SettleCustom_NotCustom(t *testing.T) {
	svc, _, events, _, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceOddsAPI, Status: domain.EventUpcoming}, nil
	}

	err := svc.SettleCustom(context.Background(), 1, 200)
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("err = %v, want ErrMarketClosed for non-custom", err)
	}
}

// --- AdjustBalance ---

func TestAdminService_AdjustBalance_SuccessPositive(t *testing.T) {
	svc, _, _, _, _, wallets, walletTx := newTestAdmin(t)
	wallets.getForUpdFn = func(_ context.Context, userID int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: userID, Balance: 1000}, nil
	}
	wallets.updateBalFn = func(_ context.Context, _ int64, delta int64) (int64, error) {
		return 1000 + delta, nil
	}
	walletTx.createFn = func(_ context.Context, _ domain.WalletTransaction) (int64, error) { return 1, nil }

	bal, err := svc.AdjustBalance(context.Background(), 5, 5000, "компенсация")
	if err != nil {
		t.Fatalf("AdjustBalance: %v", err)
	}
	if bal != 6000 {
		t.Errorf("balance = %d, want 6000", bal)
	}
	// Запись admin_adjust с правильным balance_after.
	if walletTx.lastCreated.Type != domain.TxAdminAdjust {
		t.Errorf("tx type = %q, want admin_adjust", walletTx.lastCreated.Type)
	}
	if walletTx.lastCreated.Amount != 5000 {
		t.Errorf("tx amount = %d, want 5000", walletTx.lastCreated.Amount)
	}
	if walletTx.lastCreated.BalanceAfter != 6000 {
		t.Errorf("tx balance_after = %d, want 6000", walletTx.lastCreated.BalanceAfter)
	}
	if walletTx.lastCreated.BetID != nil {
		t.Errorf("tx bet_id = %v, want nil for admin_adjust", walletTx.lastCreated.BetID)
	}
}

func TestAdminService_AdjustBalance_SuccessNegative(t *testing.T) {
	svc, _, _, _, _, wallets, walletTx := newTestAdmin(t)
	wallets.getForUpdFn = func(_ context.Context, userID int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: userID, Balance: 1000}, nil
	}
	wallets.updateBalFn = func(_ context.Context, _ int64, delta int64) (int64, error) {
		return 1000 + delta, nil
	}
	walletTx.createFn = func(_ context.Context, _ domain.WalletTransaction) (int64, error) { return 1, nil }

	// Списание 300 с баланса 1000 — допустимо (итог 700 ≥ 0).
	bal, err := svc.AdjustBalance(context.Background(), 5, -300, "штраф")
	if err != nil {
		t.Fatalf("AdjustBalance: %v", err)
	}
	if bal != 700 {
		t.Errorf("balance = %d, want 700", bal)
	}
	if walletTx.lastCreated.Amount != -300 {
		t.Errorf("tx amount = %d, want -300", walletTx.lastCreated.Amount)
	}
}

func TestAdminService_AdjustBalance_InsufficientBalance(t *testing.T) {
	svc, _, _, _, _, wallets, walletTx := newTestAdmin(t)
	wallets.getForUpdFn = func(_ context.Context, userID int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: userID, Balance: 100}, nil
	}
	updateCalled := false
	wallets.updateBalFn = func(_ context.Context, _ int64, _ int64) (int64, error) {
		updateCalled = true
		return 0, nil
	}
	walletTx.createFn = func(_ context.Context, _ domain.WalletTransaction) (int64, error) { return 1, nil }

	// Списание 500 с баланса 100 → итог -400 < 0 — отказ.
	_, err := svc.AdjustBalance(context.Background(), 5, -500, "штраф")
	if !errors.Is(err, domain.ErrInsufficientBalance) {
		t.Errorf("err = %v, want ErrInsufficientBalance", err)
	}
	// Баланс не должен меняться, запись не должна создаваться.
	if updateCalled {
		t.Error("UpdateBalance must not be called when result balance < 0")
	}
}

func TestAdminService_AdjustBalance_ZeroAmount(t *testing.T) {
	svc, tx, _, _, _, _, _ := newTestAdmin(t)

	_, err := svc.AdjustBalance(context.Background(), 5, 0, "бездействие")
	if !errors.Is(err, domain.ErrBetOutOfRange) {
		t.Errorf("err = %v, want ErrBetOutOfRange for zero amount", err)
	}
	// Нулевая корректировка не должна открывать транзакцию.
	if tx.calls != 0 {
		t.Errorf("tx calls = %d, want 0", tx.calls)
	}
}

func TestAdminService_AdjustBalance_LocksWallet(t *testing.T) {
	svc, _, _, _, _, wallets, walletTx := newTestAdmin(t)
	forUpdateCalled := false
	wallets.getForUpdFn = func(_ context.Context, userID int64) (domain.Wallet, error) {
		forUpdateCalled = true
		return domain.Wallet{UserID: userID, Balance: 1000}, nil
	}
	wallets.updateBalFn = func(_ context.Context, _ int64, delta int64) (int64, error) {
		return 1000 + delta, nil
	}
	walletTx.createFn = func(_ context.Context, _ domain.WalletTransaction) (int64, error) { return 1, nil }

	if _, err := svc.AdjustBalance(context.Background(), 5, 100, "тест"); err != nil {
		t.Fatalf("AdjustBalance: %v", err)
	}
	// FOR UPDATE обязателен перед изменением баланса (conventions §7-8).
	if !forUpdateCalled {
		t.Error("GetByUserIDForUpdate (FOR UPDATE) must be called before UpdateBalance")
	}
}
