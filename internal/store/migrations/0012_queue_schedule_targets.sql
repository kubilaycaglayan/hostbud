-- Durable delayed starts and item execution targets (V2-M8).
-- +goose Up
ALTER TABLE queues ADD COLUMN scheduled_at TIMESTAMPTZ;
ALTER TABLE queue_items ADD COLUMN execution_mode TEXT NOT NULL DEFAULT 'agent'
    CHECK (execution_mode IN ('agent', 'session'));
ALTER TABLE queue_items ADD COLUMN target_session TEXT NOT NULL DEFAULT '';
ALTER TABLE queue_items ADD COLUMN command TEXT NOT NULL DEFAULT '';

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
