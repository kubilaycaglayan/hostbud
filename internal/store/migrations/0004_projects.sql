-- Migrations are append-only: never edit a released file, never drop user data.
-- Project metadata and project-scoped recent start commands (M4).

-- +goose Up
CREATE TABLE projects (
    id           TEXT PRIMARY KEY,
    machine_id   TEXT NOT NULL REFERENCES machines (id),
    path         TEXT NOT NULL,
    name         TEXT NOT NULL,
    sort_order   INTEGER NOT NULL DEFAULT 0,
    pinned       BOOLEAN NOT NULL DEFAULT FALSE,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (machine_id, path),
    UNIQUE (id, machine_id)
);
CREATE INDEX projects_machine_order ON projects (machine_id, sort_order, name, id);

CREATE TABLE session_links (
    machine_id   TEXT NOT NULL REFERENCES machines (id),
    session_name TEXT NOT NULL,
    project_id   TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (machine_id, session_name),
    FOREIGN KEY (project_id, machine_id) REFERENCES projects (id, machine_id) ON DELETE CASCADE
);

CREATE TABLE recent_commands (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    command      TEXT NOT NULL,
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, command)
);
CREATE INDEX recent_commands_project_order ON recent_commands (project_id, last_used_at DESC, command);

-- +goose Down
-- Intentionally empty. Production migrations are append-only.
