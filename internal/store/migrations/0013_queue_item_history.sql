-- Durable, high-level queue item history. This table intentionally has no
-- foreign keys to live queue rows; deleting a queue must not erase history.
-- It records metadata only, never session output or hook payloads.
-- +goose Up
CREATE TABLE queue_item_history (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    machine_id TEXT NOT NULL,
    queue_id TEXT NOT NULL,
    queue_name TEXT NOT NULL,
    project_name TEXT NOT NULL,
    item_id TEXT NOT NULL,
    position INTEGER NOT NULL,
    execution_mode TEXT NOT NULL,
    target_session TEXT NOT NULL DEFAULT '',
    agent TEXT NOT NULL,
    flags TEXT NOT NULL,
    instruction TEXT NOT NULL,
    command TEXT NOT NULL,
    verify_command TEXT NOT NULL DEFAULT '',
    requires_approval BOOLEAN NOT NULL DEFAULT false,
    item_status TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('created', 'edited', 'status', 'deleted')),
    detail TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX queue_item_history_machine_time ON queue_item_history (machine_id, occurred_at DESC, id DESC);

-- +goose StatementBegin
CREATE FUNCTION hostbud_record_queue_item_history() RETURNS trigger AS $$
DECLARE
    item queue_items%ROWTYPE;
    history_action TEXT;
    q_name TEXT;
    p_name TEXT;
    run_detail TEXT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        item := OLD;
        history_action := 'deleted';
    ELSE
        item := NEW;
        IF TG_OP = 'INSERT' THEN
            history_action := 'created';
        ELSIF OLD.status IS DISTINCT FROM NEW.status THEN
            history_action := 'status';
        ELSIF ROW(OLD.position, OLD.agent, OLD.flags, OLD.instruction, OLD.execution_mode, OLD.target_session, OLD.command, OLD.verify_command, OLD.requires_approval)
            IS DISTINCT FROM ROW(NEW.position, NEW.agent, NEW.flags, NEW.instruction, NEW.execution_mode, NEW.target_session, NEW.command, NEW.verify_command, NEW.requires_approval) THEN
            history_action := 'edited';
        ELSE
            RETURN NEW;
        END IF;
    END IF;
    SELECT q.name, COALESCE(p.name, '(deleted project)')
      INTO q_name, p_name
      FROM queues q LEFT JOIN projects p ON p.id = q.project_id
     WHERE q.id = item.queue_id;
    SELECT COALESCE(
        (SELECT NULLIF(COALESCE(NULLIF(r.detail, ''), (
            SELECT payload_json::jsonb ->> 'detail' FROM run_events
             WHERE run_id = r.id AND source = 'verify' AND kind = 'verify_result'
             ORDER BY id DESC LIMIT 1
        ), ''), '') FROM runs r WHERE r.item_id = item.id ORDER BY r.started_at DESC LIMIT 1),
        (SELECT detail FROM queue_item_history WHERE item_id = item.id AND detail <> '' ORDER BY id DESC LIMIT 1),
        '') INTO run_detail;
    INSERT INTO queue_item_history
      (machine_id, queue_id, queue_name, project_name, item_id, position,
       execution_mode, target_session, agent, flags, instruction, command, verify_command, requires_approval, item_status,
       action, detail)
    VALUES
      (item.machine_id, item.queue_id, COALESCE(q_name, '(deleted queue)'), COALESCE(p_name, '(deleted project)'),
       item.id, item.position, COALESCE(item.execution_mode, 'agent'), COALESCE(item.target_session, ''), item.agent,
       item.flags, item.instruction, COALESCE(item.command, ''), COALESCE(item.verify_command, ''), COALESCE(item.requires_approval, false), item.status,
       history_action, COALESCE(run_detail, ''));
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER queue_items_history
AFTER INSERT OR UPDATE OR DELETE ON queue_items
FOR EACH ROW EXECUTE FUNCTION hostbud_record_queue_item_history();

-- Preserve a useful initial snapshot for queues that predate this migration.
INSERT INTO queue_item_history
  (machine_id, queue_id, queue_name, project_name, item_id, position,
   execution_mode, target_session, agent, flags, instruction, command, verify_command, requires_approval, item_status,
   action, detail, occurred_at)
SELECT i.machine_id, i.queue_id, q.name, COALESCE(p.name, '(deleted project)'), i.id, i.position,
       COALESCE(i.execution_mode, 'agent'), COALESCE(i.target_session, ''), i.agent, i.flags, i.instruction, COALESCE(i.command, ''), COALESCE(i.verify_command, ''), COALESCE(i.requires_approval, false), i.status,
       'status', COALESCE(r.detail, ''), i.updated_at
  FROM queue_items i
  JOIN queues q ON q.id = i.queue_id
  LEFT JOIN projects p ON p.id = q.project_id
  LEFT JOIN LATERAL (
      SELECT COALESCE(NULLIF(r.detail, ''), (
          SELECT payload_json::jsonb ->> 'detail' FROM run_events
           WHERE run_id = r.id AND source = 'verify' AND kind = 'verify_result'
           ORDER BY id DESC LIMIT 1
      ), '') AS detail
        FROM runs r WHERE r.item_id = i.id ORDER BY r.started_at DESC LIMIT 1
  ) r ON true;

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
