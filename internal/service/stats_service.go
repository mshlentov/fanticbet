package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"fanticbet/internal/domain"
	"fanticbet/internal/repository"
)

// PublicProfile — публичный профиль пользователя (GET /users/:id): профиль без
// секретов + агрегированная статистика по ставкам. Склейка данных из users и
// агрегата bets в один ответ для удобства клиента (по аналогии с MeResponse).
type PublicProfile struct {
	User  domain.User
	Stats domain.UserStats
}

// LeaderboardCacheTTL — время жизни закэшированной страницы лидерборда. 60с по
// архитектуре (architecture.md:198): за этот горизонт результат не меняется
// сколько-нибудь значительно, а SQL-агрегат по bets недешёв при росте объёма.
const LeaderboardCacheTTL = 60 * time.Second

// cacheKey — ключ in-memory кэша лидерборда: период × метрика × страница.
// Один и тот же запрос с разными параметрами кэшируется отдельно.
type cacheKey struct {
	period domain.StatsPeriod
	metric domain.LeaderboardMetric
	page   int
}

// cacheEntry — закэшированная страница лидерборда с моментом сохранения.
type cacheEntry struct {
	rows      []domain.LeaderboardRow
	createdAt time.Time
}

// StatsService — публичная социальная часть: профиль пользователя со статистикой,
// публичная история ставок и лидерборд. Только чтение из локальной БД; бизнес-
// правил ставок/кошелька здесь нет. Лидерборд кэшируется in-memory на TTL.
type StatsService struct {
	users   repository.UserRepository
	bets    repository.BetRepository
	stats   repository.StatsRepository
	minBets int // порог числа ставок для попадания в топ (LEADERBOARD_MIN_BETS)

	mu    sync.RWMutex
	cache map[cacheKey]cacheEntry
}

func NewStatsService(
	users repository.UserRepository,
	bets repository.BetRepository,
	stats repository.StatsRepository,
	minBets int,
) *StatsService {
	return &StatsService{
		users:   users,
		bets:    bets,
		stats:   stats,
		minBets: minBets,
		cache:   make(map[cacheKey]cacheEntry),
	}
}

// GetPublicProfile возвращает публичный профиль + статистику. Если пользователя
// нет — domain.ErrNotFound (handler отдаст 404). Пользователь без ставок получит
// zero-value статистику (это валидный случай, не ошибка).
func (s *StatsService) GetPublicProfile(ctx context.Context, userID int64) (PublicProfile, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return PublicProfile{}, fmt.Errorf("StatsService.GetPublicProfile user_id=%d: %w", userID, err)
	}

	stats, err := s.stats.GetUserStats(ctx, userID)
	if err != nil {
		return PublicProfile{}, fmt.Errorf("StatsService.GetPublicProfile stats user_id=%d: %w", userID, err)
	}

	return PublicProfile{User: user, Stats: stats}, nil
}

// ListUserBets возвращает страницу публичной истории ставок пользователя
// (GET /users/:id/bets). Прокси к BetRepository.ListByUser, чтобы handler зависел
// только от StatsService (слои: handler не зовёт repository напрямую).
func (s *StatsService) ListUserBets(ctx context.Context, userID int64, status domain.BetStatus, page int) ([]domain.Bet, error) {
	bets, err := s.bets.ListByUser(ctx, userID, status, page)
	if err != nil {
		return nil, fmt.Errorf("StatsService.ListUserBets user_id=%d status=%s: %w", userID, status, err)
	}
	return bets, nil
}

// GetLeaderboard возвращает страницу топа прогнозистов. Результат кэшируется
// in-memory на LeaderboardCacheTTL: повторный запрос в пределах TTL берётся из
// кэша без обращения к БД. При cache miss — запрос к репозиторию и запись в кэш.
//
// Валидация period/metric лежит на handler (он знает значения из query-параметров);
// сюда приходят уже типизированные значения. MinBets подставляем из конфига.
func (s *StatsService) GetLeaderboard(ctx context.Context, period domain.StatsPeriod, metric domain.LeaderboardMetric, page int) ([]domain.LeaderboardRow, error) {
	key := cacheKey{period: period, metric: metric, page: page}

	// Сначала читаем под RLock — быстрый путь на горячем кэше.
	if entry, ok := s.cacheGet(key); ok {
		return entry, nil
	}

	// Miss — запрос к БД. Минимум ставок проставляем из конфига сервиса.
	filter := domain.LeaderboardFilter{
		Period:  period,
		Metric:  metric,
		MinBets: s.minBets,
		Page:    page,
	}
	rows, err := s.stats.GetLeaderboard(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("StatsService.GetLeaderboard period=%s metric=%s page=%d: %w", period, metric, page, err)
	}

	s.cachePut(key, rows)
	return rows, nil
}

// cacheGet возвращает закэшированную страницу, если она есть и не протухла.
// Блокировка на чтение — конкурентные читатели не мешают друг другу.
func (s *StatsService) cacheGet(key cacheKey) ([]domain.LeaderboardRow, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.cache[key]
	if !ok {
		return nil, false
	}
	if time.Since(entry.createdAt) > LeaderboardCacheTTL {
		return nil, false
	}
	return entry.rows, true
}

// cachePut записывает страницу в кэш с текущим моментом. Запись под Lock:
// конкурирующие miss'ы не повредят map (один перетрёт другого — приемлемо,
// данные идентичны; это дешевле, чем singleflight для MVP).
func (s *StatsService) cachePut(key cacheKey, rows []domain.LeaderboardRow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[key] = cacheEntry{rows: rows, createdAt: time.Now()}
}
