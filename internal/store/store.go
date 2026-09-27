// Package store is hostbud's PostgreSQL persistence layer with embedded goose
// migrations. It is the only package with SQL.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
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
	Home        string
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
	// SaveCapabilities records a machine's probe results and when it was seen.
	SaveCapabilities(ctx context.Context, id string, c Capabilities, seen time.Time) error
	// UIState returns the stored JSON for key, or ErrNotFound.
	UIState(ctx context.Context, key string) (json.RawMessage, error)
	PutUIState(ctx context.Context, key string, value json.RawMessage) error
	// UIStateForUser and PutUIStateForUser keep one account's UI state
	// (layout, …) apart from every other account's.
	UIStateForUser(ctx context.Context, userID, key string) (json.RawMessage, error)
	PutUIStateForUser(ctx context.Context, userID, key string, value json.RawMessage) error
	Projects(ctx context.Context, machineID string) ([]Project, error)
	Project(ctx context.Context, id string) (Project, error)
	CreateProject(ctx context.Context, machineID, projectPath, name string) (Project, error)
	RenameProject(ctx context.Context, id, name string) (Project, error)
	DeleteProject(ctx context.Context, id string) error
	SessionLink(ctx context.Context, machineID, sessionName string) (SessionLink, error)
	UpsertSessionLink(ctx context.Context, machineID, sessionName, projectID string) error
	RenameSessionLink(ctx context.Context, machineID, oldName, newName string) error
	DeleteSessionLink(ctx context.Context, machineID, sessionName string) error
	PruneSessionLinks(ctx context.Context, machineID string, activeNames []string) error
	RecentCommands(ctx context.Context, projectID string) ([]RecentCommand, error)
	RememberRecentCommand(ctx context.Context, projectID, command string) error
	// Backup writes a consistent copy of the database to dest (must not exist).
	Backup(ctx context.Context, dest string) error
	Close() error
}

// Config contains the connection settings for PostgreSQL.
type Config struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
	SSLMode  string
	// Schema is intended for isolated tests. Production uses the public schema.
	Schema string
}

func (c Config) dsn() string {
	q := url.Values{"sslmode": {c.SSLMode}}
	q.Set("connect_timeout", "5")
	q.Set("statement_timeout", "5000")
	q.Set("lock_timeout", "3000")
	q.Set("idle_in_transaction_session_timeout", "30000")
	if c.Schema != "" {
		q.Set("search_path", c.Schema)
	}
	u := url.URL{Scheme: "postgres", User: url.UserPassword(c.User, c.Password), Host: fmt.Sprintf("%s:%d", c.Host, c.Port), Path: "/" + c.Name, RawQuery: q.Encode()}
	return u.String()
}

var identifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var dumpVersionRE = regexp.MustCompile(`(?i)postgresql\)?\s+([0-9]+)(?:\.[0-9]+)?`)

func quoteIdentifier(name string) (string, error) {
	if !identifierRE.MatchString(name) {
		return "", fmt.Errorf("invalid PostgreSQL identifier %q", name)
	}
	return `"` + name + `"`, nil
}

// Store implements Repository on PostgreSQL.
type Store struct {
	db     *sql.DB
	now    func() time.Time
	dbconf Config
}

var _ Repository = (*Store)(nil)

// Open connects to PostgreSQL and applies pending embedded migrations.
func Open(ctx context.Context, conf Config) (*Store, error) {
	if conf.Host == "" || conf.Port < 1 || conf.Name == "" || conf.User == "" {
		return nil, errors.New("database configuration is incomplete")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", conf.dsn())
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	if err := pingUntilAvailable(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	if conf.Schema != "" {
		ident, err := quoteIdentifier(conf.Schema)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+ident); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("create PostgreSQL schema: %w", err)
		}
		_ = db.Close()
		db, err = sql.Open("pgx", conf.dsn())
		if err != nil {
			return nil, fmt.Errorf("open PostgreSQL schema: %w", err)
		}
		if err := pingUntilAvailable(ctx, db); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("connect to PostgreSQL schema: %w", err)
		}
	}
	configurePool(db)
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }, dbconf: conf}, nil
}

func pingUntilAvailable(ctx context.Context, db *sql.DB) error {
	var last error
	for {
		if err := db.PingContext(ctx); err == nil {
			return nil
		} else {
			last = err
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("PostgreSQL did not become available within 90 seconds: %w (check `docker compose ps hostbud-postgres`)", last)
		case <-timer.C:
		}
	}
}

func configurePool(db *sql.DB) {
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
}

