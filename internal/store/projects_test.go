package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func TestNormalizeProjectPathAndName(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"/home/dev/work/../app/", "/home/dev/app"},
		{"/", "/"},
		{"/home/dev/a b/'$(touch nope)/λ", "/home/dev/a b/'$(touch nope)/λ"},
	} {
		got, err := normalizeProjectPath(tc.input)
		if err != nil || got != tc.want {
			t.Errorf("normalizeProjectPath(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
		}
	}
	for _, invalid := range []string{"", "relative/path", "/bad\x00path", "/" + strings.Repeat("a", maxProjectPathBytes)} {
		if _, err := normalizeProjectPath(invalid); err == nil {
			t.Errorf("normalizeProjectPath(%q) accepted invalid path", invalid)
		}
	}
	if got, err := normalizeProjectName("  Work  ", "/home/dev/work"); err != nil || got != "Work" {
		t.Fatalf("normalized name = %q, %v", got, err)
	}
	if got, err := normalizeProjectName("", "/home/dev/work"); err != nil || got != "work" {
		t.Fatalf("default name = %q, %v", got, err)
	}
	if _, err := normalizeProjectName(" ", ""); err == nil {
		t.Fatal("empty renamed project name accepted")
	}
}

func TestNormalizeRecentCommand(t *testing.T) {
	command := `  codex --prompt "literal $(touch nope)"; echo λ  `
	got, err := normalizeCommand(command)
	if err != nil || got != command {
		t.Fatalf("command changed: %q, %v", got, err)
	}
	for _, invalid := range []string{"", "  \t", "bad\x00command", strings.Repeat("x", maxCommandBytes+1)} {
		if _, err := normalizeCommand(invalid); err == nil {
			t.Errorf("normalizeCommand accepted %q", invalid)
		}
	}
}

func TestProjectMigrationIsAppendOnly(t *testing.T) {
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 || files[len(files)-1] != "migrations/0004_projects.sql" {
		t.Fatalf("migration sequence = %v", files)
	}
	sqlBytes, err := migrations.ReadFile("migrations/0004_projects.sql")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(sqlBytes))
	for _, forbidden := range []string{"DROP TABLE", "DROP COLUMN", "DELETE FROM", "TRUNCATE"} {
		if strings.Contains(upper, forbidden) {
			t.Errorf("append-only migration contains %q", forbidden)
		}
	}
}

