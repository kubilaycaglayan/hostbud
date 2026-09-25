package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(context.Background(), dir)
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

	var mode string
	if err := s.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v; want wal", mode, err)
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

	// Back up straight into a fresh data dir, then open that as a store.
	dir := t.TempDir()
	dest := filepath.Join(dir, DBFile)
	if err := s.Backup(ctx, dest); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(ctx, dest); err == nil {
		t.Fatal("backup overwrote an existing file")
	}

	b := openTemp(t, dir)
	defer func() { _ = b.Close() }()
	if v, err := b.UIState(ctx, "k"); err != nil || string(v) != `"v"` {
		t.Fatalf("backup content: %s, %v", v, err)
	}
}
