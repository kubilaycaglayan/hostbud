-- Migrations are append-only: never edit a released file, never drop user data.
-- Authentication (docs/ARCHITECTURE.md §8.1–8.2).

-- +goose Up
CREATE TABLE users (
    id               TEXT PRIMARY KEY,
    email            TEXT NOT NULL,
    email_normalized TEXT NOT NULL UNIQUE,
    password_hash    TEXT NOT NULL,
    disabled         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at    TIMESTAMPTZ
);

-- Owner-managed with plain SQL, e.g.
--   INSERT INTO email_allowlist (email_normalized) VALUES ('person@example.com');
--   UPDATE email_allowlist SET enabled = FALSE WHERE email_normalized = 'person@example.com';
CREATE TABLE email_allowlist (
    email_normalized TEXT PRIMARY KEY
        CHECK (email_normalized = lower(btrim(email_normalized)) AND email_normalized <> ''),
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    note             TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Only a SHA-256 hash of the cookie token is stored.
CREATE TABLE auth_sessions (
    id_hash      TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_agent   TEXT NOT NULL DEFAULT '',
    created_ip   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX auth_sessions_user_id ON auth_sessions (user_id);
CREATE INDEX auth_sessions_expires_at ON auth_sessions (expires_at);

-- scope_key is a keyed hash (never a raw email or IP).
CREATE TABLE login_rate_limits (
    scope_key       TEXT PRIMARY KEY,
    failures        INTEGER NOT NULL DEFAULT 0,
    blocked_until   TIMESTAMPTZ,
    last_failure_at TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
