//go:build integration

package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"testing/fstest"

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
	if err := db.QueryRowContext(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version); err != nil || version != 5 {
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
