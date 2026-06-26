package service

import (
	"context"
	"encoding/json"
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
	*fakeLeagueRepo,
	*fakeWalletRepo,
	*fakeWalletTxRepo,
) {
	t.Helper()
	tx := &fakeTxRunner{}
	events := &fakeEventRepo{}
	markets := &fakeMarketRepo{}
	outcomes := &fakeOutcomeRepo{}
	leagues := &fakeLeagueRepo{}
	wallets := &fakeWalletRepo{}
	walletTx := &fakeWalletTxRepo{}

	settlement := NewSettlementService(tx, events, markets, outcomes,
		&fakeBetRepo{}, wallets, walletTx, nil)
	svc := NewAdminService(tx, events, markets, outcomes, leagues, wallets, walletTx, settlement, nil)
	return svc, tx, events, markets, outcomes, leagues, wallets, walletTx
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
	svc, tx, events, markets, outcomes, _, _, _ := newTestAdmin(t)
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
	svc, tx, _, _, _, _, _, _ := newTestAdmin(t)
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
	svc, tx, _, _, _, _, _, _ := newTestAdmin(t)
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
	svc, _, events, _, _, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceOddsAPI, Status: domain.EventUpcoming}, nil
	}

	err := svc.EditEvent(context.Background(), 1, EditEventInput{Title: strPtr("new")})
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("err = %v, want ErrMarketClosed for non-custom", err)
	}
}

func TestAdminService_EditEvent_AlreadySettled(t *testing.T) {
	svc, _, events, _, _, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceCustom, Status: domain.EventSettled}, nil
	}

	err := svc.EditEvent(context.Background(), 1, EditEventInput{Title: strPtr("new")})
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("err = %v, want ErrMarketClosed for settled", err)
	}
}

