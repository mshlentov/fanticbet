-- Откат: возвращаем неуникальный индекс по market_id, убираем уникальный.
DROP INDEX IF EXISTS idx_outcomes_market_code;

CREATE INDEX idx_outcomes_market ON outcomes(market_id);
