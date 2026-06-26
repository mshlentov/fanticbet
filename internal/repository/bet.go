package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fanticbet/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// betColumns — единый список колонок bets в порядке сканирования scanBet.
// Держим в одном месте, чтобы SELECT-ы не разъезжались между методами
// (по аналогии с eventColumns в event.go).
const betColumns = `id, user_id, outcome_id, event_id, stake, odds,
	potential_payout, status, settled_at, created_at`

// BetRepository — интерфейс работы с таблицей bets (ставки пользователей).
// Методы, меняющие данные (Create, UpdateStatusSettled), должны вызываться
// внутри транзакции там, где это диктуется бизнес-логикой (см. BettingService
// и SettlementService): репозиторий сам транзакций не открывает.
type BetRepository interface {
	// Create вставляет ставку и возвращает её id. Status и Odds caller фиксирует
	// на момент размещения (см. BettingService.PlaceBet) — здесь нет скрытой логики.
	Create(ctx context.Context, b domain.Bet) (int64, error)
	// GetByID возвращает ставку по id (domain.ErrNotFound, если нет).
	GetByID(ctx context.Context, id int64) (domain.Bet, error)
	// ListByUser возвращает страницу ставок пользователя (новые — первые),
	// обогащённую названиями события и исхода (JOIN events/outcomes/markets) —
	// для истории ставок. status == "" означает «без фильтра по статусу»; page
	// начинается с 1.
	ListByUser(ctx context.Context, userID int64, status domain.BetStatus, page int) ([]domain.BetWithDetails, error)
	// ListPendingByOutcomes возвращает все pending-ставки на указанные исходы.
	// Используется SettlementService: по результатам исходов рассчитывает ставки.
	// Пустой список outcomeIDs → nil, nil (запрос не выполняется).
	ListPendingByOutcomes(ctx context.Context, outcomeIDs []int64) ([]domain.Bet, error)
	// UpdateStatusSettled проставляет итоговый статус (won/lost/void) и время
	// расчёта. Возвращает domain.ErrNotFound, если ставки с таким id нет.
	UpdateStatusSettled(ctx context.Context, id int64, status domain.BetStatus, settledAt time.Time) error
}

type BetRepositoryImpl struct {
	pool *pgxpool.Pool
}

func NewBetRepository(pool *pgxpool.Pool) *BetRepositoryImpl {
	return &BetRepositoryImpl{pool: pool}
}

// scanBet читает одну строку bets в порядке betColumns.
func scanBet(row pgx.Row) (domain.Bet, error) {
	var b domain.Bet
	err := row.Scan(
		&b.ID, &b.UserID, &b.OutcomeID, &b.EventID, &b.Stake, &b.Odds,
		&b.PotentialPayout, &b.Status, &b.SettledAt, &b.CreatedAt,
	)
	return b, err
}