func TestAdminService_EditEvent_UpdatesFields(t *testing.T) {
	svc, _, events, markets, outcomes, _, _, _ := newTestAdmin(t)
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
	svc, _, events, _, _, _, _, _ := newTestAdmin(t)
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
	svc, _, events, markets, outcomes, _, _, _ := newTestAdmin(t)
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
	svc, _, events, _, _, _, _, _ := newTestAdmin(t)
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
	svc, _, events, markets, outcomes, _, _, _ := newTestAdmin(t)
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
	svc, _, events, _, _, _, _, _ := newTestAdmin(t)
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
	svc, _, _, _, _, _, wallets, walletTx := newTestAdmin(t)
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
	svc, _, _, _, _, _, wallets, walletTx := newTestAdmin(t)
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
	svc, _, _, _, _, _, wallets, walletTx := newTestAdmin(t)
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
	svc, tx, _, _, _, _, _, _ := newTestAdmin(t)

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
	svc, _, _, _, _, _, wallets, walletTx := newTestAdmin(t)
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

// --- CreateLeague ---

func TestAdminService_CreateLeague_Success(t *testing.T) {
	svc, _, _, _, _, leagues, _, _ := newTestAdmin(t)

	league, err := svc.CreateLeague(context.Background(), CreateLeagueInput{
		Name: "Английская Премьер-лига", SportSlug: "football",
	})
	if err != nil {
		t.Fatalf("CreateLeague: %v", err)
	}
	if league.ID == 0 {
		t.Errorf("league id = %d, want non-zero", league.ID)
	}
	if league.Name != "Английская Премьер-лига" || league.SportSlug != "football" {
		t.Errorf("league = %+v", league)
	}
	// Репозиторий получил ровно то, что пришло из DTO.
	if len(leagues.createCalls) != 1 {
		t.Fatalf("createCalls = %d, want 1", len(leagues.createCalls))
	}
	if leagues.createCalls[0].Name != "Английская Премьер-лига" {
		t.Errorf("repo name = %q", leagues.createCalls[0].Name)
	}
}

func TestAdminService_CreateLeague_EmptyName(t *testing.T) {
	svc, _, _, _, _, _, _, _ := newTestAdmin(t)

	_, err := svc.CreateLeague(context.Background(), CreateLeagueInput{Name: "", SportSlug: "football"})
	if !errors.Is(err, domain.ErrBetOutOfRange) {
		t.Errorf("err = %v, want ErrBetOutOfRange for empty name", err)
	}
}

func TestAdminService_CreateLeague_EmptySportSlug(t *testing.T) {
	svc, _, _, _, _, leagues, _, _ := newTestAdmin(t)

	_, err := svc.CreateLeague(context.Background(), CreateLeagueInput{Name: "АПЛ", SportSlug: ""})
	if !errors.Is(err, domain.ErrBetOutOfRange) {
		t.Errorf("err = %v, want ErrBetOutOfRange for empty sport_slug", err)
	}
	// Невалидный запрос не должен дойти до репозитория.
	if len(leagues.createCalls) != 0 {
		t.Errorf("createCalls = %d, want 0 for invalid input", len(leagues.createCalls))
	}
}

// --- UpdateLeague ---

func TestAdminService_UpdateLeague_Success(t *testing.T) {
	svc, _, _, _, _, leagues, _, _ := newTestAdmin(t)

	newName := "Серия А"
	err := svc.UpdateLeague(context.Background(), 7, UpdateLeagueInput{Name: &newName})
	if err != nil {
		t.Fatalf("UpdateLeague: %v", err)
	}
	if len(leagues.updateCalls) != 1 {
		t.Fatalf("updateCalls = %d, want 1", len(leagues.updateCalls))
	}
	if leagues.updateCalls[0].ID != 7 {
		t.Errorf("update id = %d, want 7", leagues.updateCalls[0].ID)
	}
	if leagues.updateCalls[0].Name == nil || *leagues.updateCalls[0].Name != newName {
		t.Errorf("update name = %v, want %q", leagues.updateCalls[0].Name, newName)
	}
	if leagues.updateCalls[0].SportSlug != nil {
		t.Errorf("update sport_slug = %v, want nil (unchanged)", leagues.updateCalls[0].SportSlug)
	}
}

func TestAdminService_UpdateLeague_NotFound(t *testing.T) {
	svc, _, _, _, _, leagues, _, _ := newTestAdmin(t)
	leagues.updateErr = domain.ErrNotFound

	err := svc.UpdateLeague(context.Background(), 999, UpdateLeagueInput{Name: strPtr("x")})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestAdminService_UpdateLeague_EmptyName(t *testing.T) {
	svc, _, _, _, _, _, _, _ := newTestAdmin(t)
	empty := ""

	err := svc.UpdateLeague(context.Background(), 1, UpdateLeagueInput{Name: &empty})
	if !errors.Is(err, domain.ErrBetOutOfRange) {
		t.Errorf("err = %v, want ErrBetOutOfRange for empty name", err)
	}
}

// --- DeleteLeague ---

func TestAdminService_DeleteLeague_Success(t *testing.T) {
	svc, _, _, _, _, leagues, _, _ := newTestAdmin(t)
	// countEvents = 0 по умолчанию (fakeLeagueRepo) → удаление разрешено.

	err := svc.DeleteLeague(context.Background(), 3)
	if err != nil {
		t.Fatalf("DeleteLeague: %v", err)
	}
	if len(leagues.deleteCalls) != 1 || leagues.deleteCalls[0] != 3 {
		t.Errorf("deleteCalls = %v, want [3]", leagues.deleteCalls)
	}
}

func TestAdminService_DeleteLeague_BlockedByEvents(t *testing.T) {
	svc, _, _, _, _, leagues, _, _ := newTestAdmin(t)
	leagues.countEvents = 5 // к лиге привязаны 5 событий

	err := svc.DeleteLeague(context.Background(), 3)
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("err = %v, want ErrConflict (409)", err)
	}
	// Блокировка происходит до вызова Delete — репозиторий не должен удалять.
	if len(leagues.deleteCalls) != 0 {
		t.Errorf("deleteCalls = %v, want none when league has events", leagues.deleteCalls)
	}
}

func TestAdminService_DeleteLeague_NotFound(t *testing.T) {
	svc, _, _, _, _, leagues, _, _ := newTestAdmin(t)
	leagues.deleteErr = domain.ErrNotFound // лиги не существует

	err := svc.DeleteLeague(context.Background(), 999)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// --- ListLeagues ---

func TestAdminService_ListLeagues_DelegatesToRepo(t *testing.T) {
	svc, _, _, _, _, leagues, _, _ := newTestAdmin(t)
	var gotFilter string
	leagues.listFn = func(_ context.Context, sportSlug string) ([]domain.League, error) {
		gotFilter = sportSlug
		return []domain.League{{ID: 1, Name: "АПЛ", SportSlug: "football"}}, nil
	}

	got, err := svc.ListLeagues(context.Background(), "football")
	if err != nil {
		t.Fatalf("ListLeagues: %v", err)
	}
	if gotFilter != "football" {
		t.Errorf("filter = %q, want football", gotFilter)
	}
	if len(got) != 1 || got[0].Name != "АПЛ" {
		t.Errorf("leagues = %+v, want 1 «АПЛ»", got)
	}
}

// --- Спортивные матчи (source='manual', M8) ---

// setupMatchLeague настраивает фейк лиги так, что loadLeagueForMatch находит
// чемпионат {id=1, name=«АПЛ», sport_slug=football}.
func setupMatchLeague(leagues *fakeLeagueRepo) {
	leagues.getByIDFn = func(_ context.Context, id int64) (domain.League, error) {
		if id != 1 {
			return domain.League{}, domain.ErrNotFound
		}
		return domain.League{ID: 1, Name: "АПЛ", SportSlug: "football"}, nil
	}
}

// validMatchInput — корректное тело создания матча: лига 1 (АПЛ), один рынок ML
// с тремя исходами и один рынок TOTALS 2.5 с over/under.
func validMatchInput() CreateMatchInput {
	line := decimal.RequireFromString("2.5")
	return CreateMatchInput{
		Title:    "Manchester United — Liverpool",
		LeagueID: 1,
		StartsAt: time.Now().Add(24 * time.Hour),
		Home:     "Manchester United",
		Away:     "Liverpool",
		Markets: []MatchMarketInput{
			{Type: domain.MarketML, Outcomes: []MatchOutcomeInput{
				{Code: domain.OutcomeHome, Label: "П1", Odds: decimal.RequireFromString("2.10")},
				{Code: domain.OutcomeDraw, Label: "X", Odds: decimal.RequireFromString("3.40")},
				{Code: domain.OutcomeAway, Label: "П2", Odds: decimal.RequireFromString("3.20")},
			}},
			{Type: domain.MarketTotals, Line: &line, Outcomes: []MatchOutcomeInput{
				{Code: domain.OutcomeOver, Label: "ТБ 2.5", Odds: decimal.RequireFromString("1.90")},
				{Code: domain.OutcomeUnder, Label: "ТМ 2.5", Odds: decimal.RequireFromString("1.90")},
			}},
		},
	}
}

func TestAdminService_CreateMatch_Success(t *testing.T) {
	svc, _, events, markets, outcomes, leagues, _, _ := newTestAdmin(t)
	setupMatchLeague(leagues)
	input := validMatchInput()

	result, err := svc.CreateMatch(context.Background(), 77, input)
	if err != nil {
		t.Fatalf("CreateMatch: %v", err)
	}

	// Событие: manual, sport_slug из лиги (football), upcoming, ссылка на лигу.
	if len(events.createCalls) != 1 {
		t.Fatalf("createCalls = %d, want 1", len(events.createCalls))
	}
	ev := events.createCalls[0]
	if ev.Source != domain.SourceManual {
		t.Errorf("source = %s, want manual", ev.Source)
	}
	if ev.SportSlug != "football" {
		t.Errorf("sport_slug = %s, want football (из лиги)", ev.SportSlug)
	}
	if ev.Status != domain.EventUpcoming {
		t.Errorf("status = %s, want upcoming", ev.Status)
	}
	if ev.LeagueID == nil || *ev.LeagueID != 1 {
		t.Errorf("league_id = %v, want 1", ev.LeagueID)
	}
	if ev.LeagueName == nil || *ev.LeagueName != "АПЛ" {
		t.Errorf("league_name = %v, want АПЛ (копия из лиги)", ev.LeagueName)
	}
	if ev.Home == nil || *ev.Home != "Manchester United" || ev.Away == nil || *ev.Away != "Liverpool" {
		t.Errorf("home/away = %v/%v", ev.Home, ev.Away)
	}
	if ev.CreatedBy == nil || *ev.CreatedBy != 77 {
		t.Errorf("created_by = %v, want 77", ev.CreatedBy)
	}

	// 2 рынка (ML + TOTALS), 5 исходов (3 + 2).
	if len(markets.createCalls) != 2 {
		t.Fatalf("market createCalls = %d, want 2", len(markets.createCalls))
	}
	if markets.createCalls[0].Type != domain.MarketML {
		t.Errorf("market[0] type = %s, want ML", markets.createCalls[0].Type)
	}
	if markets.createCalls[1].Type != domain.MarketTotals || markets.createCalls[1].Line == nil {
		t.Errorf("market[1] = %+v, want TOTALS with line", markets.createCalls[1])
	}
	if len(outcomes.upsertCalls) != 5 {
		t.Fatalf("outcome upsertCalls = %d, want 5", len(outcomes.upsertCalls))
	}

	// Результат содержит проставленные id события и исходов.
	if result.Event.ID == 0 {
		t.Errorf("result event id not set")
	}
	if len(result.Outcomes) != 5 || result.Outcomes[0].ID == 0 {
		t.Errorf("result outcomes = %+v, want 5 with ids", result.Outcomes)
	}
}

func TestAdminService_CreateMatch_LeagueNotFound(t *testing.T) {
	svc, _, _, _, _, leagues, _, _ := newTestAdmin(t)
	setupMatchLeague(leagues) // лига 1 существует, просим несуществующую 999
	input := validMatchInput()
	input.LeagueID = 999

	_, err := svc.CreateMatch(context.Background(), 1, input)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestAdminService_CreateMatch_NoMLMarket(t *testing.T) {
	svc, _, _, _, _, leagues, _, _ := newTestAdmin(t)
	setupMatchLeague(leagues)
	input := validMatchInput()
	input.Markets = input.Markets[1:] // оставляем только TOTALS — ML нет

	_, err := svc.CreateMatch(context.Background(), 1, input)
	if !errors.Is(err, domain.ErrBetOutOfRange) {
		t.Errorf("err = %v, want ErrBetOutOfRange", err)
	}
}

func TestAdminService_CreateMatch_InvalidOutcomeCode(t *testing.T) {
	svc, _, _, _, _, leagues, _, _ := newTestAdmin(t)
	setupMatchLeague(leagues)
	input := validMatchInput()
	// Код 'over' недопустим для ML-рынка.
	input.Markets[0].Outcomes[0].Code = domain.OutcomeOver

	_, err := svc.CreateMatch(context.Background(), 1, input)
	if !errors.Is(err, domain.ErrBetOutOfRange) {
		t.Errorf("err = %v, want ErrBetOutOfRange", err)
	}
}

func TestAdminService_CreateMatch_TotalsWithoutLine(t *testing.T) {
	svc, _, _, _, _, leagues, _, _ := newTestAdmin(t)
	setupMatchLeague(leagues)
	input := validMatchInput()
	input.Markets[1].Line = nil // TOTALS без линии

	_, err := svc.CreateMatch(context.Background(), 1, input)
	if !errors.Is(err, domain.ErrBetOutOfRange) {
		t.Errorf("err = %v, want ErrBetOutOfRange", err)
	}
}

func TestAdminService_SetMatchStatus_ToLiveSuspendsMarkets(t *testing.T) {
	svc, _, events, markets, _, leagues, _, _ := newTestAdmin(t)
	setupMatchLeague(leagues)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceManual, Status: domain.EventUpcoming}, nil
	}
	markets.byEvent = map[int64][]domain.Market{
		1: {
			{ID: 10, EventID: 1, Type: domain.MarketML, Status: domain.MarketOpen},
			{ID: 11, EventID: 1, Type: domain.MarketTotals, Status: domain.MarketOpen},
		},
	}

	err := svc.SetMatchStatus(context.Background(), 1, MatchStatusInput{Status: domain.EventLive})
	if err != nil {
		t.Fatalf("SetMatchStatus: %v", err)
	}
	// Событие → live.
	if len(events.updateStatusCalls) != 1 || events.updateStatusCalls[0].Status != domain.EventLive {
		t.Errorf("event status updates = %+v, want 1 live", events.updateStatusCalls)
	}
	// Оба рынка → suspended.
	if markets.statusUpdates[10] != domain.MarketSuspended || markets.statusUpdates[11] != domain.MarketSuspended {
		t.Errorf("market statuses = %+v, want both suspended", markets.statusUpdates)
	}
}

func TestAdminService_SetMatchStatus_OnlyLiveAllowed(t *testing.T) {
	svc, _, _, _, _, _, _, _ := newTestAdmin(t)
	// Запрос settled через status — не поддерживается.
	err := svc.SetMatchStatus(context.Background(), 1, MatchStatusInput{Status: domain.EventSettled})
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("err = %v, want ErrMarketClosed", err)
	}
}

