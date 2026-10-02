//go:build integration

package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
)

// V2-M3 T0: a populated V2-M2 database (migrations 0001–0007) keeps every
// row when 0008 applies, every account reads off, and applying again
// changes nothing. The migration only adds.
func TestIntegrationNotificationsMigrationKeepsV2M2Data(t *testing.T) {
	ctx := context.Background()
	raw, err := migrations.ReadFile("migrations/0008_notifications.sql")
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
	for _, forbidden := range []string{"DROP", "UPDATE ", "DELETE", "RENAME", "TRUNCATE", "ALTER "} {
		if strings.Contains(upper, forbidden) {
			t.Errorf("0008 contains %q; the migration may only add", forbidden)
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

	v2m2 := fstest.MapFS{}
	for _, name := range []string{"0001_init.sql", "0002_machine_home.sql", "0003_auth.sql", "0004_projects.sql", "0005_queues.sql", "0006_parallel_queues.sql", "0007_parallel_queues_setting.sql"} {
		b, err := migrations.ReadFile(filepath.ToSlash(filepath.Join("migrations", name)))
		if err != nil {
			t.Fatal(err)
		}
		v2m2[name] = &fstest.MapFile{Data: b}
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, v2m2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO machines (id, source, ssh_alias, label, active, home, os, tmux_version, tmux_missing, created_at, updated_at) VALUES ('host', 'host', 'hostbud-host', 'Host machine', TRUE, '/home/dev', 'Linux', 'tmux 3.4', FALSE, now(), now())`,
		`INSERT INTO users (id, email, email_normalized, password_hash) VALUES ('user_a', 'a@example.com', 'a@example.com', 'hash'), ('user_b', 'b@example.com', 'b@example.com', 'hash')`,
		`INSERT INTO email_allowlist (email_normalized) VALUES ('a@example.com')`,
		`INSERT INTO auth_sessions (id_hash, user_id, expires_at) VALUES ('hash-a', 'user_a', now() + interval '1 day')`,
		`INSERT INTO ui_state (key, value_json, updated_at) VALUES ('user:user_a:theme', '"dark"', '2026-09-28T00:00:00Z')`,
		`INSERT INTO projects (id, machine_id, path, name, sort_order, pinned) VALUES ('project_a', 'host', '/home/dev/app', 'app', 1, TRUE)`,
		`INSERT INTO machine_capacity (machine_id, max_concurrent_runs, parallel_queues) VALUES ('host', 2, TRUE)`,
		`INSERT INTO queues (id, machine_id, project_id, name, status, waiting_since) VALUES ('queue_a', 'host', 'project_a', 'Queue', 'running', now()), ('queue_b', 'host', 'project_a', 'Second', 'paused', NULL)`,
		`INSERT INTO queue_items (id, queue_id, machine_id, position, agent, flags, instruction, status) VALUES
			('item_a', 'queue_a', 'host', 1, 'claude', '--model x', '/goal one', 'done'),
			('item_b', 'queue_b', 'host', 1, 'codex', '', '/goal two', 'needs_attention')`,
		`INSERT INTO runs (id, item_id, machine_id, session_name, agent_session_id, token_hash, status, started_at, ended_at) VALUES
			('01ARZ3NDEKTSV4RRFFQ69G5FA1', 'item_a', 'host', 'app-q1', 'session-1', decode('` + strings.Repeat("ab", 32) + `', 'hex'), 'achieved', now(), now()),
			('01ARZ3NDEKTSV4RRFFQ69G5FA2', 'item_b', 'host', 'app-Second-q1', 'thread-2', decode('` + strings.Repeat("cd", 32) + `', 'hex'), 'exited', now(), now())`,
		`INSERT INTO run_events (run_id, machine_id, source, kind, payload_json) VALUES ('01ARZ3NDEKTSV4RRFFQ69G5FA1', 'host', 'hook', 'session_start', '{"session_id":"session-1"}')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	tables := map[string]string{}
	for _, table := range []string{"machines", "users", "email_allowlist", "auth_sessions", "ui_state", "projects", "machine_capacity", "queues", "queue_items", "runs", "run_events"} {
		tables[table] = "*"
	}
	tables["queues"] = v2m5QueueCols
	tables["machine_capacity"] = v2m2CapacityCols
	tables["queue_items"] = v2m1ItemCols
	tables["runs"] = v2m1RunCols
	before := columnChecksums(t, db, tables)

	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	after := columnChecksums(t, db, tables)
	for table := range tables {
		if before[table] != after[table] {
			t.Errorf("%s changed by the notifications migration", table)
		}
	}
	var version int64
	if err := db.QueryRowContext(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version); err != nil || version < 8 {
		t.Fatalf("version = %d, %v", version, err)
	}
	for _, table := range []string{"notification_prefs", "push_subscriptions", "notification_outbox", "notification_deliveries"} {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s rows = %d, %v; want 0 (every account off)", table, n, err)
		}
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
