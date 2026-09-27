-- Migrations are append-only: never edit a released file, never drop user data.
-- v2 agent task queue (V2-M1, docs/roadmap-v2/ARCHITECTURE.md §6): one queue
-- per project, its items, one run per attempt at an item, and the run's audit
-- trail. No implicit cascades: deleting a queue is one explicit transaction.

-- +goose Up
CREATE TABLE queues (
    id         TEXT PRIMARY KEY,
    machine_id TEXT NOT NULL REFERENCES machines (id),
    project_id TEXT NOT NULL,
    name       TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'idle' CHECK (status IN ('idle', 'running', 'paused', 'finished')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, machine_id),
    FOREIGN KEY (project_id, machine_id) REFERENCES projects (id, machine_id)
);

CREATE TABLE queue_items (
    id          TEXT PRIMARY KEY,
    queue_id    TEXT NOT NULL,
    machine_id  TEXT NOT NULL REFERENCES machines (id),
    position    INTEGER NOT NULL,
    agent       TEXT NOT NULL CHECK (agent IN ('claude', 'codex')),
    flags       TEXT NOT NULL DEFAULT '',
    instruction TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'needs_attention', 'skipped')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (queue_id, position),
    UNIQUE (id, machine_id),
    FOREIGN KEY (queue_id, machine_id) REFERENCES queues (id, machine_id)
);

-- id is a ULID. token_hash is the SHA-256 of the run's bearer token (never the
-- token). transcript_path and transcript_offset are the bound transcript and
-- how far it has been read (design additions to §6).
CREATE TABLE runs (
    id                TEXT PRIMARY KEY,
    item_id           TEXT NOT NULL,
    machine_id        TEXT NOT NULL REFERENCES machines (id),
    session_name      TEXT NOT NULL DEFAULT '',
    agent_session_id  TEXT,
    transcript_path   TEXT,
    transcript_offset BIGINT,
    client_version    TEXT,
    token_hash        BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    status            TEXT NOT NULL DEFAULT 'starting' CHECK (status IN ('starting', 'running', 'achieved', 'failed', 'exited', 'stale', 'cancelled')),
    started_at        TIMESTAMPTZ NOT NULL,
    ended_at          TIMESTAMPTZ,
    last_signal_at    TIMESTAMPTZ,
    detail            TEXT,
    UNIQUE (id, machine_id),
    FOREIGN KEY (item_id, machine_id) REFERENCES queue_items (id, machine_id)
);
CREATE INDEX runs_item ON runs (item_id);
CREATE INDEX runs_status ON runs (status);

CREATE TABLE run_events (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id       TEXT NOT NULL,
    machine_id   TEXT NOT NULL REFERENCES machines (id),
    source       TEXT NOT NULL CHECK (source IN ('hook', 'poller', 'timer', 'user', 'llm')),
    kind         TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}' CHECK (octet_length(payload_json) <= 65536),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (run_id, machine_id) REFERENCES runs (id, machine_id)
);
CREATE INDEX run_events_run_created ON run_events (run_id, created_at);

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