func TestAdminService_SetMatchStatus_NotUpcoming(t *testing.T) {
	svc, _, events, _, _, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceManual, Status: domain.EventLive}, nil
	}
	// Уже live — повторный перевод запрещён.
	err := svc.SetMatchStatus(context.Background(), 1, MatchStatusInput{Status: domain.EventLive})
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("err = %v, want ErrMarketClosed", err)
	}
}

// --- SetFeatured (M9) ---

func TestAdminService_SetFeatured_On(t *testing.T) {
	svc, _, events, _, _, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceManual, Status: domain.EventUpcoming}, nil
	}

	if err := svc.SetFeatured(context.Background(), 5, true); err != nil {
		t.Fatalf("SetFeatured: %v", err)
	}
	if len(events.featuredCalls) != 1 {
		t.Fatalf("featuredCalls = %d, want 1", len(events.featuredCalls))
	}
	if events.featuredCalls[0].ID != 5 || !events.featuredCalls[0].Featured {
		t.Errorf("featuredCalls[0] = %+v, want {ID:5 Featured:true}", events.featuredCalls[0])
	}
}

func TestAdminService_SetFeatured_Off(t *testing.T) {
	svc, _, events, _, _, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceCustom, Status: domain.EventUpcoming}, nil
	}

	if err := svc.SetFeatured(context.Background(), 7, false); err != nil {
		t.Fatalf("SetFeatured: %v", err)
	}
	if len(events.featuredCalls) != 1 || events.featuredCalls[0].Featured {
		t.Errorf("featuredCalls = %+v, want one call with Featured=false", events.featuredCalls)
	}
}

