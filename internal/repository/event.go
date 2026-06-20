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

// eventColumns — единый список колонок events в порядке сканирования scanEvent.
// Держим в одном месте, чтобы SELECT-ы не разъезжались между методами.
const eventColumns = `id, source, external_id, sport_slug, league_name, title,
	home, away, starts_at, status, scores, created_by, created_at, updated_at`

// EventFilter — параметры выборки ленты событий (GET /events). Пустые строковые
// поля означают «без фильтра»; Page начинается с 1.
type EventFilter struct {
	Sport  string             // фильтр по sport_slug; "" — без фильтра
	Status domain.EventStatus // фильтр по статусу; "" — без фильтра
	Query  string             // поиск по title (ILIKE); "" — без фильтра
	Page   int                // страница, с 1
}

// EventRepository — интерфейс работы с таблицей events.
type EventRepository interface {
	// Upsert вставляет или обновляет событие по (source, external_id) и
	// возвращает его id. Предназначен для oddsapi-событий (external_id NOT NULL);
	// существующее событие сохраняет уже накопленный scores, если новый пуст.
	Upsert(ctx context.Context, e domain.Event) (int64, error)
	// GetByID возвращает событие по внутреннему id (domain.ErrNotFound, если нет).
	GetByID(ctx context.Context, id int64) (domain.Event, error)
	// ListWithFilters возвращает страницу ленты событий (старт раньше — первым).
	ListWithFilters(ctx context.Context, f EventFilter) ([]domain.Event, error)
	// ListForOddsSync возвращает upcoming oddsapi-события, стартующие в ближайшие
	// within (например, 48ч). Их выбирает OddsSyncWorker для обновления котировок.
	ListForOddsSync(ctx context.Context, within time.Duration) ([]domain.Event, error)
	// ListForSettlement возвращает oddsapi-события в статусах upcoming/live,
	// которые уже должны были начаться (starts_at < now). Их проверяет
	// SettlementWorker на предмет завершения/отмены.
	ListForSettlement(ctx context.Context) ([]domain.Event, error)
	// UpdateStatusAndScores меняет статус события и сохраняет сырой scores.
	// Возвращает domain.ErrNotFound, если события с таким id нет.
	UpdateStatusAndScores(ctx context.Context, id int64, status domain.EventStatus, scores []byte) error
}

type EventRepositoryImpl struct {
	pool *pgxpool.Pool
}

func NewEventRepository(pool *pgxpool.Pool) *EventRepositoryImpl {
	return &EventRepositoryImpl{pool: pool}
}

// scanEvent читает одну строку events в порядке eventColumns.
func scanEvent(row pgx.Row) (domain.Event, error) {
	var e domain.Event
	err := row.Scan(
		&e.ID, &e.Source, &e.ExternalID, &e.SportSlug, &e.LeagueName, &e.Title,
		&e.Home, &e.Away, &e.StartsAt, &e.Status, &e.Scores, &e.CreatedBy,
		&e.CreatedAt, &e.UpdatedAt,
	)
	return e, err
}

