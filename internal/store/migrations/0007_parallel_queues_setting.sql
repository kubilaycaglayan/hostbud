-- Migrations are append-only: never edit a released file, never drop user data.
-- The parallel-queues switch as an owner setting (Queue panel): NULL keeps
-- the HOSTBUD_PARALLEL_QUEUES default; true/false overrides it. Only additions.

-- +goose Up
ALTER TABLE machine_capacity ADD COLUMN parallel_queues BOOLEAN;

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
