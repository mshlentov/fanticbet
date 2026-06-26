package repository

import (
	"context"
	"fmt"
	"strings"

	"fanticbet/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// leagueColumns — единый список колонок leagues в порядке сканирования scanLeague.
// Держим в одном месте, чтобы SELECT-ы не разъезжались между методами.
const leagueColumns = `id, name, sport_slug, created_at, updated_at`

// LeagueRepository — интерфейс работы с таблицей leagues (чемпионаты, веха M8).
// Справочник, группирующий события; заводится админом через /admin/leagues.
type LeagueRepository interface {
	// Create вставляет чемпионат и возвращает его id.
	Create(ctx context.Context, l domain.League) (int64, error)
	// GetByID возвращает чемпионат по id (domain.ErrNotFound, если нет).
	GetByID(ctx context.Context, id int64) (domain.League, error)
	// List возвращает чемпионаты, опционально отфильтрованные по sport_slug
	// (пустая строка — без фильтра). Сортировка по sport_slug, name.
	List(ctx context.Context, sportSlug string) ([]domain.League, error)
	// Update правит name и/или sport_slug. nil-поля оставляют текущее значение.
	// Оба nil — сводится к проверке существования (чтобы пустой PATCH не маскировал
	// 404). Возвращает domain.ErrNotFound, если чемпионата нет.
	Update(ctx context.Context, id int64, name *string, sportSlug *string) error
	// Delete удаляет чемпионат. Проверку «нет привязанных событий» делает сервис
	// (через CountEventsByLeague) до вызова Delete. Возвращает domain.ErrNotFound,
	// если чемпионата нет.
	Delete(ctx context.Context, id int64) error
	// CountEventsByLeague возвращает число событий, ссылающихся на чемпионат.
	// Используется сервисом для блокировки удаления занятой лиги (409).
	CountEventsByLeague(ctx context.Context, id int64) (int64, error)
}

type LeagueRepositoryImpl struct {
	pool *pgxpool.Pool
}

func NewLeagueRepository(pool *pgxpool.Pool) *LeagueRepositoryImpl {
	return &LeagueRepositoryImpl{pool: pool}
}

// scanLeague читает одну строку leagues в порядке leagueColumns.
func scanLeague(row pgx.Row) (domain.League, error) {
	var l domain.League
	err := row.Scan(&l.ID, &l.Name, &l.SportSlug, &l.CreatedAt, &l.UpdatedAt)
	return l, err
}

// Create — простой INSERT; name и sport_slug передаёт сервис (после валидации).
// created_at/updated_at проставляются DEFAULT в схеме (now()).
func (r *LeagueRepositoryImpl) Create(ctx context.Context, l domain.League) (int64, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		INSERT INTO leagues (name, sport_slug)
		VALUES ($1, $2)
		RETURNING id`

	var id int64
	err := q.QueryRow(ctx, sql, l.Name, l.SportSlug).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("LeagueRepository.Create name=%q sport_slug=%q: %w",
			l.Name, l.SportSlug, mapErr(err))
	}
	return id, nil
}

func (r *LeagueRepositoryImpl) GetByID(ctx context.Context, id int64) (domain.League, error) {
	q := QuerierFromCtx(ctx, r.pool)

	sql := `SELECT ` + leagueColumns + ` FROM leagues WHERE id = $1`

	l, err := scanLeague(q.QueryRow(ctx, sql, id))
	if err != nil {
		return domain.League{}, fmt.Errorf("LeagueRepository.GetByID id=%d: %w", id, mapErr(err))
	}
	return l, nil
}

// List — фильтр по sport_slug собирается динамически: пустой slug не добавляет
// WHERE и возвращает все чемпионаты. Сортировка детерминированная (sport_slug, name).
func (r *LeagueRepositoryImpl) List(ctx context.Context, sportSlug string) ([]domain.League, error) {
	q := QuerierFromCtx(ctx, r.pool)

	sql := `SELECT ` + leagueColumns + ` FROM leagues`
	args := []any{}
	if sportSlug != "" {
		args = append(args, sportSlug)
		sql += fmt.Sprintf(` WHERE sport_slug = $%d`, len(args))
	}
	sql += ` ORDER BY sport_slug ASC, name ASC, id ASC`

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("LeagueRepository.List: %w", mapErr(err))
	}
	defer rows.Close()

	var result []domain.League
	for rows.Next() {
		l, err := scanLeague(rows)
		if err != nil {
			return nil, fmt.Errorf("LeagueRepository.List scan: %w", mapErr(err))
		}
		result = append(result, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("LeagueRepository.List rows: %w", mapErr(err))
	}
	return result, nil
}

// Update динамически собирает SET по non-nil аргументам (как UpdateDetails).
func (r *LeagueRepositoryImpl) Update(ctx context.Context, id int64, name *string, sportSlug *string) error {
	q := QuerierFromCtx(ctx, r.pool)

	sets := []string{}
	args := []any{id}
	if name != nil {
		args = append(args, *name)
		sets = append(sets, fmt.Sprintf("name = $%d", len(args)))
	}
	if sportSlug != nil {
		args = append(args, *sportSlug)
		sets = append(sets, fmt.Sprintf("sport_slug = $%d", len(args)))
	}
	if len(sets) == 0 {
		// Нечего обновлять — но чемпионат должен существовать, проверим это.
		const checkSQL = `SELECT 1 FROM leagues WHERE id = $1`
		var one int
		if err := q.QueryRow(ctx, checkSQL, id).Scan(&one); err != nil {
			return fmt.Errorf("LeagueRepository.Update id=%d: %w", id, mapErr(err))
		}
		return nil
	}

	sql := fmt.Sprintf(`UPDATE leagues SET %s WHERE id = $1`, strings.Join(sets, ", "))

	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("LeagueRepository.Update id=%d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("LeagueRepository.Update id=%d: %w", id, domain.ErrNotFound)
	}
	return nil
}

func (r *LeagueRepositoryImpl) Delete(ctx context.Context, id int64) error {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `DELETE FROM leagues WHERE id = $1`

	tag, err := q.Exec(ctx, sql, id)
	if err != nil {
		return fmt.Errorf("LeagueRepository.Delete id=%d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("LeagueRepository.Delete id=%d: %w", id, domain.ErrNotFound)
	}
	return nil
}

// CountEventsByLeague считает события, привязанные к чемпионату._fk events.league_id.
func (r *LeagueRepositoryImpl) CountEventsByLeague(ctx context.Context, id int64) (int64, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `SELECT count(*) FROM events WHERE league_id = $1`

	var n int64
	err := q.QueryRow(ctx, sql, id).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("LeagueRepository.CountEventsByLeague id=%d: %w", id, mapErr(err))
	}
	return n, nil
}

var _ LeagueRepository = (*LeagueRepositoryImpl)(nil)
