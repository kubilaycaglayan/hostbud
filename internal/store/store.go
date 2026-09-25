// Package store is hostbud's persistence layer: SQLite (modernc.org/sqlite,
// pure Go) with embedded goose migrations. It is the only package with SQL.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql
var migrations embed.FS

// HostMachineID is the id of the built-in host machine row.
const HostMachineID = "host"

// HostSSHAlias is the ssh config alias of the host machine (see sshx).
const HostSSHAlias = "hostbud-host"

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// Machine is a row of the machines table.
type Machine struct {
	ID          string
	Source      string // host | sshconfig | custom
	SSHAlias    string
	Label       string
	Active      bool
	SortOrder   int
	Hidden      bool
	OS          string
	TmuxVersion string
	TmuxMissing bool
	LastSeenAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Repository is the data-access interface the rest of hostbud depends on.
type Repository interface {
	Machines(ctx context.Context) ([]Machine, error)
	Machine(ctx context.Context, id string) (Machine, error)
	// EnsureHostMachine creates the built-in host row, or updates its label.
	EnsureHostMachine(ctx context.Context, label string) (Machine, error)
	// UIState returns the stored JSON for key, or ErrNotFound.
	UIState(ctx context.Context, key string) (json.RawMessage, error)
	PutUIState(ctx context.Context, key string, value json.RawMessage) error
	// Backup writes a consistent copy of the database to dest (must not exist).
	Backup(ctx context.Context, dest string) error
	Close() error
}

// Store implements Repository on SQLite.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

var _ Repository = (*Store)(nil)

// DBFile is the database file name inside the data dir.
const DBFile = "hostbud.db"

// Open opens (creating if needed) ${dataDir}/hostbud.db and applies pending
// migrations.
func Open(ctx context.Context, dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	dsn := "file:" + filepath.Join(dataDir, DBFile) +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open database %s: %w", filepath.Join(dataDir, DBFile), err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, db, fsys)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

const machineCols = `id, source, ssh_alias, label, active, sort_order, hidden,
	os, tmux_version, tmux_missing, last_seen_at, created_at, updated_at`

func scanMachine(row interface{ Scan(...any) error }) (Machine, error) {
	var m Machine
	var lastSeen sql.NullString
	var created, updated string
	err := row.Scan(&m.ID, &m.Source, &m.SSHAlias, &m.Label, &m.Active, &m.SortOrder, &m.Hidden,
		&m.OS, &m.TmuxVersion, &m.TmuxMissing, &lastSeen, &created, &updated)
	if err != nil {
		return m, err
	}
	if lastSeen.Valid {
		t, err := parseTime(lastSeen.String)
		if err != nil {
			return m, err
		}
		m.LastSeenAt = &t
	}
	if m.CreatedAt, err = parseTime(created); err != nil {
		return m, err
	}
	m.UpdatedAt, err = parseTime(updated)
	return m, err
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }

// Machines lists machines in display order.
func (s *Store) Machines(ctx context.Context) ([]Machine, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+machineCols+` FROM machines ORDER BY sort_order, label, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Machine
	for rows.Next() {
		m, err := scanMachine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Machine returns one machine or ErrNotFound.
func (s *Store) Machine(ctx context.Context, id string) (Machine, error) {
	m, err := scanMachine(s.db.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// EnsureHostMachine creates the built-in host row (active) or refreshes its
// label from config. Safe to call on every startup.
func (s *Store) EnsureHostMachine(ctx context.Context, label string) (Machine, error) {
	now := formatTime(s.now())
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO machines (id, source, ssh_alias, label, active, created_at, updated_at)
		VALUES (?, 'host', ?, ?, TRUE, ?, ?)
		ON CONFLICT (id) DO UPDATE SET label = excluded.label, updated_at = excluded.updated_at
		WHERE machines.label <> excluded.label`,
		HostMachineID, HostSSHAlias, label, now, now)
	if err != nil {
		return Machine{}, fmt.Errorf("seed host machine: %w", err)
	}
	return s.Machine(ctx, HostMachineID)
}

// UIState returns the stored JSON value for key, or ErrNotFound.
func (s *Store) UIState(ctx context.Context, key string) (json.RawMessage, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value_json FROM ui_state WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return json.RawMessage(v), err
}

// PutUIState stores a JSON value under key.
func (s *Store) PutUIState(ctx context.Context, key string, value json.RawMessage) error {
	if !json.Valid(value) {
		return errors.New("ui state: value is not valid JSON")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ui_state (key, value_json, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		key, string(value), formatTime(s.now()))
	return err
}

// Backup writes a consistent, compacted copy of the database to dest.
func (s *Store) Backup(ctx context.Context, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("backup: %s already exists", dest)
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}
