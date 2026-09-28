-- Migrations are append-only: never edit a released file, never drop user data.
-- v2 completion gates (V2-M4, docs/roadmap-v2/ARCHITECTURE.md §6): an item's
-- optional verify command and approval switch, the two gate states, and the
-- verify runner's run-event source. Existing rows keep their values; the new
-- CHECK sets are strict supersets, so every existing row stays valid.

-- +goose Up
-- verify_command NULL = no verify gate (the API stores an empty command as NULL).
ALTER TABLE queue_items ADD COLUMN verify_command TEXT CHECK (octet_length(verify_command) BETWEEN 1 AND 4096);
ALTER TABLE queue_items ADD COLUMN requires_approval BOOLEAN NOT NULL DEFAULT FALSE;

-- The constraint names are PostgreSQL's defaults for 0005's inline CHECKs.
ALTER TABLE queue_items
    DROP CONSTRAINT queue_items_status_check,
    ADD CONSTRAINT queue_items_status_check CHECK (status IN ('queued', 'running', 'verifying', 'awaiting_approval', 'done', 'needs_attention', 'skipped'));
ALTER TABLE run_events
    DROP CONSTRAINT run_events_source_check,
    ADD CONSTRAINT run_events_source_check CHECK (source IN ('hook', 'poller', 'timer', 'user', 'llm', 'verify'));

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
