-- Кошелёк пользователя. Баланс — целое (фантики), CHECK не даёт уйти в минус.
-- updated_at обновляем триггером, созданным в миграции 000001.
CREATE TABLE wallets (
    user_id    BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    balance    BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER trg_wallets_updated_at
    BEFORE UPDATE ON wallets
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();
