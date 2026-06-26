package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"fanticbet/internal/domain"
)

// newTestStats собирает StatsService с инъектированными фейками и возвращает
// их для настройки в конкретном сценарии. По образцу newTestBetting/newTestAuth.
func newTestStats(t *testing.T, minBets int) (*StatsService, *fakeUserRepo, *fakeBetRepo, *fakeStatsRepo) {
	t.Helper()

	users := &fakeUserRepo{
		getByIDFn: func(ctx context.Context, id int64) (domain.User, error) {
			return domain.User{}, domain.ErrNotFound
		},
	}
	bets := &fakeBetRepo{
		listByUserFn: func(ctx context.Context, userID int64, status domain.BetStatus, page int) ([]domain.BetWithDetails, error) {
			return nil, nil
		},
	}
	stats := &fakeStatsRepo{
		getUserStatsFn: func(ctx context.Context, userID int64) (domain.UserStats, error) {
			return domain.UserStats{}, nil
		},
		getLeaderboardFn: func(ctx context.Context, f domain.LeaderboardFilter) ([]domain.LeaderboardRow, error) {
			return nil, nil
		},
	}

	svc := NewStatsService(users, bets, stats, minBets)
	return svc, users, bets, stats
}

// TestStatsService_GetPublicProfile_Success — профиль + статистика в одном ответе.
// Проверяем, что оба репозитория зовутся с корректным user_id и данные склеиваются.
func TestStatsService_GetPublicProfile_Success(t *testing.T) {
	svc, users, _, stats := newTestStats(t, 10)

	const userID = int64(42)
	users.getByIDFn = func(ctx context.Context, id int64) (domain.User, error) {
		if id != userID {
			t.Fatalf("GetByID called with id=%d, want %d", id, userID)
		}
		return domain.User{ID: id, DisplayName: "alice"}, nil
	}
	stats.getUserStatsFn = func(ctx context.Context, id int64) (domain.UserStats, error) {
		if id != userID {
			t.Fatalf("GetUserStats called with id=%d, want %d", id, userID)
		}
		return domain.UserStats{TotalBets: 5, WonBets: 3, Profit: 1000}, nil
	}

	profile, err := svc.GetPublicProfile(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetPublicProfile: unexpected error: %v", err)
	}
	if profile.User.ID != userID {
		t.Errorf("profile.User.ID = %d, want %d", profile.User.ID, userID)
	}
	if profile.User.DisplayName != "alice" {
		t.Errorf("profile.User.DisplayName = %q, want %q", profile.User.DisplayName, "alice")
	}
	if profile.Stats.TotalBets != 5 || profile.Stats.WonBets != 3 || profile.Stats.Profit != 1000 {
		t.Errorf("profile.Stats = %+v, want {TotalBets:5 WonBets:3 Profit:1000}", profile.Stats)
	}
	if stats.getUserStatsCalls != 1 {
		t.Errorf("GetUserStats calls = %d, want 1", stats.getUserStatsCalls)
	}
}

// TestStatsService_GetPublicProfile_UserNotFound — нет пользователя → ErrNotFound.
// Важно: к статистике обращение не должно идти (профиль не найден, дальше смысла нет).
func TestStatsService_GetPublicProfile_UserNotFound(t *testing.T) {
	svc, _, _, stats := newTestStats(t, 10)

	_, err := svc.GetPublicProfile(context.Background(), 999)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetPublicProfile: error = %v, want ErrNotFound", err)
	}
	if stats.getUserStatsCalls != 0 {
		t.Errorf("GetUserStats should not be called on missing user, calls = %d", stats.getUserStatsCalls)
	}
}

// TestStatsService_ListUserBets_DelegatesToRepo — прокси к BetRepository.ListByUser:
// аргументы пробрасываются как есть, результат возвращается без изменений.
func TestStatsService_ListUserBets_DelegatesToRepo(t *testing.T) {
	svc, _, bets, _ := newTestStats(t, 10)

	const userID = int64(7)
	called := false
	bets.listByUserFn = func(ctx context.Context, id int64, status domain.BetStatus, page int) ([]domain.BetWithDetails, error) {
		called = true
		if id != userID {
			t.Errorf("ListByUser id = %d, want %d", id, userID)
		}
		if status != domain.BetWon {
			t.Errorf("ListByUser status = %q, want %q", status, domain.BetWon)
		}
		if page != 2 {
			t.Errorf("ListByUser page = %d, want 2", page)
		}
		return []domain.BetWithDetails{{Bet: domain.Bet{ID: 1, UserID: id}}}, nil
	}

	result, err := svc.ListUserBets(context.Background(), userID, domain.BetWon, 2)
	if err != nil {
		t.Fatalf("ListUserBets: unexpected error: %v", err)
	}
	if !called {
		t.Fatal("BetRepository.ListByUser was not called")
	}
	if len(result) != 1 || result[0].ID != 1 {
		t.Errorf("ListUserBets result = %+v, want [{ID:1}]", result)
	}
}

