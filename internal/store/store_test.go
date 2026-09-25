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
	"testing"
)

func openTemp(t *testing.T, dir string) *Store {
	t.Helper()
	host := os.Getenv("HOSTBUD_TEST_DB_HOST")
	if host == "" {
		host = "hostbud-test-postgres"
	}
	password := os.Getenv("HOSTBUD_TEST_DB_PASSWORD")
	if password == "" {
		password = "hostbud-test-password" //nolint:gosec // throwaway test database (scripts/test-sshd.sh), not a credential
	}
	schema := fmt.Sprintf("test_%x", sha256.Sum256([]byte(dir)))[:20]
	s, err := Open(context.Background(), Config{
		Host: host, Port: 5432, Name: "hostbud_test", User: "hostbud_test",
		Password: password, SSLMode: "disable", Schema: schema,
	})
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
