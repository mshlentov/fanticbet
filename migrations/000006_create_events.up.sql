-- События — единая модель для спортивных (source='oddsapi') и кастомных
-- (source='custom') событий. external_id — id события в Odds-API (NULL для
-- custom; UNIQUE(source, external_id) при NULL разрешает множество custom-строк).
-- scores — сырой JSON счёта из API, хранится для аудита расчёта (settlement).
CREATE TABLE events (
    id          BIGSERIAL PRIMARY KEY,
    source      TEXT NOT NULL,                 -- 'oddsapi' | 'custom'
    external_id BIGINT,                         -- id события в Odds-API (NULL для custom)
    sport_slug  TEXT NOT NULL,                  -- 'football', ...; 'custom' для кастомных
    league_name TEXT,
    title       TEXT NOT NULL,                  -- "Manchester United — Liverpool" или текст кастомного
    home        TEXT,                           -- NULL для кастомных
    away        TEXT,                           -- NULL для кастомных
    starts_at   TIMESTAMPTZ NOT NULL,
    status      TEXT NOT NULL DEFAULT 'upcoming', -- 'upcoming' | 'live' | 'settled' | 'cancelled'
    scores      JSONB,                          -- сырой scores из API; для аудита расчёта
    created_by  BIGINT REFERENCES users(id) ON DELETE SET NULL, -- админ-автор кастомного события
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source, external_id)
);

-- Индекс под ленту событий и выборки воркеров (фильтр по статусу + сортировка по старту).
CREATE INDEX idx_events_status_start ON events(status, starts_at);

-- updated_at поддерживаем тем же триггером, что и в M1 (функция set_updated_at).
CREATE TRIGGER trg_events_updated_at
    BEFORE UPDATE ON events
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();
