-- Migrations are append-only: never edit a released file, never drop user data.
-- v2 parallel queues (V2-M2, docs/roadmap-v2/ARCHITECTURE.md §6): an optional
-- per-machine cap on active runs, the time a queue started waiting for a slot
-- (FIFO order), and unique queue names per project. Only additions.

-- +goose Up
-- max_concurrent_runs NULL = no cap; no row = no cap.
CREATE TABLE machine_capacity (
    machine_id          TEXT PRIMARY KEY REFERENCES machines (id),
    max_concurrent_runs INTEGER CHECK (max_concurrent_runs BETWEEN 1 AND 32),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE queues ADD COLUMN waiting_since TIMESTAMPTZ;

-- V2-M1 allowed one queue per machine, so no existing rows can conflict.
CREATE UNIQUE INDEX queues_project_name ON queues (project_id, lower(name));

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
