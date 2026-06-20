package repository

import (
	"context"
	"fmt"

	"fanticbet/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// MarketRepository — интерфейс работы с таблицей markets (рынки события).
type MarketRepository interface {
	// CreateForEvent вставляет рынок события и возвращает его id. Рынки создаёт
	// EventSyncWorker для новых событий (ML/TOTALS) или админ (CUSTOM); далее
	// меняется только статус и наполнение исходов.
	CreateForEvent(ctx context.Context, m domain.Market) (int64, error)
	// GetByID возвращает рынок по id (domain.ErrNotFound, если нет). Используется
	// BettingService.PlaceBet: по market outcome'а проверяем, что рынок открыт.
	GetByID(ctx context.Context, id int64) (domain.Market, error)
	// GetByEvent возвращает все рынки события (без сортировки по бизнес-смыслу,
	// порядок по id).
	GetByEvent(ctx context.Context, eventID int64) ([]domain.Market, error)
	// GetByEvents возвращает рынки сразу нескольких событий одним запросом
	// (для ленты GET /events — чтобы не делать N+1). Порядок по event_id, id.
	GetByEvents(ctx context.Context, eventIDs []int64) ([]domain.Market, error)
	// UpdateStatus меняет статус рынка (open/suspended/settled/void).
	// Возвращает domain.ErrNotFound, если рынка с таким id нет.
	UpdateStatus(ctx context.Context, id int64, status domain.MarketStatus) error
	// UpdateLine обновляет линию рынка (для TOTALS — основную линию, выбранную
	// OddsSyncWorker). line=nil сбрасывает линию в NULL. Возвращает
	// domain.ErrNotFound, если рынка с таким id нет.
	UpdateLine(ctx context.Context, id int64, line *decimal.Decimal) error
}

type MarketRepositoryImpl struct {
	pool *pgxpool.Pool
}

func NewMarketRepository(pool *pgxpool.Pool) *MarketRepositoryImpl {
	return &MarketRepositoryImpl{pool: pool}
}

func (r *MarketRepositoryImpl) CreateForEvent(ctx context.Context, m domain.Market) (int64, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		INSERT INTO markets (event_id, type, line, question, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`

	var id int64
	err := q.QueryRow(ctx, sql, m.EventID, m.Type, m.Line, m.Question, m.Status).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("MarketRepository.CreateForEvent event_id=%d type=%s: %w", m.EventID, m.Type, mapErr(err))
	}
	return id, nil
}

func (r *MarketRepositoryImpl) GetByID(ctx context.Context, id int64) (domain.Market, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		SELECT id, event_id, type, line, question, status
		FROM markets
		WHERE id = $1`

	var m domain.Market
	err := q.QueryRow(ctx, sql, id).Scan(&m.ID, &m.EventID, &m.Type, &m.Line, &m.Question, &m.Status)
	if err != nil {
		return domain.Market{}, fmt.Errorf("MarketRepository.GetByID id=%d: %w", id, mapErr(err))
	}
	return m, nil
}

func (r *MarketRepositoryImpl) GetByEvent(ctx context.Context, eventID int64) ([]domain.Market, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		SELECT id, event_id, type, line, question, status
		FROM markets
		WHERE event_id = $1
		ORDER BY id ASC`

	rows, err := q.Query(ctx, sql, eventID)
	if err != nil {
		return nil, fmt.Errorf("MarketRepository.GetByEvent event_id=%d: %w", eventID, mapErr(err))
	}
	defer rows.Close()

	var result []domain.Market
	for rows.Next() {
		var m domain.Market
		if err := rows.Scan(&m.ID, &m.EventID, &m.Type, &m.Line, &m.Question, &m.Status); err != nil {
			return nil, fmt.Errorf("MarketRepository.GetByEvent scan: %w", mapErr(err))
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("MarketRepository.GetByEvent rows: %w", mapErr(err))
	}
	return result, nil
}

func (r *MarketRepositoryImpl) GetByEvents(ctx context.Context, eventIDs []int64) ([]domain.Market, error) {
	if len(eventIDs) == 0 {
		return nil, nil
	}
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		SELECT id, event_id, type, line, question, status
		FROM markets
		WHERE event_id = ANY($1)
		ORDER BY event_id ASC, id ASC`

	rows, err := q.Query(ctx, sql, eventIDs)
	if err != nil {
		return nil, fmt.Errorf("MarketRepository.GetByEvents: %w", mapErr(err))
	}
	defer rows.Close()

	var result []domain.Market
	for rows.Next() {
		var m domain.Market
		if err := rows.Scan(&m.ID, &m.EventID, &m.Type, &m.Line, &m.Question, &m.Status); err != nil {
			return nil, fmt.Errorf("MarketRepository.GetByEvents scan: %w", mapErr(err))
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("MarketRepository.GetByEvents rows: %w", mapErr(err))
	}
	return result, nil
}

func (r *MarketRepositoryImpl) UpdateStatus(ctx context.Context, id int64, status domain.MarketStatus) error {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `UPDATE markets SET status = $2 WHERE id = $1`

	tag, err := q.Exec(ctx, sql, id, status)
	if err != nil {
		return fmt.Errorf("MarketRepository.UpdateStatus id=%d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("MarketRepository.UpdateStatus id=%d: %w", id, domain.ErrNotFound)
	}
	return nil
}

func (r *MarketRepositoryImpl) UpdateLine(ctx context.Context, id int64, line *decimal.Decimal) error {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `UPDATE markets SET line = $2 WHERE id = $1`

	tag, err := q.Exec(ctx, sql, id, line)
	if err != nil {
		return fmt.Errorf("MarketRepository.UpdateLine id=%d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("MarketRepository.UpdateLine id=%d: %w", id, domain.ErrNotFound)
	}
	return nil
}

var _ MarketRepository = (*MarketRepositoryImpl)(nil)
