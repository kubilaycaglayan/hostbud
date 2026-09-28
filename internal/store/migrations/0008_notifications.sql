-- Migrations are append-only: never edit a released file, never drop user data.
-- v2 notifications (V2-M3, docs/roadmap-v2/ARCHITECTURE.md §6): the
-- per-account switch and event choices, Web Push subscriptions, and the
-- outbox of notifications to deliver. Only additions.

-- +goose Up
-- No row = off. The event choices apply once the account turns it on.
CREATE TABLE notification_prefs (
    user_id      TEXT PRIMARY KEY REFERENCES users (id),
    enabled      BOOLEAN NOT NULL DEFAULT FALSE,
    on_done      BOOLEAN NOT NULL DEFAULT TRUE,
    on_attention BOOLEAN NOT NULL DEFAULT TRUE,
    on_finished  BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per device (browser push endpoint); an endpoint belongs to the
-- account that subscribed it last.
CREATE TABLE push_subscriptions (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id),
    endpoint   TEXT NOT NULL UNIQUE CHECK (length(endpoint) BETWEEN 1 AND 2048),
    p256dh     TEXT NOT NULL CHECK (length(p256dh) BETWEEN 1 AND 256),
    auth       TEXT NOT NULL CHECK (length(auth) BETWEEN 1 AND 256),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX push_subscriptions_user_id ON push_subscriptions (user_id);

-- One row per account and event (dedupe_key), written in the transaction
-- of the transition that caused it.
CREATE TABLE notification_outbox (
    id           BIGSERIAL PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id),
    dedupe_key   TEXT NOT NULL CHECK (length(dedupe_key) BETWEEN 1 AND 200),
    payload_json TEXT NOT NULL CHECK (length(payload_json) <= 1024),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, dedupe_key)
);
CREATE INDEX notification_outbox_created_at ON notification_outbox (created_at);

-- One row per outbox row and device. A delivery is claimed (claimed_at)
-- before its POST and never sent again after that (at most once).
CREATE TABLE notification_deliveries (
    outbox_id       BIGINT NOT NULL REFERENCES notification_outbox (id),
    subscription_id TEXT NOT NULL REFERENCES push_subscriptions (id),
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    claimed_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    PRIMARY KEY (outbox_id, subscription_id)
);
CREATE INDEX notification_deliveries_pending ON notification_deliveries (outbox_id) WHERE claimed_at IS NULL;
CREATE INDEX notification_deliveries_subscription ON notification_deliveries (subscription_id);

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
