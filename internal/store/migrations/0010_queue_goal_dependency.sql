-- Queue dependencies on an already active, tracked agent goal (V2-M6).
-- Existing queues have no predecessor and retain their behavior.
-- +goose Up
-- Keep the identifier even if the source queue is later deleted: a missing
-- predecessor must fail closed instead of silently releasing its successor.
ALTER TABLE queues ADD COLUMN after_run_id TEXT;

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