func (r *BetRepositoryImpl) Create(ctx context.Context, b domain.Bet) (int64, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		INSERT INTO bets (user_id, outcome_id, event_id, stake, odds,
		                  potential_payout, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`

	var id int64
	err := q.QueryRow(ctx, sql,
		b.UserID, b.OutcomeID, b.EventID, b.Stake, b.Odds,
		b.PotentialPayout, b.Status,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("BetRepository.Create user_id=%d outcome_id=%d: %w", b.UserID, b.OutcomeID, mapErr(err))
	}
	return id, nil
}

func (r *BetRepositoryImpl) GetByID(ctx context.Context, id int64) (domain.Bet, error) {
	q := QuerierFromCtx(ctx, r.pool)

	sql := `SELECT ` + betColumns + ` FROM bets WHERE id = $1`

	b, err := scanBet(q.QueryRow(ctx, sql, id))
	if err != nil {
		return domain.Bet{}, fmt.Errorf("BetRepository.GetByID id=%d: %w", id, mapErr(err))
	}
	return b, nil
}

// ListByUser — история ставок пользователя с опциональным фильтром по статусу.
// JOIN к events/outcomes/markets обогащает каждую ставку названием события,
// командами, названием исхода и типом рынка (для отображения в истории вместо
// «Событие #id»). Сортировка по давности (created_at DESC, id DESC) совпадает с
// индексом idx_bets_user. Пустой status не добавляет фильтр по статусу.
func (r *BetRepositoryImpl) ListByUser(ctx context.Context, userID int64, status domain.BetStatus, page int) ([]domain.BetWithDetails, error) {
	q := QuerierFromCtx(ctx, r.pool)

	if page < 1 {
		page = 1
	}

	// Условия и аргументы собираем динамически: пустой status не добавляет фильтр
	// (по аналогии с EventRepository.ListWithFilters). Колонки квалифицированы
	// префиксом b., т.к. в выборке участвуют несколько таблиц.
	conds := []string{"b.user_id = $1"}
	args := []any{userID}
	if status != "" {
		args = append(args, status)
		conds = append(conds, fmt.Sprintf("b.status = $%d", len(args)))
	}
	args = append(args, DefaultPageSize)
	limitPos := len(args)
	args = append(args, (page-1)*DefaultPageSize)
	offsetPos := len(args)

	sql := fmt.Sprintf(`
		SELECT b.id, b.user_id, b.outcome_id, b.event_id, b.stake, b.odds,
		       b.potential_payout, b.status, b.settled_at, b.created_at,
		       e.title, e.home, e.away, o.label, m.type
		FROM bets b
		JOIN events e   ON e.id = b.event_id
		JOIN outcomes o ON o.id = b.outcome_id
		JOIN markets m  ON m.id = o.market_id
		WHERE %s
		ORDER BY b.created_at DESC, b.id DESC
		LIMIT $%d OFFSET $%d`,
		strings.Join(conds, " AND "), limitPos, offsetPos)

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("BetRepository.ListByUser user_id=%d status=%s: %w", userID, status, mapErr(err))
	}
	defer rows.Close()

	var result []domain.BetWithDetails
	for rows.Next() {
		var bd domain.BetWithDetails
		if err := rows.Scan(
			&bd.ID, &bd.UserID, &bd.OutcomeID, &bd.EventID, &bd.Stake, &bd.Odds,
			&bd.PotentialPayout, &bd.Status, &bd.SettledAt, &bd.CreatedAt,
			&bd.EventTitle, &bd.EventHome, &bd.EventAway, &bd.OutcomeLabel, &bd.MarketType,
		); err != nil {
			return nil, fmt.Errorf("BetRepository.ListByUser scan: %w", mapErr(err))
		}
		result = append(result, bd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("BetRepository.ListByUser rows: %w", mapErr(err))
	}
	return result, nil
}

// ListPendingByOutcomes — выборка pending-ставок для расчёта события.
// Покрывает частичный индекс idx_bets_event_pending (WHERE status = 'pending').
func (r *BetRepositoryImpl) ListPendingByOutcomes(ctx context.Context, outcomeIDs []int64) ([]domain.Bet, error) {
	if len(outcomeIDs) == 0 {
		return nil, nil
	}
	q := QuerierFromCtx(ctx, r.pool)

	sql := `SELECT ` + betColumns + ` FROM bets
		WHERE status = 'pending' AND outcome_id = ANY($1)
		ORDER BY id ASC`

	rows, err := q.Query(ctx, sql, outcomeIDs)
	if err != nil {
		return nil, fmt.Errorf("BetRepository.ListPendingByOutcomes: %w", mapErr(err))
	}
	defer rows.Close()

	var result []domain.Bet
	for rows.Next() {
		b, err := scanBet(rows)
		if err != nil {
			return nil, fmt.Errorf("BetRepository.ListPendingByOutcomes scan: %w", mapErr(err))
		}
		result = append(result, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("BetRepository.ListPendingByOutcomes rows: %w", mapErr(err))
	}
	return result, nil
}

// UpdateStatusSettled проставляет результат расчёта. settledAt — момент расчёта
// (now на стороне сервиса). Идемпотентность на уровне выборки (только pending),
// а не здесь: повторный вызов с тем же id просто перепишет статус.
func (r *BetRepositoryImpl) UpdateStatusSettled(ctx context.Context, id int64, status domain.BetStatus, settledAt time.Time) error {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `UPDATE bets SET status = $2, settled_at = $3 WHERE id = $1`

	tag, err := q.Exec(ctx, sql, id, status, settledAt)
	if err != nil {
		return fmt.Errorf("BetRepository.UpdateStatusSettled id=%d status=%s: %w", id, status, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("BetRepository.UpdateStatusSettled id=%d: %w", id, domain.ErrNotFound)
	}
	return nil
}

var _ BetRepository = (*BetRepositoryImpl)(nil)