func TestAdminService_SetFeatured_NotFound(t *testing.T) {
	svc, _, events, _, _, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, _ int64) (domain.Event, error) {
		return domain.Event{}, domain.ErrNotFound
	}

	err := svc.SetFeatured(context.Background(), 999, true)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if len(events.featuredCalls) != 0 {
		t.Errorf("featuredCalls = %d, want 0 (no SetFeatured after 404)", len(events.featuredCalls))
	}
}

// fakeBetRepoWithPending возвращает фейк BetRepository, отдающий заданные
// pending-ставки — нужен тестам SetMatchScores/CancelMatch, где settlement
// считает выплаты. В newTestAdmin уже стоит пустой fakeBetRepo без listPendingFn.
func fakeBetRepoWithPending(bets []domain.Bet) *fakeBetRepo {
	return &fakeBetRepo{
		listPendingFn: func(context.Context, []int64) ([]domain.Bet, error) {
			return bets, nil
		},
	}
}

func setupMatchForScores(svc *AdminService, events *fakeEventRepo, markets *fakeMarketRepo, outcomes *fakeOutcomeRepo, status domain.EventStatus, hasScores bool) {
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		ev := domain.Event{ID: id, Source: domain.SourceManual, Status: status}
		if hasScores {
			ev.Scores = json.RawMessage(`{"home":2,"away":1}`)
		}
		return ev, nil
	}
	markets.byEvent = map[int64][]domain.Market{
		1: {
			{ID: 10, EventID: 1, Type: domain.MarketML, Status: domain.MarketOpen},
			{ID: 11, EventID: 1, Type: domain.MarketTotals, Line: decPtr("2.5"), Status: domain.MarketOpen},
		},
	}
	markets.getByIDFn = func(_ context.Context, id int64) (domain.Market, error) {
		return domain.Market{ID: id, Status: domain.MarketOpen}, nil
	}
	outcomes.byMarket = map[int64][]domain.Outcome{
		10: {
			{ID: 100, MarketID: 10, Code: domain.OutcomeHome},
			{ID: 101, MarketID: 10, Code: domain.OutcomeDraw},
			{ID: 102, MarketID: 10, Code: domain.OutcomeAway},
		},
		11: {
			{ID: 110, MarketID: 11, Code: domain.OutcomeOver},
			{ID: 111, MarketID: 11, Code: domain.OutcomeUnder},
		},
	}
	outcomes.getByIDFn = func(_ context.Context, id int64) (domain.Outcome, error) {
		return domain.Outcome{ID: id, Odds: decimal.RequireFromString("2.00")}, nil
	}
}

