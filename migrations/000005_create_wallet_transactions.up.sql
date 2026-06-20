-- Журнал всех движений фантиков (append-only). Баланс кошелька должен сходиться
-- с суммой транзакций — это защита от багов и аудит. bet_id — ссылка на ставку,
-- когда движение связано со ставкой (в M3 появится таблица bets).
CREATE TABLE wallet_transactions (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount        BIGINT NOT NULL,                -- + начисление, − списание
    type          TEXT NOT NULL,                  -- 'signup_bonus' | 'bet_stake'
                                                   -- | 'bet_payout' | 'bet_refund'
                                                   -- | 'admin_adjust'
    bet_id        BIGINT,                         -- ссылка на ставку (с M3)
    balance_after BIGINT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_wtx_user ON wallet_transactions(user_id, created_at DESC);
