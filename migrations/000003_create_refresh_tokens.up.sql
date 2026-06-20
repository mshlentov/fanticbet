-- Refresh-токены. В БД хранится sha256-хэш, не сам токен. revoked_at — для
-- logout и инвалидации скомпрометированных сессий.
CREATE TABLE refresh_tokens (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);

CREATE INDEX idx_refresh_tokens_hash ON refresh_tokens(token_hash);
