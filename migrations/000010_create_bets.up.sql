-- Ставки пользователя. Ключевая таблица для расчётов (settlement) и кошелька.
-- odds — коэффициент, ЗАФИКСИРОВАННЫЙ в момент ставки (копия текущего
-- outcomes.odds на тот момент): дальнейшие изменения коэффициента воркером
-- на ставку не влияют. potential_payout = floor(stake * odds) считается при
-- размещении, чтобы при расчёте не зависеть от арифметики на «горячем» пути.
-- event_id — намеренная денормализация: позволяет выбирать ставки события
-- (для settlement и публичной истории) без JOIN к outcome→market→event.
-- FK без ON DELETE CASCADE намеренно: ставка — часть финансового аудита
-- (как wallet_transactions), удаление event/outcome/user не должно молча
-- стирать историю ставок; в MVP эти сущности не удаляются, только меняют статус.
CREATE TABLE bets (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT NOT NULL REFERENCES users(id),
    outcome_id       BIGINT NOT NULL REFERENCES outcomes(id),
    event_id         BIGINT NOT NULL REFERENCES events(id),
    stake            BIGINT NOT NULL CHECK (stake > 0),
    odds             NUMERIC(8,3) NOT NULL,                  -- зафиксированный коэффициент
    potential_payout BIGINT NOT NULL,                         -- floor(stake * odds)
    status           TEXT NOT NULL DEFAULT 'pending',         -- 'pending' | 'won' | 'lost' | 'void'
    settled_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- История ставок пользователя (GET /me/bets, /users/:id/bets): сортировка по давности.
CREATE INDEX idx_bets_user ON bets(user_id, created_at DESC);

-- Выборка ставок для расчёта события (SettlementService): только pending, по event_id.
-- Частичный индекс (WHERE status = 'pending') — компактный и ускоряет именно settlement;
-- рассчитанные ставки из него автоматически выпадают, не загрязняя индекс.
CREATE INDEX idx_bets_event_pending ON bets(event_id) WHERE status = 'pending';