func TestProjectRepositoriesAndMachineScoping(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()
	if _, err := s.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO machines (id, source, ssh_alias, label, active, created_at, updated_at) VALUES ('server-a', 'custom', 'server-a', 'server-a', TRUE, now(), now())`); err != nil {
		t.Fatal(err)
	}

	first, err := s.CreateProject(ctx, HostMachineID, "/home/dev/apps/../app/", "App")
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := s.CreateProject(ctx, HostMachineID, "/home/dev/app", "Ignored duplicate name")
	if err != nil || duplicate.ID != first.ID || duplicate.Name != "App" || duplicate.Path != "/home/dev/app" {
		t.Fatalf("duplicate project = %+v, %v; existing %+v", duplicate, err, first)
	}
	otherMachine, err := s.CreateProject(ctx, "server-a", "/home/dev/app", "Other machine")
	if err != nil || otherMachine.ID == first.ID {
		t.Fatalf("same path on a different machine = %+v, %v", otherMachine, err)
	}
	updated, err := s.RenameProject(ctx, first.ID, "Renamed app")
	if err != nil || updated.Name != "Renamed app" || !updated.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("rename = %+v, %v", updated, err)
	}
	nested, err := s.CreateProject(ctx, HostMachineID, "/home/dev/app/nested", "Renamed app")
	if err != nil || nested.ID == first.ID {
		t.Fatalf("nested project with the same display name = %+v, %v", nested, err)
	}
	projects, err := s.Projects(ctx, HostMachineID)
	if err != nil || len(projects) != 2 || projects[0].ID != first.ID || projects[1].ID != nested.ID || projects[0].Name != projects[1].Name {
		t.Fatalf("host projects = %+v, %v", projects, err)
	}
	if got, err := s.Project(ctx, first.ID); err != nil || got.Name != "Renamed app" {
		t.Fatalf("get project = %+v, %v", got, err)
	}

	if err := s.UpsertSessionLink(ctx, HostMachineID, "work", first.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSessionLink(ctx, HostMachineID, "work", first.ID); err != nil {
		t.Fatal(err)
	}
	if link, err := s.SessionLink(ctx, HostMachineID, "work"); err != nil || link.ProjectID != first.ID {
		t.Fatalf("session link = %+v, %v", link, err)
	}
	if err := s.RenameSessionLink(ctx, HostMachineID, "work", "renamed-work"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionLink(ctx, HostMachineID, "work"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old link survived rename: %v", err)
	}
	if link, err := s.SessionLink(ctx, HostMachineID, "renamed-work"); err != nil || link.ProjectID != first.ID {
		t.Fatalf("renamed link = %+v, %v", link, err)
	}
	if err := s.RenameSessionLink(ctx, HostMachineID, "renamed-work", ""); err == nil {
		t.Fatal("invalid session rename accepted")
	}
	if err := s.DeleteSessionLink(ctx, HostMachineID, "renamed-work"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionLink(ctx, HostMachineID, "renamed-work"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ended session link survived cleanup: %v", err)
	}
	if err := s.UpsertSessionLink(ctx, "server-a", "work", first.ID); err == nil {
		t.Fatal("cross-machine project link accepted")
	}
	if err := s.DeleteProject(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Project(ctx, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted project lookup = %v", err)
	}
}

func TestRecentCommandsAreBoundedOrderedAndProjectScoped(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t, t.TempDir())
	defer func() { _ = s.Close() }()
	if _, err := s.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	p1, err := s.CreateProject(ctx, HostMachineID, "/home/dev/one", "One")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := s.CreateProject(ctx, HostMachineID, "/home/dev/two", "Two")
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	for i := range RecentCommandLimit + 3 {
		if err := s.RememberRecentCommand(ctx, p1.ID, fmt.Sprintf("command-%02d", i)); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(time.Second)
	}
	if err := s.RememberRecentCommand(ctx, p1.ID, `command-05 --literal '$HOME; λ'`); err != nil {
		t.Fatal(err)
	}
	if err := s.RememberRecentCommand(ctx, p2.ID, "only-two"); err != nil {
		t.Fatal(err)
	}
	commands, err := s.RecentCommands(ctx, p1.ID)
	if err != nil || len(commands) != RecentCommandLimit {
		t.Fatalf("recent commands count = %d, %v", len(commands), err)
	}
	if commands[0].Command != `command-05 --literal '$HOME; λ'` || commands[1].Command != "command-22" {
		t.Fatalf("recent command order = %+v", commands[:2])
	}
	for _, command := range commands {
		if command.Command == "command-00" {
			t.Fatal("oldest command not pruned")
		}
	}
	other, err := s.RecentCommands(ctx, p2.ID)
	if err != nil || len(other) != 1 || other[0].Command != "only-two" {
		t.Fatalf("project-scoped commands = %+v, %v", other, err)
	}
	if err := s.RememberRecentCommand(ctx, p1.ID, " \t "); err == nil {
		t.Fatal("empty command accepted")
	}
	if err := s.RememberRecentCommand(ctx, "missing-project", "command"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown project: %v", err)
	}
}

func TestUpgradeFromM3PreservesExistingRows(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t.TempDir())
	plain := cfg
	plain.Schema = ""
	db, err := sql.Open("pgx", plain.dsn())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	quotedSchema, err := quoteIdentifier(cfg.Schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+quotedSchema); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("pgx", cfg.dsn())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	oldMigrations := fstest.MapFS{}
	for _, name := range []string{"0001_init.sql", "0002_machine_home.sql", "0003_auth.sql"} {
		b, err := migrations.ReadFile(filepath.ToSlash(filepath.Join("migrations", name)))
		if err != nil {
			t.Fatal(err)
		}
		oldMigrations[name] = &fstest.MapFile{Data: b}
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, oldMigrations)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO machines (id, source, ssh_alias, label, active, home, os, tmux_version, tmux_missing, created_at, updated_at) VALUES ('host', 'host', 'hostbud-host', 'Host machine', TRUE, '/home/dev', 'Linux', 'tmux 3.4', FALSE, now(), now())`,
		`INSERT INTO ui_state (key, value_json, updated_at) VALUES ('layout', '{"open":true}', now())`,
		`INSERT INTO ui_state (key, value_json, updated_at) VALUES ('user:user-a:layout', '{"version":1,"tabs":[]}', now())`,
		`INSERT INTO users (id, email, email_normalized, password_hash) VALUES ('user-a', 'person@example.com', 'person@example.com', 'test-hash')`,
		`INSERT INTO email_allowlist (email_normalized) VALUES ('person@example.com')`,
		`INSERT INTO auth_sessions (id_hash, user_id, expires_at, user_agent) VALUES ('test-session-hash', 'user-a', now() + interval '1 day', 'test-agent')`,
		`INSERT INTO login_rate_limits (scope_key, failures, last_failure_at) VALUES ('test-rate-limit-key', 2, now())`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var machines, layouts, userLayouts, users, allowlist, authSessions, rateLimits int
	for _, check := range []struct {
		query string
		dest  *int
	}{
		{`SELECT count(*) FROM machines WHERE id='host' AND source='host' AND ssh_alias='hostbud-host' AND label='Host machine' AND active AND home='/home/dev' AND os='Linux' AND tmux_version='tmux 3.4' AND NOT tmux_missing`, &machines},
		{`SELECT count(*) FROM ui_state WHERE key='layout' AND value_json='{"open":true}'`, &layouts},
		{`SELECT count(*) FROM ui_state WHERE key='user:user-a:layout' AND value_json='{"version":1,"tabs":[]}'`, &userLayouts},
		{`SELECT count(*) FROM users WHERE id='user-a'`, &users},
		{`SELECT count(*) FROM email_allowlist WHERE email_normalized='person@example.com'`, &allowlist},
		{`SELECT count(*) FROM auth_sessions WHERE id_hash='test-session-hash' AND user_id='user-a' AND user_agent='test-agent'`, &authSessions},
		{`SELECT count(*) FROM login_rate_limits WHERE scope_key='test-rate-limit-key' AND failures=2`, &rateLimits},
	} {
		if err := db.QueryRowContext(ctx, check.query).Scan(check.dest); err != nil {
			t.Fatal(err)
		}
	}
	if machines != 1 || layouts != 1 || userLayouts != 1 || users != 1 || allowlist != 1 || authSessions != 1 || rateLimits != 1 {
		t.Fatalf("pre-existing M1–M3 rows changed: machines=%d layouts=%d userLayouts=%d users=%d allowlist=%d authSessions=%d rateLimits=%d", machines, layouts, userLayouts, users, allowlist, authSessions, rateLimits)
	}
	var version int64
	if err := db.QueryRowContext(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version); err != nil || version != 4 {
		t.Fatalf("migration version = %d, %v", version, err)
	}
}
