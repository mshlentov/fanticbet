package repository

import (
	"context"
	"fmt"
	"strings"

	"fanticbet/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StatsRepository — аналитические агрегатные запросы по таблицам bets/users
// (M5: статистика профиля и лидерборд). В отличие от CRUD-репозиториев (Bet,
// User), здесь нет операций изменения — только чтение агрегатов. Поэтому
// вынесен в отдельный интерфейс, а не примешан к BetRepository.
type StatsRepository interface {
	// GetUserStats возвращает агрегированную статистику пользователя по bets.
	// Profit/Staked считаются в SQL (BIGINT), WinRate/ROI достраиваются в Go
	// (деление с защитой от деления на 0) — см. computeRates.
	GetUserStats(ctx context.Context, userID int64) (domain.UserStats, error)
	// GetLeaderboard возвращает страницу топа прогнозистов по фильтру:
	// period задаёт окно по settled_at, metric — сортировку (profit/roi),
	// MinBets — порог числа рассчитанных ставок (HAVING), Page — пагинация.
	GetLeaderboard(ctx context.Context, f domain.LeaderboardFilter) ([]domain.LeaderboardRow, error)
}

type StatsRepositoryImpl struct {
	pool *pgxpool.Pool
}

func NewStatsRepository(pool *pgxpool.Pool) *StatsRepositoryImpl {
	return &StatsRepositoryImpl{pool: pool}
}

// GetUserStats — один агрегатный SELECT по bets. COUNT(*) FILTER (WHERE status)
// считает ставки по статусам одним проходом; SUM(...) FILTER (...) — суммы по
// financially-реализованным ставкам (won+lost). NULL'ы от SUM на пользователе
// без ставок коэрцятся в 0 через COALESCE. Если у пользователя ставок нет —
// возвращаем zero-value UserStats (это не ошибка, просто нулевая статистика).
func (r *StatsRepositoryImpl) GetUserStats(ctx context.Context, userID int64) (domain.UserStats, error) {
	q := QuerierFromCtx(ctx, r.pool)

	// Staked — Σ(stake) по won+lost (база ROI). Profit — Σ(potential_payout по won)
	// минус Σ(stake по won+lost). Void и pending в деньги не входят: void нейтрален
	// (refund=stake), pending — результат неизвестен (см. domain.UserStats).
	const sql = `
		SELECT
			COUNT(*)                                                  AS total_bets,
			COUNT(*) FILTER (WHERE status = $2)                       AS won_bets,
			COUNT(*) FILTER (WHERE status = $3)                       AS lost_bets,
			COUNT(*) FILTER (WHERE status = $4)                       AS void_bets,
			COUNT(*) FILTER (WHERE status = $5)                       AS pending_bets,
			COALESCE(SUM(stake)            FILTER (WHERE status IN ($2, $3)), 0),
			COALESCE(SUM(potential_payout) FILTER (WHERE status = $2), 0)
				- COALESCE(SUM(stake)        FILTER (WHERE status IN ($2, $3)), 0)
		FROM bets
		WHERE user_id = $1`

	var (
		s         domain.UserStats
		totalBets int64
		profitSum int64
	)
	// Profit считаем в SQL целиком (последний столбец): на пользователе без
	// won+lost ставок COALESCE даёт 0 − 0 = 0.
	err := q.QueryRow(ctx, sql,
		userID,
		domain.BetWon, domain.BetLost, domain.BetVoid, domain.BetPending,
	).Scan(
		&totalBets,
		&s.WonBets,
		&s.LostBets,
		&s.VoidBets,
		&s.PendingBets,
		&s.Staked,
		&profitSum,
	)
	if err != nil {
		// pgx не вернёт ErrNoRows для агрегата без GROUP BY — он всегда отдаёт
		// одну строку (с нулями). Поэтому любая ошибка здесь техническая.
		return domain.UserStats{}, fmt.Errorf("StatsRepository.GetUserStats user_id=%d: %w", userID, mapErr(err))
	}

	s.TotalBets = totalBets
	s.Profit = profitSum
	s.WinRate, s.ROI = computeRates(s.WonBets, s.LostBets, s.Staked, s.Profit)
	return s, nil
}

// GetLeaderboard — JOIN users + агрегат по bets с группировкой по пользователю.
// SQL строится динамически по аналогии с EventRepository.ListWithFilters:
//
//   - WHERE settled_at >= <граница периода> — для week/month; для all условия нет.
//     Фильтруем по settled_at (а не created_at), т.к. метрика — результат
//     рассчитанных ставок: pending в лидерборде не участвует.
//   - status IN ('won','lost') — только финансово реализованные ставки: void
//     нейтрален, pending ещё не посчитан.
//   - HAVING COUNT(*) >= MinBets — порог для попадания в топ (открытый вопрос
//     tasks.md:264, задаётся конфигом LEADERBOARD_MIN_BETS).
//   - ORDER BY по метрике: profit DESC (число) либо profit/staked DESC (ROI).
//     Сортировка по выражению, а не по алиасу, т.к. у ROI нет отдельного столбца.
func (r *StatsRepositoryImpl) GetLeaderboard(ctx context.Context, f domain.LeaderboardFilter) ([]domain.LeaderboardRow, error) {
	q := QuerierFromCtx(ctx, r.pool)

	page := f.Page
	if page < 1 {
		page = 1
	}

	// Динамические части запроса. Аргументы нумеруем по мере добавления.
	args := []any{}
	conds := []string{"b.status IN ('won', 'lost')"}

	// Период: граница по settled_at. week → 7 дней назад, month → 30, all — без фильтра.
	if f.Period != domain.PeriodAll {
		days := 7
		if f.Period == domain.PeriodMonth {
			days = 30
		}
		args = append(args, days)
		conds = append(conds, fmt.Sprintf("b.settled_at >= now() - make_interval(days => $%d)", len(args)))
	}

	// Порог числа ставок. HAVING применяется после GROUP BY, поэтому здесь —
	// обычный COUNT(*) без FILTER (WHERE уже отсёк всё, кроме won+lost).
	having := "TRUE"
	if f.MinBets > 0 {
		args = append(args, f.MinBets)
		having = fmt.Sprintf("COUNT(*) >= $%d", len(args))
	}

	// Сортировка по метрике. ROI = profit/staked — отдельного столбца нет,
	// поэтому ORDER BY по выражению. NULLS LAST на случай staked=0 (теоретически
	// невозможен при HAVING > 0, но защита от деления лишней не бывает — Postgres
	// всё равно вычисляет выражение).
	orderExpr := "profit DESC"
	if f.Metric == domain.MetricROI {
		orderExpr = "(CASE WHEN SUM(b.stake) > 0 THEN SUM(b.potential_payout) FILTER (WHERE b.status = 'won') - SUM(b.stake) ELSE NULL END) / NULLIF(SUM(b.stake), 0) DESC NULLS LAST"
	}

	args = append(args, DefaultPageSize)
	limitPos := len(args)
	args = append(args, (page-1)*DefaultPageSize)
	offsetPos := len(args)

	sql := fmt.Sprintf(`
		SELECT
			u.id,
			u.display_name,
			u.avatar_url,
			COUNT(*)                                                  AS total_bets,
			COUNT(*) FILTER (WHERE b.status = 'won')                  AS won_bets,
			COALESCE(SUM(b.stake), 0)                                 AS staked,
			COALESCE(SUM(b.potential_payout) FILTER (WHERE b.status = 'won'), 0)
				- COALESCE(SUM(b.stake), 0)                           AS profit
		FROM bets b
		JOIN users u ON u.id = b.user_id
		WHERE %s
		GROUP BY u.id, u.display_name, u.avatar_url
		HAVING %s
		ORDER BY %s, u.id ASC
		LIMIT $%d OFFSET $%d`,
		strings.Join(conds, " AND "), having, orderExpr, limitPos, offsetPos)

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("StatsRepository.GetLeaderboard period=%s metric=%s: %w", f.Period, f.Metric, mapErr(err))
	}
	defer rows.Close()

	var result []domain.LeaderboardRow
	for rows.Next() {
		var row domain.LeaderboardRow
		if err := rows.Scan(
			&row.UserID, &row.DisplayName, &row.AvatarURL,
			&row.TotalBets, &row.WonBets, &row.Staked, &row.Profit,
		); err != nil {
			return nil, fmt.Errorf("StatsRepository.GetLeaderboard scan: %w", mapErr(err))
		}
		// В строке лидерборда все ставки уже won+lost (void/pending отсечены WHERE
		// на уровне SQL). WinRate сюда не входит (по плану — не информативен в
		// топе), считаем только ROI = profit/staked.
		_, row.ROI = computeRates(row.WonBets, row.TotalBets-row.WonBets, row.Staked, row.Profit)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("StatsRepository.GetLeaderboard rows: %w", mapErr(err))
	}
	return result, nil
}

// computeRates достраивает WinRate и ROI из счётчиков. WinRate = won/(won+lost),
// ROI = profit/staked. При нулевом знаменателе возвращаем 0: пользователь без
// рассчитанных ставок не имеет осмысленной метрики, но в ответе ждём число, не NULL.
func computeRates(won, lost, staked, profit int64) (winRate, roi float64) {
	decided := won + lost
	if decided > 0 {
		winRate = float64(won) / float64(decided)
	}
	if staked > 0 {
		roi = float64(profit) / float64(staked)
	}
	return winRate, roi
}

var _ StatsRepository = (*StatsRepositoryImpl)(nil)
