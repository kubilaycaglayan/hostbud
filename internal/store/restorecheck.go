package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// RestoreCheckResult contains the migration version and row counts from an
// isolated restore of a hostbud custom-format dump.
type RestoreCheckResult struct {
	Version, Users, Allowlist, Projects, UIState int64
}

// RestoreCheck restores dump into a temporary database on the configured
// server, checks core tables, and drops that database on every return path.
func RestoreCheck(ctx context.Context, conf Config, dump string) (result RestoreCheckResult, retErr error) {
	if strings.TrimSpace(dump) == "" {
		return result, errors.New("restore-check: dump path is required")
	}
	if _, err := exec.LookPath("pg_restore"); err != nil {
		return result, fmt.Errorf("restore-check: pg_restore is unavailable: %w", err)
	}
	list, err := exec.CommandContext(ctx, "pg_restore", "--list", dump).Output() //nolint:gosec // fixed executable and fixed args; dump path is a separate argument
	if err != nil {
		return result, fmt.Errorf("restore-check: file is not a PostgreSQL custom-format dump: %w", err)
	}
	if err := validateRestoreCheckList(list); err != nil {
		return result, err
	}

	schema := conf.Schema
	conf.Schema = ""
	admin, err := sql.Open("pgx", conf.dsn())
	if err != nil {
		return result, fmt.Errorf("restore-check: connect to PostgreSQL: %w", err)
	}
	defer func() {
		if closeErr := admin.Close(); retErr == nil && closeErr != nil {
			retErr = closeErr
		}
	}()
	configurePool(admin)
	if err := admin.PingContext(ctx); err != nil {
		return result, fmt.Errorf("restore-check: connect to PostgreSQL: %w", err)
	}

	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return result, fmt.Errorf("restore-check: generate temporary database name: %w", err)
	}
	dbName := fmt.Sprintf("hostbud_restore_check_%x", random[:])
	dbIdent, err := quoteIdentifier(dbName)
	if err != nil {
		return result, err
	}
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+dbIdent); err != nil {
		return result, fmt.Errorf("restore-check: create temporary database: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if _, dropErr := admin.ExecContext(cleanupCtx, `DROP DATABASE IF EXISTS `+dbIdent+` WITH (FORCE)`); retErr == nil && dropErr != nil {
			retErr = fmt.Errorf("restore-check: drop temporary database %s: %w", dbName, dropErr)
		}
	}()
	args := []string{"--no-password", "--no-owner", "--exit-on-error", "--host", conf.Host, "--port", strconv.Itoa(conf.Port), "--username", conf.User, "--dbname", dbName, dump}
	cmd := exec.CommandContext(ctx, "pg_restore", args...) //nolint:gosec // fixed executable and separate arguments; restore targets the generated throwaway database
	cmd.Env = backupEnv(os.Environ(), conf.Password, conf.SSLMode)
	if output, err := cmd.CombinedOutput(); err != nil {
		return result, fmt.Errorf("restore-check: restore into temporary database: %w: %s", err, strings.TrimSpace(string(output)))
	}

	checkConf := conf
	checkConf.Name = dbName
	checkConf.Schema = schema
	db, err := sql.Open("pgx", checkConf.dsn())
	if err != nil {
		return result, fmt.Errorf("restore-check: open restored database: %w", err)
	}
	configurePool(db)
	defer func() {
		if closeErr := db.Close(); retErr == nil && closeErr != nil {
			retErr = closeErr
		}
	}()
	if err := db.PingContext(ctx); err != nil {
		return result, fmt.Errorf("restore-check: connect to restored database: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&result.Version); err != nil {
		return result, fmt.Errorf("restore-check: read migration version: %w", err)
	}
	latest, err := latestMigrationVersion()
	if err != nil {
		return result, err
	}
	if result.Version < latest {
		return result, fmt.Errorf("restore-check: migration version %d is older than expected version %d", result.Version, latest)
	}
	for _, item := range []struct {
		table string
		dest  *int64
	}{
		{"users", &result.Users}, {"email_allowlist", &result.Allowlist}, {"projects", &result.Projects}, {"ui_state", &result.UIState},
	} {
		query := `SELECT count(*) FROM ` + item.table
		if err := db.QueryRowContext(ctx, query).Scan(item.dest); err != nil {
			return result, fmt.Errorf("restore-check: count %s: %w", item.table, err)
		}
	}
	return result, nil
}

func validateRestoreCheckList(list []byte) error {
	for _, line := range strings.Split(string(list), "\n") {
		fields := strings.Fields(line)
		for i := 0; i+2 < len(fields); i++ {
			if fields[i] == "TABLE" && fields[i+2] == "goose_db_version" {
				return nil
			}
		}
	}
	return errors.New("restore-check: dump does not contain hostbud's goose_db_version table")
}

func latestMigrationVersion() (int64, error) {
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return 0, fmt.Errorf("restore-check: list migrations: %w", err)
	}
	var latest int64
	for _, entry := range entries {
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			continue
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err == nil && version > latest {
			latest = version
		}
	}
	if latest == 0 {
		return 0, errors.New("restore-check: no embedded migrations found")
	}
	return latest, nil
}
