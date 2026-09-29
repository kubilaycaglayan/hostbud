-- Looping queues: when the last item ends, requeue every item and run the
-- list again until the runtime limit since the loop started has passed.
-- Existing queues don't loop and keep their behavior.
-- +goose Up
ALTER TABLE queues ADD COLUMN loop_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE queues ADD COLUMN loop_max_runtime_seconds INTEGER NOT NULL DEFAULT 18000
    CHECK (loop_max_runtime_seconds > 0);
ALTER TABLE queues ADD COLUMN loop_started_at TIMESTAMPTZ;
ALTER TABLE queues ADD COLUMN loop_pass_started_at TIMESTAMPTZ;
ALTER TABLE queues ADD COLUMN loop_count INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
