package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrInUse is returned when a machine can't be removed because projects,
// queues or runs still reference it.
var ErrInUse = errors.New("in use")

// NewMachine is a custom machine (a server added in the UI, V2-M13).
type NewMachine struct {
	ID       string
	SSHAlias string
	Label    string
	HostName string
	Port     int
	SSHUser  string
	HostKeys string
}

// CreateMachine stores an active custom machine after every existing one.
func (s *Store) CreateMachine(ctx context.Context, m NewMachine) (Machine, error) {
	now := formatTime(s.now())
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO machines (id, source, ssh_alias, label, active, sort_order, host_name, port, ssh_user, host_keys, created_at, updated_at)
		VALUES ($1, 'custom', $2, $3, TRUE, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM machines), $4, $5, $6, $7, $8, $9)`,
		m.ID, m.SSHAlias, m.Label, m.HostName, m.Port, m.SSHUser, m.HostKeys, now, now)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Machine{}, ErrDuplicate
	}
	if err != nil {
		return Machine{}, fmt.Errorf("create machine: %w", err)
	}
	return s.Machine(ctx, m.ID)
}

// RenameMachine changes a custom machine's label (its nickname).
func (s *Store) RenameMachine(ctx context.Context, id, label string) (Machine, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE machines SET label = $1, updated_at = $2 WHERE id = $3 AND source = 'custom'`,
		label, formatTime(s.now()), id)
	if err != nil {
		return Machine{}, fmt.Errorf("rename machine: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return Machine{}, ErrNotFound
	}
	return s.Machine(ctx, id)
}

// UpdateServer replaces the editable connection fields of a custom server.
func (s *Store) UpdateServer(ctx context.Context, id string, m NewMachine) (Machine, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE machines SET label=$1, host_name=$2, port=$3, ssh_user=$4, host_keys=$5, updated_at=$6 WHERE id=$7 AND source='custom'`, m.Label, m.HostName, m.Port, m.SSHUser, m.HostKeys, formatTime(s.now()), id)
	if err != nil {
		return Machine{}, fmt.Errorf("update server: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return Machine{}, ErrNotFound
	}
	return s.Machine(ctx, id)
}

// DeleteMachine removes a custom machine with its session links and run cap.
// It returns ErrNotFound for the host or an unknown id, and ErrInUse while
// projects (or their queues and runs) still reference the machine.
func (s *Store) DeleteMachine(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var source string
	err = tx.QueryRowContext(ctx, `SELECT source FROM machines WHERE id = $1 FOR UPDATE`, id).Scan(&source)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && source != "custom") {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var projects int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE machine_id = $1`, id).Scan(&projects); err != nil {
		return err
	}
	if projects > 0 {
		return ErrInUse
	}
	for _, q := range []string{
		`DELETE FROM session_links WHERE machine_id = $1`,
		`DELETE FROM machine_capacity WHERE machine_id = $1`,
		`DELETE FROM machines WHERE id = $1`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" {
				return ErrInUse
			}
			return fmt.Errorf("delete machine: %w", err)
		}
	}
	return tx.Commit()
}