func TestAdminService_SetMatchScores_DelegatesToSettlement(t *testing.T) {
	// Собираем сервис вручную с bet-репозиторием, отдающим pending-ставку.
	tx := &fakeTxRunner{}
	events := &fakeEventRepo{}
	markets := &fakeMarketRepo{}
	outcomes := &fakeOutcomeRepo{}
	leagues := &fakeLeagueRepo{}
	wallets := &fakeWalletRepo{}
	walletTx := &fakeWalletTxRepo{}
	bets := fakeBetRepoWithPending([]domain.Bet{
		{ID: 500, UserID: 9, OutcomeID: 100, Stake: 100, PotentialPayout: 200}, // home — выиграет при 2:1
	})
	settlement := NewSettlementService(tx, events, markets, outcomes, bets, wallets, walletTx, nil)
	svc := NewAdminService(tx, events, markets, outcomes, leagues, wallets, walletTx, settlement, nil)

	setupMatchForScores(svc, events, markets, outcomes, domain.EventUpcoming, false)
	wallets.getForUpdFn = func(_ context.Context, userID int64) (domain.Wallet, error) {
		return domain.Wallet{UserID: userID, Balance: 0}, nil
	}
	wallets.updateBalFn = func(_ context.Context, _ int64, delta int64) (int64, error) { return delta, nil }
	walletTx.createFn = func(_ context.Context, _ domain.WalletTransaction) (int64, error) { return 1, nil }

	err := svc.SetMatchScores(context.Background(), 1, MatchScoresInput{Home: 2, Away: 1})
	if err != nil {
		t.Fatalf("SetMatchScores: %v", err)
	}
	// Событие переведено в settled с сохранённым scores {"home":2,"away":1}.
	if len(events.statusScoresCalls) != 1 || events.statusScoresCalls[0].Status != domain.EventSettled {
		t.Fatalf("status/scores updates = %+v, want 1 settled", events.statusScoresCalls)
	}
	got := string(events.statusScoresCalls[0].Scores)
	if got != `{"home":2,"away":1}` {
		t.Errorf("scores = %s, want {\"home\":2,\"away\":1}", got)
	}
	// ML по счёту 2:1 → победа home (outcome 100 → won), прочие ML → lost.
	if outcomes.resultUpdates[100] != domain.ResultWon {
		t.Errorf("home outcome result = %v, want won", outcomes.resultUpdates[100])
	}
	if outcomes.resultUpdates[101] != domain.ResultLost {
		t.Errorf("draw outcome result = %v, want lost", outcomes.resultUpdates[101])
	}
	// TOTALS 2.5, total=3 > 2.5 → over выигрывает (110), under проигрывает (111).
	if outcomes.resultUpdates[110] != domain.ResultWon {
		t.Errorf("over outcome result = %v, want won", outcomes.resultUpdates[110])
	}
	if outcomes.resultUpdates[111] != domain.ResultLost {
		t.Errorf("under outcome result = %v, want lost", outcomes.resultUpdates[111])
	}
}

