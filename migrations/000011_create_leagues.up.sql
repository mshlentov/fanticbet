-- Чемпионаты/лиги (веха M8). Справочник, группирующий события (АПЛ, НБА,
-- «Кубок двора» и т.д.). Заводится админом через /admin/leagues; на событие
-- ссылается events.league_id (см. миграцию 000012). sport_slug — чтобы
-- фильтровать лиги по виду спорта (football, basketball, ...).
-- У custom-событий league_id = NULL, поэтому 'custom' здесь не используется.
CREATE TABLE leagues (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    sport_slug  TEXT NOT NULL,                  -- 'football', 'basketball', ...; 'custom' — не используется
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Фильтр лиг по виду спорта (GET /admin/leagues?sport_slug=, публичный /leagues?sport_slug=).
CREATE INDEX idx_leagues_sport ON leagues(sport_slug);

-- updated_at поддерживаем тем же триггером, что и в M1 (функция set_updated_at).
CREATE TRIGGER trg_leagues_updated_at
    BEFORE UPDATE ON leagues
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();
