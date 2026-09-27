package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

// testConfig points at an isolated schema (named after dir) in the test DB.
func testConfig(dir string) Config {
	host := os.Getenv("HOSTBUD_TEST_DB_HOST")
	if host == "" {
		host = "hostbud-test-postgres"
	}
	password := os.Getenv("HOSTBUD_TEST_DB_PASSWORD")
	if password == "" {
		password = "hostbud-test-password" //nolint:gosec // throwaway test database (scripts/test-sshd.sh), not a credential
	}
	schema := fmt.Sprintf("test_%x", sha256.Sum256([]byte(dir)))[:20]
	return Config{
		Host: host, Port: 5432, Name: "hostbud_test", User: "hostbud_test",
		Password: password, SSLMode: "disable", Schema: schema,
	}
}

func openTemp(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(context.Background(), testConfig(dir))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestOpenMigratesAndSeedsHost(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()

	m, err := s.EnsureHostMachine(ctx, "Host machine")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != HostMachineID || m.Source != "host" || m.SSHAlias != HostSSHAlias || !m.Active || m.Label != "Host machine" {
		t.Fatalf("unexpected host row: %+v", m)
	}
	if m.CreatedAt.IsZero() || m.LastSeenAt != nil {
		t.Fatalf("timestamps: %+v", m)
	}

	var schema string
	if err := s.db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil || schema == "public" {
		t.Fatalf("current_schema = %q, %v; want isolated test schema", schema, err)
	}
}

func TestDatabaseLimitsAndPool(t *testing.T) {
	conf := testConfig(t.TempDir())
	parsed, err := url.Parse(conf.dsn())
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"connect_timeout": "5", "statement_timeout": "5000", "lock_timeout": "3000", "idle_in_transaction_session_timeout": "30000",
	} {
		if got := parsed.Query().Get(key); got != want {
			t.Errorf("DSN %s=%q, want %q", key, got, want)
		}
	}
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()
	if got := s.db.Stats().MaxOpenConnections; got != 20 {
		t.Fatalf("MaxOpenConnections=%d, want 20", got)
	}
	ctx := context.Background()
	var statement, lock, idle string
	for _, item := range []struct {
		name string
		dest *string
	}{{"statement_timeout", &statement}, {"lock_timeout", &lock}, {"idle_in_transaction_session_timeout", &idle}} {
		if err := s.db.QueryRowContext(ctx, "SHOW "+item.name).Scan(item.dest); err != nil {
			t.Fatal(err)
		}
	}
	if statement != "5s" || lock != "3s" || idle != "30s" {
		t.Fatalf("session timeouts: statement=%s lock=%s idle=%s", statement, lock, idle)
	}
	conns := make([]*sql.Conn, 0, 6)
	for i := 0; i < 6; i++ {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
	}
	for _, conn := range conns {
		_ = conn.Close()
	}
	if got := s.db.Stats().Idle; got > 5 {
		t.Fatalf("idle pool connections=%d, want at most 5", got)
	}
}

func TestPostgresStatementAndLockTimeouts(t *testing.T) {
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	start := time.Now()
	if _, err := s.db.ExecContext(ctx, `SELECT pg_sleep(6)`); err == nil {
		t.Fatal("pg_sleep passed statement_timeout")
	}
	if d := time.Since(start); d < 4*time.Second || d > 7*time.Second {
		t.Fatalf("statement timeout after %v, want about 5s", d)
	}
	key := "lock-timeout-integration"
	if _, err := s.db.ExecContext(ctx, `INSERT INTO login_rate_limits(scope_key) VALUES ($1) ON CONFLICT DO NOTHING`, key); err != nil {
		t.Fatal(err)
	}
	one, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = one.Rollback() }()
	var locked string
	if err := one.QueryRowContext(ctx, `SELECT scope_key FROM login_rate_limits WHERE scope_key=$1 FOR UPDATE`, key).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	two, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = two.Rollback() }()
	start = time.Now()
	_, err = two.ExecContext(ctx, `UPDATE login_rate_limits SET failures=failures+1 WHERE scope_key=$1`, key)
	if err == nil {
		t.Fatal("contended update passed lock_timeout")
	}
	if d := time.Since(start); d < 2*time.Second || d > 5*time.Second {
		t.Fatalf("lock timeout after %v, want about 3s", d)
	}
}