// TestStatsService_GetLeaderboard_CacheMiss — первый запрос идёт в репозиторий.
func TestStatsService_GetLeaderboard_CacheMiss(t *testing.T) {
	svc, _, _, stats := newTestStats(t, 10)

	stats.getLeaderboardFn = func(ctx context.Context, f domain.LeaderboardFilter) ([]domain.LeaderboardRow, error) {
		if f.Period != domain.PeriodWeek {
			t.Errorf("filter.Period = %q, want %q", f.Period, domain.PeriodWeek)
		}
		if f.Metric != domain.MetricProfit {
			t.Errorf("filter.Metric = %q, want %q", f.Metric, domain.MetricProfit)
		}
		if f.MinBets != 10 {
			t.Errorf("filter.MinBets = %d, want 10", f.MinBets)
		}
		if f.Page != 1 {
			t.Errorf("filter.Page = %d, want 1", f.Page)
		}
		return []domain.LeaderboardRow{{UserID: 1, Profit: 500}}, nil
	}

	rows, err := svc.GetLeaderboard(context.Background(), domain.PeriodWeek, domain.MetricProfit, 1)
	if err != nil {
		t.Fatalf("GetLeaderboard: unexpected error: %v", err)
	}
	if len(rows) != 1 || rows[0].UserID != 1 {
		t.Errorf("rows = %+v, want [{UserID:1}]", rows)
	}
	if stats.getLeaderboardCalls != 1 {
		t.Errorf("GetLeaderboard repo calls = %d, want 1", stats.getLeaderboardCalls)
	}
}

