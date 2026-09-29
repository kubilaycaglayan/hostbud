-- Queue dependencies on any existing tmux session, tracked or not.
-- Existing queues have no linked session and retain their behavior.
-- +goose Up
ALTER TABLE queues ADD COLUMN after_session TEXT;

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
