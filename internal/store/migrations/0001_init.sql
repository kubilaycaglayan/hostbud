-- Migrations are append-only: never edit a released file, never drop user data.
-- Keep to portable SQL (no SQLite-only syntax) so a Postgres move stays cheap.

-- +goose Up
CREATE TABLE machines (
    id           TEXT PRIMARY KEY,
    source       TEXT NOT NULL CHECK (source IN ('host', 'sshconfig', 'custom')),
    ssh_alias    TEXT NOT NULL,
    label        TEXT NOT NULL,
    active       BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order   INTEGER NOT NULL DEFAULT 0,
    hidden       BOOLEAN NOT NULL DEFAULT FALSE,
    os           TEXT NOT NULL DEFAULT '',
    tmux_version TEXT NOT NULL DEFAULT '',
    tmux_missing BOOLEAN NOT NULL DEFAULT FALSE,
    last_seen_at TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE TABLE ui_state (
    key        TEXT PRIMARY KEY,
    value_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
