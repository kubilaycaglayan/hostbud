-- Migrations are append-only: never edit a released file, never drop user data.

-- +goose Up
-- The probed home directory (the default start directory for new sessions).
ALTER TABLE machines ADD COLUMN home TEXT NOT NULL DEFAULT '';
