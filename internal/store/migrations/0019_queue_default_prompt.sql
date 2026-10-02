-- Migrations are append-only: never edit a released file, never drop user data.
-- The queue default prompt (Settings): an opt-in text that prefills each new
-- queue item's instruction. NULL text keeps the built-in default
-- (DefaultQueuePrompt). Only additions.

-- +goose Up
ALTER TABLE machine_capacity ADD COLUMN default_prompt_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE machine_capacity ADD COLUMN default_prompt TEXT;

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
