-- Исходы рынка. odds — ТЕКУЩИЙ коэффициент (его обновляет OddsSyncWorker);
-- в момент ставки коэффициент фиксируется в bets.odds, поэтому изменения здесь
-- ставку не затрагивают. CHECK odds > 1.0 — букмекерский коэффициент всегда > 1.
-- result заполняется при расчёте (settlement), до этого NULL.
CREATE TABLE outcomes (
    id        BIGSERIAL PRIMARY KEY,
    market_id BIGINT NOT NULL REFERENCES markets(id) ON DELETE CASCADE,
    code      TEXT NOT NULL,                    -- 'home'|'draw'|'away'|'over'|'under'|'opt_N'
    label     TEXT NOT NULL,                    -- отображаемое имя
    odds      NUMERIC(8,3) NOT NULL CHECK (odds > 1.0), -- текущий коэффициент
    result    TEXT                              -- NULL | 'won' | 'lost' | 'void'
);

CREATE INDEX idx_outcomes_market ON outcomes(market_id);
