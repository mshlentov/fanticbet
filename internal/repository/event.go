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
const eventColumns = `id, source, external_id, sport_slug, league_id, league_name,
	title, home, away, starts_at, status, scores, created_by, created_at, updated_at`

// EventFilter — параметры выборки ленты событий (GET /events). Пустые строковые
// поля и nil-указатели означают «без фильтра»; Page начинается с 1.
type EventFilter struct {
	Sport    string             // фильтр по sport_slug; "" — без фильтра
	Status   domain.EventStatus // фильтр по статусу; "" — без фильтра
	LeagueID *int64             // фильтр по чемпионату; nil — без фильтра
	Query    string             // поиск по title (ILIKE); "" — без фильтра
	Page     int                // страница, с 1
}

// EventRepository — интерфейс работы с таблицей events.
type EventRepository interface {
	// Upsert вставляет или обновляет событие по (source, external_id) и
	// возвращает его id. Предназначен для oddsapi-событий (external_id NOT NULL);
	// существующее событие сохраняет уже накопленный scores, если новый пуст.
	Upsert(ctx context.Context, e domain.Event) (int64, error)
	// Create вставляет новое событие без проверки на конфликт и возвращает его id.
	// Предназначен для manual/custom событий (external_id = NULL): для них нет
	// естественного внешнего ключа уникальности, как у oddsapi-событий.
	Create(ctx context.Context, e domain.Event) (int64, error)
	// UpdateDetails правит title и/или starts_at события. nil-поля оставляют
	// текущее значение. Возвращает domain.ErrNotFound, если события нет. Проверку
	// источника/статуса делает сервис, а не репозиторий.
	UpdateDetails(ctx context.Context, id int64, title *string, startsAt *time.Time) error
	// UpdateMatch правит поля ручного матча: home/away/league_id+league_name и/или
	// starts_at. nil-поля оставляют текущее значение. leagueName можно передать
	// только вместе с leagueID (это его денормализованная копия). Возвращает
	// domain.ErrNotFound, если события нет.
	UpdateMatch(ctx context.Context, id int64, home, away *string, leagueID *int64, leagueName *string, startsAt *time.Time) error
	// UpdateStatus меняет только статус события (upcoming→live и т.п.), не трогая
	// scores. Возвращает domain.ErrNotFound, если события с таким id нет.
	UpdateStatus(ctx context.Context, id int64, status domain.EventStatus) error
	// GetByID возвращает событие по внутреннему id (domain.ErrNotFound, если нет).
	GetByID(ctx context.Context, id int64) (domain.Event, error)
	// ListWithFilters возвращает страницу ленты событий (старт раньше — первым).
	ListWithFilters(ctx context.Context, f EventFilter) ([]domain.Event, error)
	// ListSports возвращает отсортированный список уникальных sport_slug, по
	// которым есть события в БД. Для ленты GET /sports.
	ListSports(ctx context.Context) ([]string, error)
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
		&e.ID, &e.Source, &e.ExternalID, &e.SportSlug, &e.LeagueID, &e.LeagueName,
		&e.Title, &e.Home, &e.Away, &e.StartsAt, &e.Status, &e.Scores,
		&e.CreatedBy, &e.CreatedAt, &e.UpdatedAt,
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
		INSERT INTO events (source, external_id, sport_slug, league_id, league_name,
		                    title, home, away, starts_at, status, scores, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (source, external_id) DO UPDATE SET
			sport_slug  = EXCLUDED.sport_slug,
			league_id   = EXCLUDED.league_id,
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
		e.Source, e.ExternalID, e.SportSlug, e.LeagueID, e.LeagueName, e.Title,
		e.Home, e.Away, e.StartsAt, e.Status, e.Scores, e.CreatedBy,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("EventRepository.Upsert source=%s external_id=%v: %w", e.Source, e.ExternalID, mapErr(err))
	}
	return id, nil
}