// TestStatsService_GetLeaderboard_CacheHit — повторный запрос с теми же
// period+metric+page берётся из кэша, репозиторий не дёргается.
func TestStatsService_GetLeaderboard_CacheHit(t *testing.T) {
	svc, _, _, stats := newTestStats(t, 10)

	stats.getLeaderboardFn = func(ctx context.Context, f domain.LeaderboardFilter) ([]domain.LeaderboardRow, error) {
		return []domain.LeaderboardRow{{UserID: 1}, {UserID: 2}}, nil
	}

	// Первый вызов — miss, обращение к репозиторию.
	if _, err := svc.GetLeaderboard(context.Background(), domain.PeriodMonth, domain.MetricROI, 1); err != nil {
		t.Fatalf("first GetLeaderboard: %v", err)
	}
	// Второй вызов с теми же параметрами — hit, репозиторий не зовётся.
	rows, err := svc.GetLeaderboard(context.Background(), domain.PeriodMonth, domain.MetricROI, 1)
	if err != nil {
		t.Fatalf("second GetLeaderboard: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("rows len = %d, want 2 (из кэша)", len(rows))
	}
	if stats.getLeaderboardCalls != 1 {
		t.Errorf("GetLeaderboard repo calls = %d, want 1 (второй запрос — из кэша)", stats.getLeaderboardCalls)
	}
}

// TestStatsService_GetLeaderboard_DifferentParamsSeparateCache — ключ кэша
// учитывает все три параметра: разные period/metric/page дают разные запросы.
func TestStatsService_GetLeaderboard_DifferentParamsSeparateCache(t *testing.T) {
	svc, _, _, stats := newTestStats(t, 5)

	stats.getLeaderboardFn = func(ctx context.Context, f domain.LeaderboardFilter) ([]domain.LeaderboardRow, error) {
		return []domain.LeaderboardRow{{UserID: int64(f.Page)}}, nil
	}

	// Три разных запроса — три обращения к репозиторию.
	_, _ = svc.GetLeaderboard(context.Background(), domain.PeriodAll, domain.MetricProfit, 1)
	_, _ = svc.GetLeaderboard(context.Background(), domain.PeriodAll, domain.MetricROI, 1)    // другая metric
	_, _ = svc.GetLeaderboard(context.Background(), domain.PeriodAll, domain.MetricProfit, 2) // другая page

	if stats.getLeaderboardCalls != 3 {
		t.Errorf("GetLeaderboard repo calls = %d, want 3 (разные ключи кэша)", stats.getLeaderboardCalls)
	}
}

// TestStatsService_GetLeaderboard_CacheExpiry — после истечения TTL запись
// протухает и следующий запрос снова идёт в репозиторий. Эмулируем протухание,
// записав в кэш старую метку времени напрямую (через внутренний метод cachePut
// с пред-состаренным значением — для этого используем helper, не трогая часы).
func TestStatsService_GetLeaderboard_CacheExpiry(t *testing.T) {
	svc, _, _, stats := newTestStats(t, 10)

	stats.getLeaderboardFn = func(ctx context.Context, f domain.LeaderboardFilter) ([]domain.LeaderboardRow, error) {
		return []domain.LeaderboardRow{{UserID: 1}}, nil
	}

	// Первый запрос — заполняем кэш.
	_, _ = svc.GetLeaderboard(context.Background(), domain.PeriodWeek, domain.MetricProfit, 1)
	if stats.getLeaderboardCalls != 1 {
		t.Fatalf("after first call: repo calls = %d, want 1", stats.getLeaderboardCalls)
	}

	// Состариваем запись: кладём в кэш то же значение, но с меткой времени,
	// гарантированно старше TTL. cacheGet при чтении увидит протухание → miss.
	svc.mu.Lock()
	key := cacheKey{period: domain.PeriodWeek, metric: domain.MetricProfit, page: 1}
	svc.cache[key] = cacheEntry{
		rows:      []domain.LeaderboardRow{{UserID: 1}},
		createdAt: time.Now().Add(-LeaderboardCacheTTL - time.Second),
	}
	svc.mu.Unlock()

	// Повторный запрос — кэш протух, снова идём в репозиторий.
	_, err := svc.GetLeaderboard(context.Background(), domain.PeriodWeek, domain.MetricProfit, 1)
	if err != nil {
		t.Fatalf("GetLeaderboard after expiry: %v", err)
	}
	if stats.getLeaderboardCalls != 2 {
		t.Errorf("after expired call: repo calls = %d, want 2 (запись протухла)", stats.getLeaderboardCalls)
	}
}

// TestStatsService_GetLeaderboard_PassesMinBets — порог числа ставок из конфига
// попадает в LeaderboardFilter, который уходит в репозиторий.
func TestStatsService_GetLeaderboard_PassesMinBets(t *testing.T) {
	svc, _, _, stats := newTestStats(t, 42)

	stats.getLeaderboardFn = func(ctx context.Context, f domain.LeaderboardFilter) ([]domain.LeaderboardRow, error) {
		if f.MinBets != 42 {
			t.Errorf("filter.MinBets = %d, want 42", f.MinBets)
		}
		return nil, nil
	}

	_, _ = svc.GetLeaderboard(context.Background(), domain.PeriodAll, domain.MetricProfit, 1)
}

// TestStatsService_GetLeaderboard_RepoError — ошибка репозитория пробрасывается
// наверх без маскировки и не записывается в кэш (чтобы не закэшировать сбой).
func TestStatsService_GetLeaderboard_RepoError(t *testing.T) {
	svc, _, _, stats := newTestStats(t, 10)

	boom := errors.New("db down")
	stats.getLeaderboardFn = func(ctx context.Context, f domain.LeaderboardFilter) ([]domain.LeaderboardRow, error) {
		return nil, boom
	}

	if _, err := svc.GetLeaderboard(context.Background(), domain.PeriodAll, domain.MetricProfit, 1); err == nil {
		t.Fatal("GetLeaderboard: expected error, got nil")
	}
	// Кэш должен быть пуст — ошибку не кэшируем.
	svc.mu.RLock()
	_, cached := svc.cache[cacheKey{period: domain.PeriodAll, metric: domain.MetricProfit, page: 1}]
	svc.mu.RUnlock()
	if cached {
		t.Error("ошибка не должна кэшироваться, но попала в кэш")
	}
}