func TestIsUnavailable(t *testing.T) {
	if !IsUnavailable(context.DeadlineExceeded) {
		t.Fatal("context deadline was not unavailable")
	}
	if IsUnavailable(ErrNotFound) {
		t.Fatal("not found classified as unavailable")
	}
}

func TestOpenStartupHonorsEarlierCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	conf := Config{Host: "127.0.0.1", Port: 1, Name: "hostbud_test", User: "hostbud_test", Password: "placeholder", SSLMode: "disable"}
	start := time.Now()
	_, err := Open(ctx, conf)
	if err == nil {
		t.Fatal("Open succeeded without PostgreSQL")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("Open waited %v past caller deadline", time.Since(start))
	}
}

func TestSeedIsIdempotentAndUpdatesLabel(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()

	first, err := s.EnsureHostMachine(ctx, "A")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureHostMachine(ctx, "A"); err != nil {
		t.Fatal(err)
	}
	second, err := s.EnsureHostMachine(ctx, "B")
	if err != nil {
		t.Fatal(err)
	}
	ms, err := s.Machines(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 {
		t.Fatalf("want 1 machine, got %d", len(ms))
	}
	if second.Label != "B" || !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("label update: first %+v second %+v", first, second)
	}
}

func TestDataSurvivesReopenAndMigrationRerunIsNoop(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	s := openTemp(t, dir)
	if _, err := s.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutUIState(ctx, "layout", json.RawMessage(`{"tabs":[1,2]}`)); err != nil {
		t.Fatal(err)
	}
	before := migrationRows(t, s.db)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s = openTemp(t, dir)
	defer func() { _ = s.Close() }()
	if after := migrationRows(t, s.db); after != before {
		t.Fatalf("goose version rows changed on re-open: %d → %d", before, after)
	}
	v, err := s.UIState(ctx, "layout")
	if err != nil || string(v) != `{"tabs":[1,2]}` {
		t.Fatalf("ui state after reopen = %s, %v", v, err)
	}
	if _, err := s.Machine(ctx, HostMachineID); err != nil {
		t.Fatalf("host row after reopen: %v", err)
	}
}

func migrationRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM goose_db_version`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestUIState(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()

	if _, err := s.UIState(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing key: %v", err)
	}
	if err := s.PutUIState(ctx, "k", json.RawMessage(`not json`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if err := s.PutUIState(ctx, "k", json.RawMessage(`1`)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutUIState(ctx, "k", json.RawMessage(`2`)); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.UIState(ctx, "k"); string(v) != "2" {
		t.Fatalf("overwrite: %s", v)
	}
}

func TestUIStateForUser(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()
	clock := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s.now = func() time.Time { return clock }

	if err := s.PutUIStateForUser(ctx, "user-a", "layout", json.RawMessage(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutUIStateForUser(ctx, "user-b", "layout", json.RawMessage(`{"b":2}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutUIStateForUser(ctx, "user-a", "tree", json.RawMessage(`{"version":1,"projects":["project-a"],"sessions":{}}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutUIStateForUser(ctx, "user-b", "tree", json.RawMessage(`{"version":2,"projects":["project-b"],"sessions":{"__other__":["shell"]},"pinned":["project-b"],"hidden":{"projects":[],"sessions":[]},"collapsed":[],"expanded":[],"showHidden":false}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutUIStateForUser(ctx, "user-a", "theme", json.RawMessage(`{"version":1,"mode":"light"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UIStateForUser(ctx, "user-c", "layout"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an account without state: %v", err)
	}
	if v, err := s.UIStateForUser(ctx, "user-a", "theme"); err != nil || string(v) != `{"version":1,"mode":"light"}` {
		t.Fatalf("theme round-trip: %s, %v", v, err)
	}
	if _, err := s.UIStateForUser(ctx, "user-b", "theme"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("theme leaked across accounts: %v", err)
	}
	for user, want := range map[string]string{"user-a": `{"a":1}`, "user-b": `{"b":2}`} {
		if v, err := s.UIStateForUser(ctx, user, "layout"); err != nil || string(v) != want {
			t.Fatalf("%s: %s, %v", user, v, err)
		}
	}
	for user, want := range map[string]string{
		"user-a": `{"version":1,"projects":["project-a"],"sessions":{}}`,
		"user-b": `{"version":2,"projects":["project-b"],"sessions":{"__other__":["shell"]},"pinned":["project-b"],"hidden":{"projects":[],"sessions":[]},"collapsed":[],"expanded":[],"showHidden":false}`,
	} {
		if v, err := s.UIStateForUser(ctx, user, "tree"); err != nil || string(v) != want {
			t.Fatalf("%s tree order: %s, %v", user, v, err)
		}
	}
	// Namespaced in the shared table: no bare "layout" row.
	if _, err := s.UIState(ctx, "layout"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("un-namespaced key: %v", err)
	}

	updatedAt := func() string {
		var v string
		if err := s.db.QueryRowContext(ctx, `SELECT updated_at FROM ui_state WHERE key = 'user:user-a:layout'`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	before := updatedAt()
	clock = clock.Add(time.Hour)
	if err := s.PutUIStateForUser(ctx, "user-a", "layout", json.RawMessage(`{"a":3}`)); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.UIStateForUser(ctx, "user-a", "layout"); string(v) != `{"a":3}` {
		t.Fatalf("overwrite: %s", v)
	}
	if after := updatedAt(); after == before {
		t.Fatalf("updated_at unchanged by an overwrite (%s)", after)
	}
}

func TestSaveCapabilities(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()
	if _, err := s.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	seen := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := Capabilities{OS: "Linux", Home: "/home/dev", TmuxVersion: "3.4"}
	if err := s.SaveCapabilities(ctx, HostMachineID, c, seen); err != nil {
		t.Fatal(err)
	}
	m, err := s.Machine(ctx, HostMachineID)
	if err != nil {
		t.Fatal(err)
	}
	if m.OS != "Linux" || m.Home != "/home/dev" || m.TmuxVersion != "3.4" || m.TmuxMissing ||
		m.LastSeenAt == nil || !m.LastSeenAt.Equal(seen) {
		t.Fatalf("machine after save: %+v", m)
	}
	if err := s.SaveCapabilities(ctx, HostMachineID, Capabilities{OS: "Linux", TmuxMissing: true}, seen); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.Machine(ctx, HostMachineID); !m.TmuxMissing || m.TmuxVersion != "" {
		t.Fatalf("tmux_missing not saved: %+v", m)
	}
	if err := s.SaveCapabilities(ctx, "server-a", c, seen); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown machine: %v", err)
	}
}

func TestMachineNotFound(t *testing.T) {
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()
	if _, err := s.Machine(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestBackup(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()
	if err := s.PutUIState(ctx, "k", json.RawMessage(`"v"`)); err != nil {
		t.Fatal(err)
	}

	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump is not available in this test environment")
	}
	dest := filepath.Join(t.TempDir(), "hostbud.dump")
	if err := s.Backup(ctx, dest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode=%v, want 0600", info.Mode().Perm())
	}
	if err := s.Backup(ctx, dest); err == nil {
		t.Fatal("backup overwrote an existing file")
	}
	if out, err := exec.CommandContext(ctx, "pg_restore", "--list", dest).CombinedOutput(); err != nil {
		t.Fatalf("restore smoke test: %v: %s", err, out)
	}
}

func TestBackupCommandAndVersion(t *testing.T) {
	s := &Store{dbconf: Config{Host: "server-a", Port: 5432, Name: "hostbud", User: "hostbud_test", Password: "test-password", SSLMode: "disable", Schema: "test_hostbud"}}
	args := strings.Join(s.backupArgs("/tmp/hostbud.dump"), " ")
	if strings.Contains(args, s.dbconf.Password) || !strings.Contains(args, "--format=custom") || !strings.Contains(args, "--schema test_hostbud") {
		t.Fatalf("backup argv=%s", args)
	}
	env := backupEnv([]string{"PATH=/bin", "PGPASSWORD=old", "PGSSLMODE=require"}, s.dbconf.Password, s.dbconf.SSLMode)
	if slices.Contains(env, "PGPASSWORD=old") || !slices.Contains(env, "PGPASSWORD=test-password") || !slices.Contains(env, "PGSSLMODE=disable") {
		t.Fatalf("backup environment=%v", env)
	}
	for _, tc := range []struct {
		client, server int
		wantErr        bool
	}{{14, 15, true}, {15, 15, false}, {17, 15, false}} {
		err := validateDumpMajor(tc.client, tc.server)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateDumpMajor(%d,%d)=%v", tc.client, tc.server, err)
		}
	}
	match := dumpVersionRE.FindStringSubmatch("pg_dump (PostgreSQL) 15.13 (Debian 15.13-0+deb12u1)")
	if len(match) != 2 || match[1] != "15" {
		t.Fatalf("version parse=%v", match)
	}
}

func TestValidateRestoreCheckList(t *testing.T) {
	for _, tc := range []struct {
		name string
		list string
		want bool
	}{
		{name: "hostbud table", list: `; 2121 2606 TABLE public goose_db_version hostbud`, want: true},
		{name: "other table", list: `; 2121 2606 TABLE public users hostbud`},
		{name: "substring is not enough", list: `; 2121 2606 TABLE public not_goose_db_version hostbud`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRestoreCheckList([]byte(tc.list))
			if (err == nil) != tc.want {
				t.Fatalf("validateRestoreCheckList()=%v, want success=%t", err, tc.want)
			}
		})
	}
}

func TestBackupRestoreRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump is not available in this test environment")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	cfg := testConfig(dir)
	s, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject(ctx, HostMachineID, "/home/dev/before", "before"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutUIState(ctx, "before", json.RawMessage(`"saved"`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO users(id,email,email_normalized,password_hash) VALUES ('backup-user','backup@example.com','backup@example.com','hash')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO email_allowlist(email_normalized) VALUES ('backup@example.com')`); err != nil {
		t.Fatal(err)
	}
	dump := filepath.Join(dir, "hostbud.dump")
	if err := s.Backup(ctx, dump); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject(ctx, HostMachineID, "/home/dev/after", "after"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutUIState(ctx, "after", json.RawMessage(`"mutation"`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO users(id,email,email_normalized,password_hash) VALUES ('mutated-user','mutated@example.com','mutated@example.com','hash')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO email_allowlist(email_normalized) VALUES ('mutated@example.com')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	args := []string{"--clean", "--if-exists", "--single-transaction", "--no-owner", "--exit-on-error", "--no-password", "--host", cfg.Host, "--port", fmt.Sprint(cfg.Port), "--username", cfg.User, "--dbname", cfg.Name, "--schema", cfg.Schema, dump}
	cmd := exec.CommandContext(ctx, "pg_restore", args...) //nolint:gosec // fixed test tool and separate arguments against the test database only
	cmd.Env = backupEnv(os.Environ(), cfg.Password, cfg.SSLMode)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pg_restore: %v: %s", err, out)
	}
	s, err = Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if got, err := s.UIState(ctx, "before"); err != nil || string(got) != `"saved"` {
		t.Fatalf("saved ui state=%s err=%v", got, err)
	}
	if _, err := s.UIState(ctx, "after"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mutated ui state survived restore: %v", err)
	}
	projects, err := s.Projects(ctx, HostMachineID)
	if err != nil || len(projects) != 1 || projects[0].Name != "before" {
		t.Fatalf("projects after restore=%+v err=%v", projects, err)
	}
	var users, allowlist int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM email_allowlist`).Scan(&allowlist); err != nil {
		t.Fatal(err)
	}
	if users != 1 || allowlist != 1 {
		t.Fatalf("restored users=%d allowlist=%d", users, allowlist)
	}
}

func TestOlderBackupRunsNewMigrationsOnOpen(t *testing.T) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump is not available in this test environment")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cfg := testConfig(t.TempDir())
	db, err := sql.Open("pgx", cfg.dsn())
	if err != nil {
		t.Fatal(err)
	}
	ident, err := quoteIdentifier(cfg.Schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+ident); err != nil {
		t.Fatal(err)
	}
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	old := &Store{db: db, now: time.Now, dbconf: cfg}
	dump := filepath.Join(t.TempDir(), "old.dump")
	if err := old.Backup(ctx, dump); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	admin := cfg
	admin.Schema = ""
	adminDB, err := sql.Open("pgx", admin.dsn())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adminDB.ExecContext(ctx, `DROP SCHEMA `+ident+` CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := adminDB.ExecContext(ctx, `CREATE SCHEMA `+ident); err != nil {
		t.Fatal(err)
	}
	if err := adminDB.Close(); err != nil {
		t.Fatal(err)
	}
	args := []string{"--no-password", "--no-owner", "--exit-on-error", "--host", cfg.Host, "--port", fmt.Sprint(cfg.Port), "--username", cfg.User, "--dbname", cfg.Name, "--schema", cfg.Schema, dump}
	cmd := exec.CommandContext(ctx, "pg_restore", args...) //nolint:gosec // fixed test tool against the isolated integration database
	cmd.Env = backupEnv(os.Environ(), cfg.Password, cfg.SSLMode)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restore old dump: %v: %s", err, out)
	}
	current, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = current.Close() }()
	var version int
	if err := current.db.QueryRowContext(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if latest, err := latestMigrationVersion(); err != nil || int64(version) != latest {
		t.Fatalf("migrated version=%d, want the latest (%d, %v)", version, latest, err)
	}
	var exists bool
	if err := current.db.QueryRowContext(ctx, `SELECT to_regclass('projects') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("latest projects migration did not run")
	}
}

func TestRestoreCheckCleansTemporaryDatabaseOnSuccessAndSanityFailure(t *testing.T) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump is not available in this test environment")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	cfg := testConfig(t.TempDir())
	s, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(t.TempDir(), "good.dump")
	if err := s.Backup(ctx, good); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	checkNoTemporaryDBs := func() {
		t.Helper()
		adminCfg := cfg
		adminCfg.Schema = ""
		admin, err := sql.Open("pgx", adminCfg.dsn())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = admin.Close() }()
		var count int
		if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM pg_database WHERE datname LIKE 'hostbud_restore_check_%'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("restore-check left %d temporary databases", count)
		}
	}
	if _, err := RestoreCheck(ctx, cfg, good); err != nil {
		t.Fatalf("RestoreCheck good dump: %v", err)
	}
	checkNoTemporaryDBs()

	bad := filepath.Join(t.TempDir(), "missing-users.dump")
	args := []string{"--format=custom", "--no-password", "--host", cfg.Host, "--port", fmt.Sprint(cfg.Port), "--username", cfg.User, "--dbname", cfg.Name, "--schema", cfg.Schema, "--exclude-table=" + cfg.Schema + ".users", "--exclude-table=" + cfg.Schema + ".auth_sessions", "--file", bad}
	cmd := exec.CommandContext(ctx, "pg_dump", args...) //nolint:gosec // fixed test tool and separate arguments against isolated integration database
	cmd.Env = backupEnv(os.Environ(), cfg.Password, cfg.SSLMode)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pg_dump incomplete archive: %v: %s", err, output)
	}
	if _, err := RestoreCheck(ctx, cfg, bad); err == nil || !strings.Contains(err.Error(), "count users") {
		t.Fatalf("RestoreCheck incomplete dump error=%v, want users sanity failure", err)
	}
	checkNoTemporaryDBs()
}

func TestConcurrentOpenMigratesOnce(t *testing.T) {
	cfg := testConfig(t.TempDir())
	// Create the (empty) schema first: concurrent CREATE SCHEMA can race in
	// PostgreSQL itself; the migrations are what this test is about.
	plain := cfg
	plain.Schema = ""
	db, err := sql.Open("pgx", plain.dsn())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `CREATE SCHEMA `+cfg.Schema); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			s, err := Open(context.Background(), cfg)
			if err == nil {
				err = s.Close()
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Open: %v", err)
		}
	}
}
