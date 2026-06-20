-- Уникальность исхода в пределах рынка по коду (home/draw/away/over/under/opt_N).
-- Нужна для OutcomeRepository.Upsert (ON CONFLICT (market_id, code)): OddsSyncWorker
-- сначала создаёт исход, затем при каждом прогоне обновляет его коэффициент.
-- Этот уникальный индекс перекрывает прежний idx_outcomes_market (префикс market_id),
-- поэтому старый индекс удаляем как избыточный.
DROP INDEX IF EXISTS idx_outcomes_market;

CREATE UNIQUE INDEX idx_outcomes_market_code ON outcomes(market_id, code);
