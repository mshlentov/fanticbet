package domain

// Периоды лидерборда. Определяют окно выборки рассчитанных ставок:
// week — последние 7 дней, month — последние 30 дней, all — за всё время.
// Значения совпадают с query-параметром period ручки GET /leaderboard.
type StatsPeriod string

const (
	PeriodWeek  StatsPeriod = "week"
	PeriodMonth StatsPeriod = "month"
	PeriodAll   StatsPeriod = "all"
)

// Метрики сортировки лидерборда. profit — абсолютная прибыль (Σpayout−Σstake
// по won+lost), roi — возврат на вложенный фантик (profit / Σstake по won+lost).
// Значения совпадают с query-параметром metric ручки GET /leaderboard.
type LeaderboardMetric string

const (
	MetricProfit LeaderboardMetric = "profit"
	MetricROI    LeaderboardMetric = "roi"
)

// UserStats — агрегированная статистика пользователя по ставкам. Считается
// SQL-агрегатом по таблице bets (см. repository.StatsRepository.GetUserStats):
//
//   - TotalBets   — всего ставок пользователя;
//   - WonBets     — рассчитанных выигрышем (status='won');
//   - LostBets    — рассчитанных проигрышем (status='lost');
//   - VoidBets    — возвращённых (status='void', нейтральны по деньгам);
//   - PendingBets — ожидающих расчёта (status='pending', результат неизвестен);
//   - Staked      — сумма ставок по won+lost (финансово реализованные, без void/pending);
//   - Profit      — Σ(potential_payout WHERE won) − Σ(stake WHERE won+lost);
//   - WinRate     — WonBets / (WonBets + LostBets), 0 при отсутствии рассчитанных;
//   - ROI         — Profit / Σ(stake WHERE won+lost), 0 при отсутствии рассчитанных.
//
// Формулы следуют поведению settlement: при won выплачивается potential_payout
// (включая stake), при lost — ничего, при void — возврат stake. Поэтому void
// нейтрален (вклад 0) и в profit/ROI/WinRate не учитывается; pending исключён,
// т.к. результат неизвестен.
type UserStats struct {
	TotalBets   int64
	WonBets     int64
	LostBets    int64
	VoidBets    int64
	PendingBets int64
	Staked      int64 // Σ(stake) по won+lost — база для ROI
	Profit      int64
	WinRate     float64
	ROI         float64
}

// LeaderboardRow — строка таблицы лидеров. JOIN users + агрегат по bets: без
// личных данных (email и др.), только публичные поля профиля + метрики.
// Совпадает по набору полей с UserStats, но без void/pending/winrate (в топе
// они не информативны) и с публичным именем/аватаром.
type LeaderboardRow struct {
	UserID      int64
	DisplayName string
	AvatarURL   *string
	TotalBets   int64
	WonBets     int64
	Staked      int64
	Profit      int64
	ROI         float64
}

// LeaderboardFilter — параметры выборки лидерборда (GET /leaderboard).
// Period и Metric — обязательные семантически (handler валидирует значения),
// MinBets — порог числа рассчитанных ставок для попадания в топ (HAVING в SQL),
// Page — номер страницы с 1 (пагинация через LIMIT/OFFSET).
type LeaderboardFilter struct {
	Period  StatsPeriod
	Metric  LeaderboardMetric
	MinBets int
	Page    int
}