// Upsert — INSERT ... ON CONFLICT (source, external_id). На конфликте обновляем
// данные матча и статус, но scores сохраняем через COALESCE: лента/EventSync
// приходят без счёта, и затирать им уже сохранённый settlement-ом scores нельзя.
// updated_at поддерживает триггер trg_events_updated_at.
func (r *EventRepositoryImpl) Upsert(ctx context.Context, e domain.Event) (int64, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		INSERT INTO events (source, external_id, sport_slug, league_name, title,
		                    home, away, starts_at, status, scores, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (source, external_id) DO UPDATE SET
			sport_slug  = EXCLUDED.sport_slug,
			league_name = EXCLUDED.league_name,
			title       = EXCLUDED.title,
			home        = EXCLUDED.home,
			away        = EXCLUDED.away,
			starts_at   = EXCLUDED.starts_at,
			status      = EXCLUDED.status,
			scores      = COALESCE(EXCLUDED.scores, events.scores)
		RETURNING id`

	var id int64
	err := q.QueryRow(ctx, sql,
		e.Source, e.ExternalID, e.SportSlug, e.LeagueName, e.Title,
		e.Home, e.Away, e.StartsAt, e.Status, e.Scores, e.CreatedBy,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("EventRepository.Upsert source=%s external_id=%v: %w", e.Source, e.ExternalID, mapErr(err))
	}
	return id, nil
}

func (r *EventRepositoryImpl) GetByID(ctx context.Context, id int64) (domain.Event, error) {
	q := QuerierFromCtx(ctx, r.pool)

	sql := `SELECT ` + eventColumns + ` FROM events WHERE id = $1`

	e, err := scanEvent(q.QueryRow(ctx, sql, id))
	if err != nil {
		return domain.Event{}, fmt.Errorf("EventRepository.GetByID id=%d: %w", id, mapErr(err))
	}
	return e, nil
}

func (r *EventRepositoryImpl) ListWithFilters(ctx context.Context, f EventFilter) ([]domain.Event, error) {
	q := QuerierFromCtx(ctx, r.pool)

	// Условия и аргументы собираем динамически: пустые фильтры не добавляют WHERE.
	conds := []string{"TRUE"}
	args := []any{}
	if f.Sport != "" {
		args = append(args, f.Sport)
		conds = append(conds, fmt.Sprintf("sport_slug = $%d", len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	if f.Query != "" {
		args = append(args, "%"+f.Query+"%")
		conds = append(conds, fmt.Sprintf("title ILIKE $%d", len(args)))
	}

	page := f.Page
	if page < 1 {
		page = 1
	}
	args = append(args, DefaultPageSize)
	limitPos := len(args)
	args = append(args, (page-1)*DefaultPageSize)
	offsetPos := len(args)

	sql := fmt.Sprintf(`SELECT %s FROM events WHERE %s
		ORDER BY starts_at ASC, id ASC
		LIMIT $%d OFFSET $%d`,
		eventColumns, strings.Join(conds, " AND "), limitPos, offsetPos)

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("EventRepository.ListWithFilters: %w", mapErr(err))
	}
	defer rows.Close()

	return collectEvents(rows, "EventRepository.ListWithFilters")
}

func (r *EventRepositoryImpl) ListForOddsSync(ctx context.Context, within time.Duration) ([]domain.Event, error) {
	q := QuerierFromCtx(ctx, r.pool)

	sql := `SELECT ` + eventColumns + ` FROM events
		WHERE source = $1
		  AND status = $2
		  AND starts_at > now()
		  AND starts_at <= now() + make_interval(secs => $3)
		ORDER BY starts_at ASC, id ASC`

	rows, err := q.Query(ctx, sql, domain.SourceOddsAPI, domain.EventUpcoming, within.Seconds())
	if err != nil {
		return nil, fmt.Errorf("EventRepository.ListForOddsSync: %w", mapErr(err))
	}
	defer rows.Close()

	return collectEvents(rows, "EventRepository.ListForOddsSync")
}

func (r *EventRepositoryImpl) ListForSettlement(ctx context.Context) ([]domain.Event, error) {
	q := QuerierFromCtx(ctx, r.pool)

	sql := `SELECT ` + eventColumns + ` FROM events
		WHERE source = $1
		  AND status IN ($2, $3)
		  AND starts_at < now()
		ORDER BY starts_at ASC, id ASC`

	rows, err := q.Query(ctx, sql, domain.SourceOddsAPI, domain.EventUpcoming, domain.EventLive)
	if err != nil {
		return nil, fmt.Errorf("EventRepository.ListForSettlement: %w", mapErr(err))
	}
	defer rows.Close()

	return collectEvents(rows, "EventRepository.ListForSettlement")
}

func (r *EventRepositoryImpl) UpdateStatusAndScores(ctx context.Context, id int64, status domain.EventStatus, scores []byte) error {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		UPDATE events
		SET status = $2, scores = $3
		WHERE id = $1`

	tag, err := q.Exec(ctx, sql, id, status, scores)
	if err != nil {
		return fmt.Errorf("EventRepository.UpdateStatusAndScores id=%d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("EventRepository.UpdateStatusAndScores id=%d: %w", id, domain.ErrNotFound)
	}
	return nil
}

// collectEvents сканирует все строки в срез событий с единой обёрткой ошибок.
func collectEvents(rows pgx.Rows, op string) ([]domain.Event, error) {
	var result []domain.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("%s scan: %w", op, mapErr(err))
		}
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s rows: %w", op, mapErr(err))
	}
	return result, nil
}

var _ EventRepository = (*EventRepositoryImpl)(nil)
