-- Migrations are append-only: never edit a released file, never drop user data.
-- V2-M13 servers: a custom machine row carries its connection (host name,
-- port, user) and the host keys the owner confirmed in the UI, as
-- known_hosts key lines ("<type> <base64>", one per line). The host row
-- keeps the defaults: its connection comes from the environment.

-- +goose Up
ALTER TABLE machines ADD COLUMN host_name TEXT NOT NULL DEFAULT '';
ALTER TABLE machines ADD COLUMN port INTEGER NOT NULL DEFAULT 22 CHECK (port BETWEEN 1 AND 65535);
ALTER TABLE machines ADD COLUMN ssh_user TEXT NOT NULL DEFAULT '';
ALTER TABLE machines ADD COLUMN host_keys TEXT NOT NULL DEFAULT '';

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
