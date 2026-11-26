-- Migrations are append-only: never edit a released file, never drop user data.
-- V2-M15 stores one opt-in fixed-time nudge schedule and at-most-once
-- occurrence claims. A machine foreign key prevents removing its target.

-- +goose Up
CREATE TABLE window_nudge_settings (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    machine_id TEXT NOT NULL REFERENCES machines(id),
    directory TEXT NOT NULL CHECK (directory <> '' AND left(directory, 1) = '/'),
    provider TEXT NOT NULL CHECK (provider IN ('claude', 'codex')),
    schedule TEXT NOT NULL CHECK (schedule IN (
        '01-06-11-16-21', '02-07-12-17-22', '03-08-13-18-23',
        '04-09-14-19-00', '05-10-15-20-00'
    )),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    next_run_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((enabled AND next_run_at IS NOT NULL) OR (NOT enabled AND next_run_at IS NULL))
);

CREATE TABLE window_nudge_occurrences (
    scheduled_at TIMESTAMPTZ PRIMARY KEY,
    machine_id TEXT NOT NULL,
    provider TEXT NOT NULL CHECK (provider IN ('claude', 'codex')),
    outcome TEXT NOT NULL CHECK (outcome IN ('claimed', 'succeeded', 'failed', 'interrupted')),
    detail TEXT NOT NULL DEFAULT '' CHECK (length(detail) <= 1000),
    finished_at TIMESTAMPTZ
);
CREATE INDEX window_nudge_occurrences_latest ON window_nudge_occurrences (scheduled_at DESC);

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
