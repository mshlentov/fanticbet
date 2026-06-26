-- Привязка событий к чемпионатам (веха M8). league_id — внешняя ссылка на
-- leagues(id); NULLable, т.к. у произвольных (custom) и oddsapi-событий без
-- лиги чемпионата нет. league_name уже существует в схеме (миграция 000006)
-- как денормализованная текстовая копия (копия leagues.name / строка из API)
-- — удобно для ленты без JOIN; её мы не трогаем.
-- Новое значение source='manual' TEXT-колонки — отдельной миграции не требует.
ALTER TABLE events
    ADD COLUMN league_id BIGINT REFERENCES leagues(id);

-- Фильтр ленты событий по чемпионату (GET /events?league_id=, админка).
CREATE INDEX idx_events_league ON events(league_id);
