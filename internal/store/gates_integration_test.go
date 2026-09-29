//go:build integration

package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
)

// V2-M4 T1: a populated V2-M3 database (migrations 0001–0008) keeps every
// row when 0009 applies, existing items have no gates, every row passes the
// widened CHECKs, and applying again changes nothing. The migration adds
// columns and replaces two CHECKs; nothing else.
func TestIntegrationCompletionGatesMigrationKeepsV2M3Data(t *testing.T) {
	ctx := context.Background()
	raw, err := migrations.ReadFile("migrations/0009_completion_gates.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, _, _ := strings.Cut(string(raw), "-- +goose Down")
	var code []string
	for _, line := range strings.Split(up, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			code = append(code, line)
		}
	}
	upper := strings.ToUpper(strings.Join(code, "\n"))
	// The only DROP allowed is DROP CONSTRAINT, each followed by the ADD of
	// the same constraint in the same statement.
	drops := regexp.MustCompile(`DROP\s+CONSTRAINT\s+(\w+),\s*ADD\s+CONSTRAINT\s+(\w+)\s+CHECK`).FindAllStringSubmatch(upper, -1)
	if len(drops) != 2 || strings.Count(upper, "DROP") != 2 {
		t.Errorf("0009 must replace exactly two CHECKs with DROP CONSTRAINT … ADD CONSTRAINT; found %d of %d DROPs", len(drops), strings.Count(upper, "DROP"))
	}
	for _, d := range drops {
		if d[1] != d[2] {
			t.Errorf("0009 drops %s but adds %s", d[1], d[2])
		}
	}
	for _, forbidden := range []string{"UPDATE ", "DELETE", "RENAME", "TRUNCATE", "ALTER COLUMN", "DROP TABLE", "DROP COLUMN"} {
		if strings.Contains(upper, forbidden) {
			t.Errorf("0009 contains %q; the migration may only add and widen", forbidden)
		}
	}

	cfg := testConfig(t.TempDir())
	plain := cfg
	plain.Schema = ""
	admin, err := sql.Open("pgx", plain.dsn())
	if err != nil {
		t.Fatal(err)
	}
	quoted, err := quoteIdentifier(cfg.Schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP SCHEMA `+quoted+` CASCADE`)
		_ = admin.Close()
	})
	db, err := sql.Open("pgx", cfg.dsn())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	v2m3 := fstest.MapFS{}
	for _, name := range []string{"0001_init.sql", "0002_machine_home.sql", "0003_auth.sql", "0004_projects.sql", "0005_queues.sql", "0006_parallel_queues.sql", "0007_parallel_queues_setting.sql", "0008_notifications.sql"} {
		b, err := migrations.ReadFile(filepath.ToSlash(filepath.Join("migrations", name)))
		if err != nil {
			t.Fatal(err)
		}
		v2m3[name] = &fstest.MapFile{Data: b}
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, v2m3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO machines (id, source, ssh_alias, label, active, home, os, tmux_version, tmux_missing, created_at, updated_at) VALUES ('host', 'host', 'hostbud-host', 'Host machine', TRUE, '/home/dev', 'Linux', 'tmux 3.4', FALSE, now(), now())`,
		`INSERT INTO users (id, email, email_normalized, password_hash) VALUES ('user_a', 'a@example.com', 'a@example.com', 'hash')`,
		`INSERT INTO projects (id, machine_id, path, name, sort_order, pinned) VALUES ('project_a', 'host', '/home/dev/app', 'app', 1, TRUE)`,
		`INSERT INTO machine_capacity (machine_id, max_concurrent_runs, parallel_queues) VALUES ('host', 2, TRUE)`,
		`INSERT INTO queues (id, machine_id, project_id, name, status, waiting_since) VALUES ('queue_a', 'host', 'project_a', 'Queue', 'running', now())`,
		`INSERT INTO queue_items (id, queue_id, machine_id, position, agent, flags, instruction, status) VALUES
			('item_a', 'queue_a', 'host', 1, 'claude', '--model x', '/goal one', 'done'),
			('item_b', 'queue_a', 'host', 2, 'codex', '', '/goal two', 'needs_attention'),
			('item_c', 'queue_a', 'host', 3, 'claude', '', '/goal three', 'running'),
			('item_d', 'queue_a', 'host', 4, 'claude', '', '/goal four', 'queued'),
			('item_e', 'queue_a', 'host', 5, 'claude', '', '/goal five', 'skipped')`,
		`INSERT INTO runs (id, item_id, machine_id, session_name, agent_session_id, token_hash, status, started_at, ended_at) VALUES
			('01ARZ3NDEKTSV4RRFFQ69G5FA1', 'item_a', 'host', 'app-q1', 'session-1', decode('` + strings.Repeat("ab", 32) + `', 'hex'), 'achieved', now(), now()),
			('01ARZ3NDEKTSV4RRFFQ69G5FA2', 'item_b', 'host', 'app-q2', 'thread-2', decode('` + strings.Repeat("cd", 32) + `', 'hex'), 'exited', now(), now()),
			('01ARZ3NDEKTSV4RRFFQ69G5FA3', 'item_c', 'host', 'app-q3', 'session-3', decode('` + strings.Repeat("ef", 32) + `', 'hex'), 'running', now(), NULL)`,
		`INSERT INTO run_events (run_id, machine_id, source, kind, payload_json) VALUES
			('01ARZ3NDEKTSV4RRFFQ69G5FA1', 'host', 'hook', 'session_start', '{"session_id":"session-1"}'),
			('01ARZ3NDEKTSV4RRFFQ69G5FA2', 'host', 'poller', 'exited', '{}'),
			('01ARZ3NDEKTSV4RRFFQ69G5FA2', 'host', 'timer', 'stale', '{}'),
			('01ARZ3NDEKTSV4RRFFQ69G5FA2', 'host', 'user', 'cancelled', '{}')`,
		`INSERT INTO notification_prefs (user_id, enabled) VALUES ('user_a', TRUE)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	tables := map[string]string{
		"machines": "*", "users": "*", "projects": "*", "machine_capacity": "*", "queues": v2m5QueueCols,
		"queue_items": v2m1ItemCols, "runs": "*", "run_events": "*", "notification_prefs": "*",
	}
	before := columnChecksums(t, db, tables)

	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	after := columnChecksums(t, db, tables)
	for table := range tables {
		if before[table] != after[table] {
			t.Errorf("%s changed by the completion-gates migration", table)
		}
	}
	var version int64
	if err := db.QueryRowContext(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version); err != nil || version < 9 {
		t.Fatalf("version = %d, %v", version, err)
	}
	var gated int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM queue_items WHERE verify_command IS NOT NULL OR requires_approval`).Scan(&gated); err != nil || gated != 0 {
		t.Fatalf("existing items with gates = %d, %v; want 0", gated, err)
	}
	// Every existing row passes the new CHECKs (re-validated explicitly).
	for _, stmt := range []string{
		`ALTER TABLE queue_items VALIDATE CONSTRAINT queue_items_status_check`,
		`ALTER TABLE run_events VALIDATE CONSTRAINT run_events_source_check`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	var widened int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_constraint WHERE conname IN ('queue_items_status_check', 'run_events_source_check')
		AND connamespace = current_schema()::regnamespace AND (pg_get_constraintdef(oid) LIKE '%awaiting_approval%' OR pg_get_constraintdef(oid) LIKE '%verify%')`).Scan(&widened); err != nil || widened != 2 {
		t.Fatalf("widened CHECKs = %d, %v", widened, err)
	}

	var rows int
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM goose_db_version`).Scan(&rows)
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var again int
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM goose_db_version`).Scan(&again)
	if again != rows {
		t.Fatalf("second apply added version rows: %d → %d", rows, again)
	}
	for table, sum := range columnChecksums(t, db, tables) {
		if sum != after[table] {
			t.Errorf("%s changed by the second apply", table)
		}
	}
}