func TestAdminService_SetMatchScores_AlreadyHasScores(t *testing.T) {
	svc, _, events, markets, outcomes, _, _, _ := newTestAdmin(t)
	// scores уже введён — повторный ввод запрещён.
	setupMatchForScores(svc, events, markets, outcomes, domain.EventUpcoming, true)

	err := svc.SetMatchScores(context.Background(), 1, MatchScoresInput{Home: 3, Away: 0})
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("err = %v, want ErrMarketClosed", err)
	}
}

func TestAdminService_SetMatchScores_NegativeScore(t *testing.T) {
	svc, _, _, _, _, _, _, _ := newTestAdmin(t)
	err := svc.SetMatchScores(context.Background(), 1, MatchScoresInput{Home: -1, Away: 0})
	if !errors.Is(err, domain.ErrBetOutOfRange) {
		t.Errorf("err = %v, want ErrBetOutOfRange", err)
	}
}

func TestAdminService_SetMatchScores_NotManual(t *testing.T) {
	svc, _, events, _, _, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceCustom, Status: domain.EventUpcoming}, nil
	}
	err := svc.SetMatchScores(context.Background(), 1, MatchScoresInput{Home: 1, Away: 0})
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("err = %v, want ErrMarketClosed for non-manual", err)
	}
}

