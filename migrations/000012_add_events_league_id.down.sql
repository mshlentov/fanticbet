-- Откат: убираем колонку league_id (FK и индекс удаляются вместе с ней).
-- league_name добавлен в миграции 000006 и здесь не трогается.
ALTER TABLE events
    DROP COLUMN IF EXISTS league_id;
