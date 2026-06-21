package repository

import (
	"context"
	"fmt"
	"strings"

	"fanticbet/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// OutcomeRepository — интерфейс работы с таблицей outcomes (исходы рынка).
type OutcomeRepository interface {
	// Upsert вставляет или обновляет исход по (market_id, code) и возвращает его id.
	// Первый прогон OddsSyncWorker создаёт исход, последующие обновляют label и odds.
	// Требует уникального индекса idx_outcomes_market_code (миграция 000009).
	Upsert(ctx context.Context, o domain.Outcome) (int64, error)
	// GetByID возвращает исход по id (domain.ErrNotFound, если нет). Используется
	// BettingService.PlaceBet: по outcome_id проверяем доступность ставки.
	GetByID(ctx context.Context, id int64) (domain.Outcome, error)
	// GetByMarket возвращает все исходы рынка (порядок по id).
	GetByMarket(ctx context.Context, marketID int64) ([]domain.Outcome, error)
	// GetByMarkets возвращает исходы сразу нескольких рынков одним запросом
	// (для ленты GET /events — чтобы не делать N+1). Порядок по market_id, id.
	GetByMarkets(ctx context.Context, marketIDs []int64) ([]domain.Outcome, error)
	// UpdateOdds обновляет текущий коэффициент исхода. Возвращает domain.ErrNotFound,
	// если исхода с таким id нет.
	UpdateOdds(ctx context.Context, id int64, odds decimal.Decimal) error
	// UpdateResult проставляет результат исхода (won/lost/void) при расчёте.
	// Возвращает domain.ErrNotFound, если исхода с таким id нет.
	UpdateResult(ctx context.Context, id int64, result domain.Result) error
	// UpdateLabelAndOdds правит label и/или коэффициент исхода. nil-поля оставляют
	// текущее значение. Используется админом для правки исходов кастомного события.
	// Возвращает domain.ErrNotFound, если исхода с таким id нет.
	UpdateLabelAndOdds(ctx context.Context, id int64, label *string, odds *decimal.Decimal) error
}

type OutcomeRepositoryImpl struct {
	pool *pgxpool.Pool
}

func NewOutcomeRepository(pool *pgxpool.Pool) *OutcomeRepositoryImpl {
	return &OutcomeRepositoryImpl{pool: pool}
}

// Upsert — INSERT ... ON CONFLICT (market_id, code). result намеренно не трогаем
// на конфликте: его выставляет только settlement через UpdateResult, и повторный
// апдейт котировок не должен сбрасывать уже рассчитанный исход.
func (r *OutcomeRepositoryImpl) Upsert(ctx context.Context, o domain.Outcome) (int64, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		INSERT INTO outcomes (market_id, code, label, odds)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (market_id, code) DO UPDATE SET
			label = EXCLUDED.label,
			odds  = EXCLUDED.odds
		RETURNING id`

	var id int64
	err := q.QueryRow(ctx, sql, o.MarketID, o.Code, o.Label, o.Odds).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("OutcomeRepository.Upsert market_id=%d code=%s: %w", o.MarketID, o.Code, mapErr(err))
	}
	return id, nil
}

func (r *OutcomeRepositoryImpl) GetByID(ctx context.Context, id int64) (domain.Outcome, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		SELECT id, market_id, code, label, odds, result
		FROM outcomes
		WHERE id = $1`

	var o domain.Outcome
	err := q.QueryRow(ctx, sql, id).Scan(&o.ID, &o.MarketID, &o.Code, &o.Label, &o.Odds, &o.Result)
	if err != nil {
		return domain.Outcome{}, fmt.Errorf("OutcomeRepository.GetByID id=%d: %w", id, mapErr(err))
	}
	return o, nil
}

func (r *OutcomeRepositoryImpl) GetByMarket(ctx context.Context, marketID int64) ([]domain.Outcome, error) {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		SELECT id, market_id, code, label, odds, result
		FROM outcomes
		WHERE market_id = $1
		ORDER BY id ASC`

	rows, err := q.Query(ctx, sql, marketID)
	if err != nil {
		return nil, fmt.Errorf("OutcomeRepository.GetByMarket market_id=%d: %w", marketID, mapErr(err))
	}
	defer rows.Close()

	var result []domain.Outcome
	for rows.Next() {
		var o domain.Outcome
		if err := rows.Scan(&o.ID, &o.MarketID, &o.Code, &o.Label, &o.Odds, &o.Result); err != nil {
			return nil, fmt.Errorf("OutcomeRepository.GetByMarket scan: %w", mapErr(err))
		}
		result = append(result, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("OutcomeRepository.GetByMarket rows: %w", mapErr(err))
	}
	return result, nil
}

func (r *OutcomeRepositoryImpl) GetByMarkets(ctx context.Context, marketIDs []int64) ([]domain.Outcome, error) {
	if len(marketIDs) == 0 {
		return nil, nil
	}
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `
		SELECT id, market_id, code, label, odds, result
		FROM outcomes
		WHERE market_id = ANY($1)
		ORDER BY market_id ASC, id ASC`

	rows, err := q.Query(ctx, sql, marketIDs)
	if err != nil {
		return nil, fmt.Errorf("OutcomeRepository.GetByMarkets: %w", mapErr(err))
	}
	defer rows.Close()

	var result []domain.Outcome
	for rows.Next() {
		var o domain.Outcome
		if err := rows.Scan(&o.ID, &o.MarketID, &o.Code, &o.Label, &o.Odds, &o.Result); err != nil {
			return nil, fmt.Errorf("OutcomeRepository.GetByMarkets scan: %w", mapErr(err))
		}
		result = append(result, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("OutcomeRepository.GetByMarkets rows: %w", mapErr(err))
	}
	return result, nil
}

func (r *OutcomeRepositoryImpl) UpdateOdds(ctx context.Context, id int64, odds decimal.Decimal) error {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `UPDATE outcomes SET odds = $2 WHERE id = $1`

	tag, err := q.Exec(ctx, sql, id, odds)
	if err != nil {
		return fmt.Errorf("OutcomeRepository.UpdateOdds id=%d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("OutcomeRepository.UpdateOdds id=%d: %w", id, domain.ErrNotFound)
	}
	return nil
}

func (r *OutcomeRepositoryImpl) UpdateResult(ctx context.Context, id int64, result domain.Result) error {
	q := QuerierFromCtx(ctx, r.pool)

	const sql = `UPDATE outcomes SET result = $2 WHERE id = $1`

	tag, err := q.Exec(ctx, sql, id, result)
	if err != nil {
		return fmt.Errorf("OutcomeRepository.UpdateResult id=%d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("OutcomeRepository.UpdateResult id=%d: %w", id, domain.ErrNotFound)
	}
	return nil
}

// UpdateLabelAndOdds динамически собирает SET по non-nil аргументам: nil-поля
// не добавляются в UPDATE. Если оба поля nil — сводится к проверке существования
// исхода (чтобы пустой PATCH не маскировал 404).
func (r *OutcomeRepositoryImpl) UpdateLabelAndOdds(ctx context.Context, id int64, label *string, odds *decimal.Decimal) error {
	q := QuerierFromCtx(ctx, r.pool)

	sets := []string{}
	args := []any{id}
	if label != nil {
		args = append(args, *label)
		sets = append(sets, fmt.Sprintf("label = $%d", len(args)))
	}
	if odds != nil {
		args = append(args, *odds)
		sets = append(sets, fmt.Sprintf("odds = $%d", len(args)))
	}
	if len(sets) == 0 {
		const checkSQL = `SELECT 1 FROM outcomes WHERE id = $1`
		var one int
		if err := q.QueryRow(ctx, checkSQL, id).Scan(&one); err != nil {
			return fmt.Errorf("OutcomeRepository.UpdateLabelAndOdds id=%d: %w", id, mapErr(err))
		}
		return nil
	}

	sql := fmt.Sprintf(`UPDATE outcomes SET %s WHERE id = $1`, strings.Join(sets, ", "))

	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("OutcomeRepository.UpdateLabelAndOdds id=%d: %w", id, mapErr(err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("OutcomeRepository.UpdateLabelAndOdds id=%d: %w", id, domain.ErrNotFound)
	}
	return nil
}

var _ OutcomeRepository = (*OutcomeRepositoryImpl)(nil)
