-- Раздел «Популярные события» (веха M9, задача 4). Метка «популярное» — одна
-- колонка featured_at: NULL = обычное событие; заполнено = популярное. Одно поле
-- даёт и флаг, и порядок (ORDER BY featured_at DESC — последнее добавленное сверху).
-- Без отдельной таблицы и без sort_order.
ALTER TABLE events
    ADD COLUMN featured_at TIMESTAMPTZ;

-- Частичный индекс: ускоряет выборку популярных (featured=true) и их сортировку.
-- Покрывает только помеченные события — обычные (NULL) не раздувают индекс.
CREATE INDEX idx_events_featured ON events(featured_at DESC) WHERE featured_at IS NOT NULL;
