package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
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
	if err := s.Backup(ctx, dest); err == nil {
		t.Fatal("backup overwrote an existing file")
	}
	if out, err := exec.CommandContext(ctx, "pg_restore", "--list", dest).CombinedOutput(); err != nil {
		t.Fatalf("restore smoke test: %v: %s", err, out)
	}
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
