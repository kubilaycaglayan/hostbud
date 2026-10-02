-- Migrations are append-only: never edit a released file, never drop user data.
-- The queue default prompt moves to each queue: an opt-in text that prefills
-- the queue's new item instructions (Queue panel). Off by default, text
-- ", commit regularly." (DefaultQueuePrompt). The per-machine columns from
-- 0019 are no longer read; they stay (append-only: nothing is removed).

-- +goose Up
ALTER TABLE queues ADD COLUMN default_prompt_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE queues ADD COLUMN default_prompt TEXT NOT NULL DEFAULT ', commit regularly.';

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
