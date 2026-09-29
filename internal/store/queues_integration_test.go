//go:build integration

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pressly/goose/v3"
)

var v1Tables = []string{"machines", "ui_state", "users", "email_allowlist", "auth_sessions", "login_rate_limits", "projects", "session_links", "recent_commands"}

// tableChecksums returns each table's row count and an md5 over its rows in
// a stable order.
func tableChecksums(t *testing.T, db *sql.DB, tables []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range tables {
		var n int
		var sum sql.NullString
		if err := db.QueryRowContext(context.Background(),
			`SELECT count(*), md5(string_agg(x::text, E'\n' ORDER BY x::text)) FROM `+table+` x`).Scan(&n, &sum); err != nil {
			t.Fatalf("checksum %s: %v", table, err)
		}
		out[table] = sum.String
		if n == 0 {
			t.Fatalf("seed left %s empty", table)
		}
	}
	return out
}

// V2-M1 T2: a populated v1 database (migrations 0001–0004) keeps every row
// when 0005 applies, and applying again changes nothing.
func TestIntegrationQueueMigrationKeepsV1Data(t *testing.T) {
	ctx := context.Background()
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

	v1 := fstest.MapFS{}
	for _, name := range []string{"0001_init.sql", "0002_machine_home.sql", "0003_auth.sql", "0004_projects.sql"} {
		b, err := migrations.ReadFile(filepath.ToSlash(filepath.Join("migrations", name)))
		if err != nil {
			t.Fatal(err)
		}
		v1[name] = &fstest.MapFile{Data: b}
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, v1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO machines (id, source, ssh_alias, label, active, home, os, tmux_version, tmux_missing, created_at, updated_at) VALUES ('host', 'host', 'hostbud-host', 'Host machine', TRUE, '/home/dev', 'Linux', 'tmux 3.4', FALSE, now(), now())`,
		`INSERT INTO ui_state (key, value_json, updated_at) VALUES ('user:user-a:layout', '{"version":1,"tabs":[]}', now())`,
		`INSERT INTO users (id, email, email_normalized, password_hash) VALUES ('user-a', 'person@example.com', 'person@example.com', 'test-hash')`,
		`INSERT INTO email_allowlist (email_normalized) VALUES ('person@example.com')`,
		`INSERT INTO auth_sessions (id_hash, user_id, expires_at, user_agent) VALUES ('test-session-hash', 'user-a', now() + interval '1 day', 'test-agent')`,
		`INSERT INTO login_rate_limits (scope_key, failures, last_failure_at) VALUES ('test-rate-limit-key', 2, now())`,
		`INSERT INTO projects (id, machine_id, path, name, sort_order, pinned) VALUES ('project_a', 'host', '/home/dev/app', 'app', 1, TRUE), ('project_b', 'host', '/home/dev/other', 'other', 2, FALSE)`,
		`INSERT INTO session_links (machine_id, session_name, project_id) VALUES ('host', 'app', 'project_a')`,
		`INSERT INTO recent_commands (project_id, command) VALUES ('project_a', 'claude'), ('project_a', 'codex --yolo')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	before := tableChecksums(t, db, v1Tables)

	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	after := tableChecksums(t, db, v1Tables)
	for _, table := range v1Tables {
		if before[table] != after[table] {
			t.Errorf("%s changed by the queue migration", table)
		}
	}
	var version int64
	if latest, _ := latestMigrationVersion(); db.QueryRowContext(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version) != nil || version != latest {
		t.Fatalf("version = %d, %v", version, err)
	}
	var queueTables int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name IN ('queues', 'queue_items', 'runs', 'run_events')`).Scan(&queueTables); err != nil || queueTables != 4 {
		t.Fatalf("queue tables = %d, %v", queueTables, err)
	}

	// A second apply is a no-op: no new version rows, same data.
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
	for table, sum := range tableChecksums(t, db, v1Tables) {
		if sum != after[table] {
			t.Errorf("%s changed by the second apply", table)
		}
	}
}

// V2-M6 T1: only a live tracked run may be attached to a new queue.
func TestIntegrationQueueAfterActiveRun(t *testing.T) {
	ctx := context.Background()
	repo, err := Open(ctx, testConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repo.Close() }()
	if _, err := repo.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	project, err := repo.CreateProject(ctx, HostMachineID, "/home/dev/after-goal", "after-goal")
	if err != nil {
		t.Fatal(err)
	}
	prior, err := repo.CreateQueue(ctx, project.ID, "Prior")
	if err != nil {
		t.Fatal(err)
	}
	item, err := repo.AddQueueItem(ctx, prior.ID, "claude", "", "/goal finish first")
	if err != nil {
		t.Fatal(err)
	}
	run, err := repo.CreateRun(ctx, item.ID, make([]byte, 32), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dependent, err := repo.CreateQueue(ctx, project.ID, "Dependent", run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dependent.AfterRunID == nil || *dependent.AfterRunID != run.ID {
		t.Fatalf("after run = %v; want %s", dependent.AfterRunID, run.ID)
	}
	ended := time.Now()
	if _, err := repo.TransitionRun(ctx, run.ID, []string{RunStarting}, RunAchieved, "", &ended); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateQueue(ctx, project.ID, "Too late", run.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("terminal predecessor accepted: %v", err)
	}
}

// columnChecksums is tableChecksums over fixed columns, so a table that
// gains a column keeps a comparable checksum.
// v2m1ItemCols are queue_items' columns before V2-M4 (0009 adds the gate
// columns, whose defaults would change a "*" checksum).
const v2m1ItemCols = "id, queue_id, machine_id, position, agent, flags, instruction, status, created_at, updated_at"
const v2m5QueueCols = "id, machine_id, project_id, name, status, created_at, updated_at, waiting_since"

func columnChecksums(t *testing.T, db *sql.DB, tables map[string]string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for table, cols := range tables {
		var n int
		var sum sql.NullString
		if err := db.QueryRowContext(context.Background(),
			`SELECT count(*), md5(string_agg(x::text, E'\n' ORDER BY x::text)) FROM (SELECT `+cols+` FROM `+table+`) x`).Scan(&n, &sum); err != nil {
			t.Fatalf("checksum %s: %v", table, err)
		}
		if n == 0 {
			t.Fatalf("seed left %s empty", table)
		}
		out[table] = fmt.Sprintf("%d:%s", n, sum.String)
	}
	return out
}

// V2-M2 T1: a populated V2-M1 database (migrations 0001–0005, with queues,
// items, runs and events) keeps every row when 0006 applies, and applying
// again changes nothing. The migration only adds.
func TestIntegrationParallelQueuesMigrationKeepsV2M1Data(t *testing.T) {
	ctx := context.Background()
	raw, err := migrations.ReadFile("migrations/0006_parallel_queues.sql")
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
	for _, forbidden := range []string{"DROP", "UPDATE ", "DELETE", "RENAME", "TRUNCATE", "ALTER COLUMN"} {
		if strings.Contains(upper, forbidden) {
			t.Errorf("0006 contains %q; the migration may only add", forbidden)
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

	v2m1 := fstest.MapFS{}
	for _, name := range []string{"0001_init.sql", "0002_machine_home.sql", "0003_auth.sql", "0004_projects.sql", "0005_queues.sql"} {
		b, err := migrations.ReadFile(filepath.ToSlash(filepath.Join("migrations", name)))
		if err != nil {
			t.Fatal(err)
		}
		v2m1[name] = &fstest.MapFile{Data: b}
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, v2m1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("ab", 32)
	for _, stmt := range []string{
		`INSERT INTO machines (id, source, ssh_alias, label, active, home, os, tmux_version, tmux_missing, created_at, updated_at) VALUES ('host', 'host', 'hostbud-host', 'Host machine', TRUE, '/home/dev', 'Linux', 'tmux 3.4', FALSE, now(), now())`,
		`INSERT INTO projects (id, machine_id, path, name, sort_order, pinned) VALUES ('project_a', 'host', '/home/dev/app', 'app', 1, TRUE)`,
		`INSERT INTO queues (id, machine_id, project_id, name, status) VALUES ('queue_a', 'host', 'project_a', 'Queue', 'paused')`,
		`INSERT INTO queue_items (id, queue_id, machine_id, position, agent, flags, instruction, status) VALUES
			('item_a', 'queue_a', 'host', 1, 'claude', '--model x', '/goal one', 'done'),
			('item_b', 'queue_a', 'host', 2, 'codex', '', '/goal two', 'needs_attention'),
			('item_c', 'queue_a', 'host', 3, 'claude', '', '/goal three', 'queued')`,
		`INSERT INTO runs (id, item_id, machine_id, session_name, agent_session_id, token_hash, status, started_at, ended_at, detail) VALUES
			('01ARZ3NDEKTSV4RRFFQ69G5FA1', 'item_a', 'host', 'app-q1', 'session-1', decode('` + hash + `', 'hex'), 'achieved', now(), now(), NULL),
			('01ARZ3NDEKTSV4RRFFQ69G5FA2', 'item_b', 'host', 'app-q2', 'thread-2', decode('` + strings.Repeat("cd", 32) + `', 'hex'), 'stale', now(), NULL, 'no signal')`,
		`INSERT INTO run_events (run_id, machine_id, source, kind, payload_json) VALUES
			('01ARZ3NDEKTSV4RRFFQ69G5FA1', 'host', 'hook', 'session_start', '{"session_id":"session-1"}'),
			('01ARZ3NDEKTSV4RRFFQ69G5FA2', 'host', 'timer', 'stale', '{"detail":"no signal"}')`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	tables := map[string]string{
		"machines":    "*",
		"projects":    "*",
		"queues":      "id, machine_id, project_id, name, status, created_at, updated_at",
		"queue_items": v2m1ItemCols,
		"runs":        "*",
		"run_events":  "*",
	}
	before := columnChecksums(t, db, tables)

	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	after := columnChecksums(t, db, tables)
	for table := range tables {
		if before[table] != after[table] {
			t.Errorf("%s changed by the parallel-queues migration", table)
		}
	}
	var version int64
	if err := db.QueryRowContext(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version); err != nil || version < 7 { // 0007 and any later migration
		t.Fatalf("version = %d, %v", version, err)
	}
	var waiting sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT waiting_since FROM queues WHERE id = 'queue_a'`).Scan(&waiting); err != nil || waiting.Valid {
		t.Fatalf("existing queue waiting_since = %v, %v; want NULL", waiting, err)
	}
	var capRows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM machine_capacity`).Scan(&capRows); err != nil || capRows != 0 {
		t.Fatalf("machine_capacity rows = %d, %v; want 0 (no cap)", capRows, err)
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