// IsUnavailable reports database errors that should be returned as a
// recoverable service outage rather than an authentication or server error.
func IsUnavailable(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, sql.ErrConnDone) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (strings.HasPrefix(pgErr.Code, "08") || pgErr.Code == "57014" || pgErr.Code == "57P01" || pgErr.Code == "57P03") {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func migrate(ctx context.Context, db *sql.DB) error {
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	// A PostgreSQL advisory lock serializes migrations when several app
	// instances start against the same database.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithSessionLocker(locker))
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

// Ping checks database reachability using the caller's deadline.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

const machineCols = `id, source, ssh_alias, label, active, sort_order, hidden,
	os, home, tmux_version, tmux_missing, last_seen_at, created_at, updated_at`

func scanMachine(row interface{ Scan(...any) error }) (Machine, error) {
	var m Machine
	var lastSeen sql.NullString
	var created, updated string
	err := row.Scan(&m.ID, &m.Source, &m.SSHAlias, &m.Label, &m.Active, &m.SortOrder, &m.Hidden,
		&m.OS, &m.Home, &m.TmuxVersion, &m.TmuxMissing, &lastSeen, &created, &updated)
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
	m, err := scanMachine(s.db.QueryRowContext(ctx, `SELECT `+machineCols+` FROM machines WHERE id = $1`, id))
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
		VALUES ($1, 'host', $2, $3, TRUE, $4, $5)
		ON CONFLICT (id) DO UPDATE SET label = excluded.label, updated_at = excluded.updated_at
		WHERE machines.label <> excluded.label`,
		HostMachineID, HostSSHAlias, label, now, now)
	if err != nil {
		return Machine{}, fmt.Errorf("seed host machine: %w", err)
	}
	return s.Machine(ctx, HostMachineID)
}

// Capabilities are a machine's probe results.
type Capabilities struct {
	OS          string
	Home        string
	TmuxVersion string
	TmuxMissing bool
}

// SaveCapabilities records probe results on the machine row.
func (s *Store) SaveCapabilities(ctx context.Context, id string, c Capabilities, seen time.Time) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE machines SET os = $1, home = $2, tmux_version = $3, tmux_missing = $4,
			last_seen_at = $5, updated_at = $6
		WHERE id = $7`,
		c.OS, c.Home, c.TmuxVersion, c.TmuxMissing, formatTime(seen), formatTime(s.now()), id)
	if err != nil {
		return fmt.Errorf("save capabilities: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// UIState returns the stored JSON value for key, or ErrNotFound.
func (s *Store) UIState(ctx context.Context, key string) (json.RawMessage, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value_json FROM ui_state WHERE key = $1`, key).Scan(&v)
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
		INSERT INTO ui_state (key, value_json, updated_at) VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		key, string(value), formatTime(s.now()))
	return err
}

// userUIKey namespaces an account's UI state key in ui_state.
func userUIKey(userID, key string) string { return "user:" + userID + ":" + key }

// UIStateForUser returns the account's stored JSON for key, or ErrNotFound.
func (s *Store) UIStateForUser(ctx context.Context, userID, key string) (json.RawMessage, error) {
	return s.UIState(ctx, userUIKey(userID, key))
}

// PutUIStateForUser stores a JSON value under the account's key.
func (s *Store) PutUIStateForUser(ctx context.Context, userID, key string, value json.RawMessage) error {
	return s.PutUIState(ctx, userUIKey(userID, key), value)
}

// Backup writes a consistent PostgreSQL custom-format dump to dest. The
// password is passed through the child process environment, never argv.
func (s *Store) Backup(ctx context.Context, dest string) error {
	if strings.TrimSpace(dest) == "" {
		return errors.New("backup: destination is required")
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("backup: %s already exists", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("backup: check destination: %w", err)
	}
	if err := s.checkDumpVersion(ctx); err != nil {
		return err
	}
	args := s.backupArgs(dest)
	cmd := exec.CommandContext(ctx, "pg_dump", args...) //nolint:gosec // fixed binary; args are separate argv entries from operator config, no shell
	cmd.Env = backupEnv(os.Environ(), s.dbconf.Password, s.dbconf.SSLMode)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("backup: %w", errors.Join(err, errors.New(strings.TrimSpace(string(output)))))
	}
	if err := os.Chmod(dest, 0o600); err != nil {
		return fmt.Errorf("backup: restrict dump permissions: %w", err)
	}
	return nil
}

func (s *Store) backupArgs(dest string) []string {
	args := []string{
		"--format=custom", "--no-password", "--file", dest,
		"--host", s.dbconf.Host, "--port", fmt.Sprint(s.dbconf.Port),
		"--username", s.dbconf.User,
	}
	if s.dbconf.Schema != "" {
		args = append(args, "--schema", s.dbconf.Schema)
	}
	args = append(args, s.dbconf.Name)
	return args
}

func (s *Store) checkDumpVersion(ctx context.Context) error {
	var serverVersion int
	if err := s.db.QueryRowContext(ctx, `SHOW server_version_num`).Scan(&serverVersion); err != nil {
		return fmt.Errorf("check PostgreSQL server version: %w", err)
	}
	output, err := exec.CommandContext(ctx, "pg_dump", "--version").Output() //nolint:gosec // fixed binary and fixed argument
	if err != nil {
		return fmt.Errorf("check pg_dump version: %w", err)
	}
	match := dumpVersionRE.FindStringSubmatch(string(output))
	if len(match) != 2 {
		return fmt.Errorf("check pg_dump version: unrecognized output %q", strings.TrimSpace(string(output)))
	}
	clientMajor, err := strconv.Atoi(match[1])
	if err != nil {
		return fmt.Errorf("check pg_dump version: %w", err)
	}
	serverMajor := serverVersion / 10000
	return validateDumpMajor(clientMajor, serverMajor)
}

func validateDumpMajor(clientMajor, serverMajor int) error {
	if clientMajor < serverMajor {
		return fmt.Errorf("pg_dump major version %d is older than PostgreSQL server major version %d; install postgresql-client-%d or newer", clientMajor, serverMajor, serverMajor)
	}
	return nil
}

func backupEnv(base []string, password, sslMode string) []string {
	env := make([]string, 0, len(base)+2)
	for _, value := range base {
		if !strings.HasPrefix(value, "PGPASSWORD=") && !strings.HasPrefix(value, "PGSSLMODE=") {
			env = append(env, value)
		}
	}
	return append(env, "PGPASSWORD="+password, "PGSSLMODE="+sslMode)
}
