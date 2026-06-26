-- Откат: убираем колонку featured_at (частичный индекс удаляется вместе с ней).
ALTER TABLE events
    DROP COLUMN IF EXISTS featured_at;
