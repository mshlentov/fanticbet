-- Рынки события. Одно событие → несколько рынков (для oddsapi: ML и основной
-- TOTALS; для custom: один CUSTOM). line заполняется только для TOTALS (линия
-- тотала, напр. 2.5), question — только для CUSTOM (текст вопроса).
CREATE TABLE markets (
    id       BIGSERIAL PRIMARY KEY,
    event_id BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    type     TEXT NOT NULL,                     -- 'ML' | 'TOTALS' | 'CUSTOM'
    line     NUMERIC(6,2),                      -- линия тотала (NULL для ML/CUSTOM)
    question TEXT,                              -- текст вопроса (только CUSTOM)
    status   TEXT NOT NULL DEFAULT 'open'       -- 'open' | 'suspended' | 'settled' | 'void'
);

CREATE INDEX idx_markets_event ON markets(event_id);
