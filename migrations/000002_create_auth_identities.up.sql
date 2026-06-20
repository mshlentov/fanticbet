-- Привязки внешних OAuth-провайдеров: у одного user может быть несколько
-- (например, Google + VK). Один внешний аккаунт — ровно один user (UNIQUE).
CREATE TABLE auth_identities (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider         TEXT NOT NULL,               -- 'google' | 'vk' | 'yandex'
    provider_user_id TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_user_id)
);
