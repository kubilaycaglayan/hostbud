-- Queue and item lifecycle timestamps (V2-M7).
-- +goose Up
ALTER TABLE queues ADD COLUMN started_at TIMESTAMPTZ;
ALTER TABLE queues ADD COLUMN ended_at TIMESTAMPTZ;
ALTER TABLE queue_items ADD COLUMN started_at TIMESTAMPTZ;
ALTER TABLE queue_items ADD COLUMN ended_at TIMESTAMPTZ;

-- Centralize lifecycle capture so every guarded transition, including
-- notification transactions, records the same timestamps.
-- +goose StatementBegin
CREATE FUNCTION hostbud_queue_lifecycle_times() RETURNS trigger AS $$
BEGIN
  IF NEW.status = 'running' AND OLD.status <> 'running' AND NEW.started_at IS NULL THEN
    NEW.started_at := now();
  END IF;
  IF NEW.status = 'finished' AND OLD.status <> 'finished' THEN
    NEW.ended_at := now();
  ELSIF NEW.status <> 'finished' THEN
    NEW.ended_at := NULL;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER queues_lifecycle_times
BEFORE UPDATE OF status ON queues
FOR EACH ROW EXECUTE FUNCTION hostbud_queue_lifecycle_times();

-- +goose StatementBegin
CREATE FUNCTION hostbud_queue_item_lifecycle_times() RETURNS trigger AS $$
BEGIN
  IF NEW.status = 'running' AND OLD.status <> 'running' AND NEW.started_at IS NULL THEN
    NEW.started_at := now();
  END IF;
  IF NEW.status IN ('done', 'needs_attention', 'skipped') AND OLD.status IS DISTINCT FROM NEW.status THEN
    NEW.ended_at := now();
  ELSIF NEW.status IN ('queued', 'running', 'verifying', 'awaiting_approval') THEN
    NEW.ended_at := NULL;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER queue_items_lifecycle_times
BEFORE UPDATE OF status ON queue_items
FOR EACH ROW EXECUTE FUNCTION hostbud_queue_item_lifecycle_times();

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
