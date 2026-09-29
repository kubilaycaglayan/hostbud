-- Token usage per run, summed from the agent's transcript (Claude Code):
-- input counts fresh, cache-write and cache-read input tokens. usage_offset
-- is how far the transcript has been read for usage. Existing runs keep 0.
-- +goose Up
ALTER TABLE runs ADD COLUMN input_tokens BIGINT NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN output_tokens BIGINT NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN usage_offset BIGINT NOT NULL DEFAULT 0;

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
