-- "Start after" on an existing queue: after_linked_at is when the current
-- link was set (NULL: at creation, created_at), and after_released records
-- that the linked session went idle and the queue started an item since.
-- Queues linked to a session that already started an item stay released.
-- +goose Up
ALTER TABLE queues ADD COLUMN after_linked_at TIMESTAMPTZ;
ALTER TABLE queues ADD COLUMN after_released BOOLEAN NOT NULL DEFAULT false;
UPDATE queues q SET after_released = true
WHERE q.after_session IS NOT NULL
  AND EXISTS (SELECT 1 FROM queue_items i WHERE i.queue_id = q.id AND i.status <> 'queued');

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