func TestAdminService_CancelMatch_DelegatesToSettlement(t *testing.T) {
	svc, _, events, markets, outcomes, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceManual, Status: domain.EventUpcoming}, nil
	}
	markets.byEvent = map[int64][]domain.Market{
		1: {{ID: 10, EventID: 1, Type: domain.MarketML, Status: domain.MarketOpen}},
	}
	outcomes.byMarket = map[int64][]domain.Outcome{
		10: {{ID: 100, MarketID: 10, Code: domain.OutcomeHome}},
	}

	err := svc.CancelMatch(context.Background(), 1)
	if err != nil {
		t.Fatalf("CancelMatch: %v", err)
	}
	// Все исходы → void, рынок → void, событие → cancelled.
	if outcomes.resultUpdates[100] != domain.ResultVoid {
		t.Errorf("outcome result = %v, want void", outcomes.resultUpdates[100])
	}
	if markets.statusUpdates[10] != domain.MarketVoid {
		t.Errorf("market status = %v, want void", markets.statusUpdates[10])
	}
	if len(events.statusScoresCalls) != 1 || events.statusScoresCalls[0].Status != domain.EventCancelled {
		t.Errorf("event updates = %+v, want 1 cancelled", events.statusScoresCalls)
	}
}

func TestAdminService_CancelMatch_AlreadySettled(t *testing.T) {
	svc, _, events, _, _, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceManual, Status: domain.EventSettled}, nil
	}
	err := svc.CancelMatch(context.Background(), 1)
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("err = %v, want ErrMarketClosed for settled", err)
	}
}