// Create — простой INSERT без ON CONFLICT, для manual/custom событий. Статус по
// умолчанию upcoming (DEFAULT в схеме), но сервис передаёт его явно.
func (r *EventRepositoryImpl) Create(ctx context.Context, e domain.Event) (int64, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		INSERT INTO events (source, external_id, sport_slug, league_id, league_name,
		                    title, home, away, starts_at, status, scores, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id`

	var id int64
	err := q.QueryRow(ctx, sql,
		e.Source, e.ExternalID, e.SportSlug, e.LeagueID, e.LeagueName, e.Title,
		e.Home, e.Away, e.StartsAt, e.Status, e.Scores, e.CreatedBy,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("EventRepository.Create source=%s: %w", e.Source, mapErr(err))
	}
	return id, nil
}

// UpdateDetails динамически собирает SET по non-nil аргументам: nil-поля не
// добавляются в UPDATE, оставляя текущее значение. Удобно для PATCH, где клиент
// присылает только изменяемые поля.
func (r *EventRepositoryImpl) UpdateDetails(ctx context.Context, id int64, title *string, startsAt *time.Time) error {
	q := QuerierFromCtx(ctx, r.pool)

	sets := []string{}
	args := []any{id}
	if title != nil {
		args = append(args, *title)
		sets = append(sets, fmt.Sprintf("title = $%d", len(args)))
	}
	if startsAt != nil {
		args = append(args, *startsAt)
		sets = append(sets, fmt.Sprintf("starts_at = $%d", len(args)))
	}
	if len(sets) == 0 {
		// Нечего обновлять — но событие должно существовать, проверим это.
		const checkSQL = `SELECT 1 FROM events WHERE id = $1`
		var one int
		if err := q.QueryRow(ctx, checkSQL, id).Scan(&one); err != nil {
			return fmt.Errorf("EventRepository.UpdateDetails id=%d: %w", id, mapErr(err))
		}
		return nil
	}

	sql := fmt.Sprintf(`UPDATE events SET %s WHERE id = $1`, strings.Join(sets, ", "))

	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("EventRepository.UpdateDetails id=%d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("EventRepository.UpdateDetails id=%d: %w", id, domain.ErrNotFound)
	}
	return nil
}

// UpdateMatch динамически собирает SET по non-nil аргументам: nil-поля не
// добавляются в UPDATE, оставляя текущее значение. leagueName намеренно нельзя
// задать без leagueID — это его денормализованная копия, сервис грузит её из
// leagues.GetByID и передаёт пару вместе. Если ни одно поле не задано — сводится
// к проверке существования события (чтобы пустой PATCH не маскировал 404).
func (r *EventRepositoryImpl) UpdateMatch(ctx context.Context, id int64, home, away *string, leagueID *int64, leagueName *string, startsAt *time.Time) error {
	q := QuerierFromCtx(ctx, r.pool)

	sets := []string{}
	args := []any{id}
	if home != nil {
		args = append(args, *home)
		sets = append(sets, fmt.Sprintf("home = $%d", len(args)))
	}
	if away != nil {
		args = append(args, *away)
		sets = append(sets, fmt.Sprintf("away = $%d", len(args)))
	}
	if leagueID != nil {
		args = append(args, *leagueID)
		sets = append(sets, fmt.Sprintf("league_id = $%d", len(args)))
		// leagueName — всегда рядом с leagueID; nil → NULL в БД.
		if leagueName != nil {
			args = append(args, *leagueName)
		} else {
			args = append(args, nil)
		}
		sets = append(sets, fmt.Sprintf("league_name = $%d", len(args)))
	}
	if startsAt != nil {
		args = append(args, *startsAt)
		sets = append(sets, fmt.Sprintf("starts_at = $%d", len(args)))
	}
	if len(sets) == 0 {
		// Нечего обновлять — но событие должно существовать, проверим это.
		const checkSQL = `SELECT 1 FROM events WHERE id = $1`
		var one int
		if err := q.QueryRow(ctx, checkSQL, id).Scan(&one); err != nil {
			return fmt.Errorf("EventRepository.UpdateMatch id=%d: %w", id, mapErr(err))
		}
		return nil
	}

	sql := fmt.Sprintf(`UPDATE events SET %s WHERE id = $1`, strings.Join(sets, ", "))

	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("EventRepository.UpdateMatch id=%d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("EventRepository.UpdateMatch id=%d: %w", id, domain.ErrNotFound)
	}
	return nil
}

// UpdateStatus меняет только статус события, не трогая scores. Используется
// AdminService.SetMatchStatus для ручного перевода upcoming→live.
func (r *EventRepositoryImpl) UpdateStatus(ctx context.Context, id int64, status domain.EventStatus) error {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `UPDATE events SET status = $2 WHERE id = $1`

	tag, err := q.Exec(ctx, sql, id, status)
	if err != nil {
		return fmt.Errorf("EventRepository.UpdateStatus id=%d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("EventRepository.UpdateStatus id=%d: %w", id, domain.ErrNotFound)
	}
	return nil
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
	if f.LeagueID != nil {
		args = append(args, *f.LeagueID)
		conds = append(conds, fmt.Sprintf("league_id = $%d", len(args)))
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

func (r *EventRepositoryImpl) ListSports(ctx context.Context) ([]string, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `SELECT DISTINCT sport_slug FROM events ORDER BY sport_slug ASC`

	rows, err := q.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("EventRepository.ListSports: %w", mapErr(err))
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, fmt.Errorf("EventRepository.ListSports scan: %w", mapErr(err))
		}
		result = append(result, slug)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("EventRepository.ListSports rows: %w", mapErr(err))
	}
	return result, nil
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
