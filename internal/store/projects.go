package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

// RecentCommandLimit bounds the history kept for each project.
const RecentCommandLimit = 20

const (
	maxProjectPathBytes = 4096
	maxProjectNameBytes = 255
	maxCommandBytes     = 4096
)

// Project is a saved target directory and its display metadata.
type Project struct {
	ID         string
	MachineID  string
	Path       string
	Name       string
	SortOrder  int
	Pinned     bool
	LastUsedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SessionLink records the project explicitly associated with a live session.
type SessionLink struct {
	MachineID   string
	SessionName string
	ProjectID   string
	CreatedAt   time.Time
}

// RecentCommand is an exact start-command string recently used in a project.
type RecentCommand struct {
	ID         int64
	ProjectID  string
	Command    string
	LastUsedAt time.Time
}

const projectCols = `id, machine_id, path, name, sort_order, pinned, last_used_at, created_at, updated_at`

func normalizeProjectPath(raw string) (string, error) {
	if raw == "" || len(raw) > maxProjectPathBytes || strings.IndexByte(raw, 0) >= 0 || !path.IsAbs(raw) {
		return "", errors.New("project path must be an absolute POSIX path of at most 4096 bytes")
	}
	return path.Clean(raw), nil
}

func normalizeProjectName(raw, projectPath string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" && raw == "" {
		name = path.Base(projectPath)
	}
	if name == "" || len(name) > maxProjectNameBytes || strings.IndexByte(name, 0) >= 0 {
		return "", errors.New("project name must be 1–255 bytes")
	}
	return name, nil
}

func normalizeSessionName(name string) (string, error) {
	if strings.TrimSpace(name) == "" || len(name) > 255 || strings.IndexByte(name, 0) >= 0 {
		return "", errors.New("session name must be 1–255 bytes")
	}
	return name, nil
}

func normalizeCommand(command string) (string, error) {
	if strings.TrimSpace(command) == "" || len(command) > maxCommandBytes || strings.IndexByte(command, 0) >= 0 {
		return "", errors.New("start command must be 1–4096 bytes")
	}
	return command, nil
}

func newProjectID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate project id: %w", err)
	}
	return "project_" + hex.EncodeToString(id[:]), nil
}

func scanProject(row interface{ Scan(...any) error }) (Project, error) {
	var p Project
	var lastUsed *time.Time
	err := row.Scan(&p.ID, &p.MachineID, &p.Path, &p.Name, &p.SortOrder, &p.Pinned, &lastUsed, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return p, err
	}
	p.LastUsedAt = lastUsed
	return p, nil
}

// Projects returns projects for one machine in stable presentation order.
func (s *Store) Projects(ctx context.Context, machineID string) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+projectCols+` FROM projects WHERE machine_id = $1 ORDER BY sort_order, name, path, id`, machineID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Project returns one project or ErrNotFound.
func (s *Store) Project(ctx context.Context, id string) (Project, error) {
	p, err := scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// CreateProject inserts a project. A duplicate machine/path returns the
// existing project and leaves its existing display name unchanged.
func (s *Store) CreateProject(ctx context.Context, machineID, projectPath, name string) (Project, error) {
	normalized, err := normalizeProjectPath(projectPath)
	if err != nil {
		return Project{}, err
	}
	name, err = normalizeProjectName(name, normalized)
	if err != nil {
		return Project{}, err
	}
	id, err := newProjectID()
	if err != nil {
		return Project{}, err
	}
	p, err := scanProject(s.db.QueryRowContext(ctx, `
		INSERT INTO projects (id, machine_id, path, name) VALUES ($1, $2, $3, $4)
		ON CONFLICT (machine_id, path) DO UPDATE SET path = excluded.path
		RETURNING `+projectCols, id, machineID, normalized, name))
	if err != nil {
		return Project{}, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

// RenameProject changes only the display name.
func (s *Store) RenameProject(ctx context.Context, id, name string) (Project, error) {
	name, err := normalizeProjectName(name, "")
	if err != nil {
		return Project{}, err
	}
	p, err := scanProject(s.db.QueryRowContext(ctx, `UPDATE projects SET name = $1, updated_at = $2 WHERE id = $3 RETURNING `+projectCols,
		name, s.now(), id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// DeleteProject removes project metadata; associated links and recents cascade.
func (s *Store) DeleteProject(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// SessionLink returns a session's explicit project association or ErrNotFound.
func (s *Store) SessionLink(ctx context.Context, machineID, sessionName string) (SessionLink, error) {
	name, err := normalizeSessionName(sessionName)
	if err != nil {
		return SessionLink{}, err
	}
	var link SessionLink
	err = s.db.QueryRowContext(ctx, `SELECT machine_id, session_name, project_id, created_at FROM session_links WHERE machine_id = $1 AND session_name = $2`, machineID, name).
		Scan(&link.MachineID, &link.SessionName, &link.ProjectID, &link.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return link, ErrNotFound
	}
	return link, err
}

// UpsertSessionLink creates or updates the explicit project association.
func (s *Store) UpsertSessionLink(ctx context.Context, machineID, sessionName, projectID string) error {
	name, err := normalizeSessionName(sessionName)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO session_links (machine_id, session_name, project_id) VALUES ($1, $2, $3)
		ON CONFLICT (machine_id, session_name) DO UPDATE SET project_id = excluded.project_id`, machineID, name, projectID)
	return err
}

// RenameSessionLink changes the key in one SQL statement, so no observer can
// see a gap between the old and new live-session names.
func (s *Store) RenameSessionLink(ctx context.Context, machineID, oldName, newName string) error {
	oldName, err := normalizeSessionName(oldName)
	if err != nil {
		return err
	}
	newName, err = normalizeSessionName(newName)
	if err != nil {
		return err
	}
	if oldName == newName {
		return nil
	}
	res, err := s.db.ExecContext(ctx, `UPDATE session_links SET session_name = $1 WHERE machine_id = $2 AND session_name = $3`, newName, machineID, oldName)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSessionLink removes an ended session's association.
func (s *Store) DeleteSessionLink(ctx context.Context, machineID, sessionName string) error {
	name, err := normalizeSessionName(sessionName)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM session_links WHERE machine_id = $1 AND session_name = $2`, machineID, name)
	return err
}

// RecentCommands returns the most recently used commands first, with a stable
// lexical tie-break for equal timestamps.
func (s *Store) RecentCommands(ctx context.Context, projectID string) ([]RecentCommand, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, project_id, command, last_used_at FROM recent_commands WHERE project_id = $1 ORDER BY last_used_at DESC, command ASC LIMIT $2`, projectID, RecentCommandLimit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []RecentCommand
	for rows.Next() {
		var c RecentCommand
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Command, &c.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RememberRecentCommand stores an exact command and keeps only the newest
// RecentCommandLimit values for its project.
func (s *Store) RememberRecentCommand(ctx context.Context, projectID, command string) error {
	command, err := normalizeCommand(command)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var found string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE id = $1 FOR UPDATE`, projectID).Scan(&found); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO recent_commands (project_id, command, last_used_at) VALUES ($1, $2, $3)
		ON CONFLICT (project_id, command) DO UPDATE SET last_used_at = excluded.last_used_at`, projectID, command, s.now()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM recent_commands WHERE project_id = $1 AND id NOT IN (
		SELECT id FROM recent_commands WHERE project_id = $1 ORDER BY last_used_at DESC, command ASC LIMIT $2
	)`, projectID, RecentCommandLimit); err != nil {
		return err
	}
	return tx.Commit()
}
