-- Record the coding agent's session id (Claude session_id, Codex thread id)
-- on queue item history, so a past item can be traced back to its agent
-- session (e.g. `claude --resume <id>`). Metadata only, like the rest.
-- +goose Up
ALTER TABLE queue_item_history ADD COLUMN agent_session_id TEXT NOT NULL DEFAULT '';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION hostbud_record_queue_item_history() RETURNS trigger AS $$
DECLARE
    item queue_items%ROWTYPE;
    history_action TEXT;
    q_name TEXT;
    p_name TEXT;
    run_detail TEXT;
    run_agent_session TEXT;
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
    -- The coding agent's own session id (Claude session_id, Codex thread id)
    -- from the item's latest bound run, or the last one history recorded.
    SELECT COALESCE(
        (SELECT NULLIF(r.agent_session_id, '') FROM runs r WHERE r.item_id = item.id ORDER BY r.started_at DESC LIMIT 1),
        (SELECT agent_session_id FROM queue_item_history WHERE item_id = item.id AND agent_session_id <> '' ORDER BY id DESC LIMIT 1),
        '') INTO run_agent_session;
    INSERT INTO queue_item_history
      (machine_id, queue_id, queue_name, project_name, item_id, position,
       execution_mode, target_session, agent, flags, instruction, command, verify_command, requires_approval, item_status,
       action, detail, agent_session_id)
    VALUES
      (item.machine_id, item.queue_id, COALESCE(q_name, '(deleted queue)'), COALESCE(p_name, '(deleted project)'),
       item.id, item.position, COALESCE(item.execution_mode, 'agent'), COALESCE(item.target_session, ''), item.agent,
       item.flags, item.instruction, COALESCE(item.command, ''), COALESCE(item.verify_command, ''), COALESCE(item.requires_approval, false), item.status,
       history_action, COALESCE(run_detail, ''), COALESCE(run_agent_session, ''));
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- Backfill: the latest bound run that started before each snapshot.
UPDATE queue_item_history h SET agent_session_id = COALESCE((
    SELECT r.agent_session_id FROM runs r
     WHERE r.item_id = h.item_id AND COALESCE(r.agent_session_id, '') <> '' AND r.started_at <= h.occurred_at
     ORDER BY r.started_at DESC LIMIT 1
), '')
 WHERE h.agent_session_id = '';

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