func TestAdminService_EditMatch_UpdatesFields(t *testing.T) {
	svc, _, events, _, outcomes, leagues, _, _ := newTestAdmin(t)
	setupMatchLeague(leagues)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceManual, Status: domain.EventUpcoming}, nil
	}
	newHome := "Chelsea"
	newStart := time.Now().Add(48 * time.Hour)

	err := svc.EditMatch(context.Background(), 1, EditMatchInput{
		Home:     &newHome,
		LeagueID: int64Ptr(1),
		StartsAt: &newStart,
		Outcomes: []EditOutcomeInput{
			{ID: 100, Odds: decPtr("2.50")},
		},
	})
	if err != nil {
		t.Fatalf("EditMatch: %v", err)
	}
	// Поля матча обновлены, league_id+league_name переданы парой.
	if len(events.updateMatchCalls) != 1 {
		t.Fatalf("updateMatchCalls = %d, want 1", len(events.updateMatchCalls))
	}
	u := events.updateMatchCalls[0]
	if u.Home == nil || *u.Home != newHome {
		t.Errorf("home update = %v, want %q", u.Home, newHome)
	}
	if u.LeagueID == nil || *u.LeagueID != 1 {
		t.Errorf("league_id update = %v, want 1", u.LeagueID)
	}
	if u.LeagueName == nil || *u.LeagueName != "АПЛ" {
		t.Errorf("league_name update = %v, want АПЛ", u.LeagueName)
	}
	// Исход обновлён.
	if outcomes.labelOddsCalls[100].Odds == nil || !outcomes.labelOddsCalls[100].Odds.Equal(decimal.RequireFromString("2.50")) {
		t.Errorf("outcome 100 odds = %v, want 2.50", outcomes.labelOddsCalls[100].Odds)
	}
}

func TestAdminService_EditMatch_Cancel(t *testing.T) {
	svc, _, events, markets, outcomes, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceManual, Status: domain.EventUpcoming}, nil
	}
	markets.byEvent = map[int64][]domain.Market{
		1: {{ID: 10, EventID: 1, Type: domain.MarketML, Status: domain.MarketOpen}},
	}
	outcomes.byMarket = map[int64][]domain.Outcome{
		10: {{ID: 100, MarketID: 10, Code: domain.OutcomeHome}},
	}

	err := svc.EditMatch(context.Background(), 1, EditMatchInput{Cancel: true})
	if err != nil {
		t.Fatalf("EditMatch cancel: %v", err)
	}
	// Отмена через Cancel → исход void, событие cancelled.
	if outcomes.resultUpdates[100] != domain.ResultVoid {
		t.Errorf("outcome result = %v, want void", outcomes.resultUpdates[100])
	}
	if len(events.statusScoresCalls) != 1 || events.statusScoresCalls[0].Status != domain.EventCancelled {
		t.Errorf("event updates = %+v, want 1 cancelled", events.statusScoresCalls)
	}
}

func TestAdminService_EditMatch_NotManual(t *testing.T) {
	svc, _, events, _, _, _, _, _ := newTestAdmin(t)
	events.getFn = func(_ context.Context, id int64) (domain.Event, error) {
		return domain.Event{ID: id, Source: domain.SourceCustom, Status: domain.EventUpcoming}, nil
	}
	err := svc.EditMatch(context.Background(), 1, EditMatchInput{Title: strPtr("x")})
	if !errors.Is(err, domain.ErrMarketClosed) {
		t.Errorf("err = %v, want ErrMarketClosed for non-manual", err)
	}
}

// int64Ptr — хелпер для *int64 (id лиги и т.п.).
func int64Ptr(v int64) *int64 { return &v }
