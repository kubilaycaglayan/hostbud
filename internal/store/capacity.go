package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// V2-M2 parallel queues (migrations/0006_parallel_queues.sql): the
// per-machine cap on active runs and the queues waiting for a slot.

// Capacity bounds for machine_capacity.max_concurrent_runs.
const (
	MinConcurrentRuns = 1
	MaxConcurrentRuns = 32
)

// ErrCapacityRange means a cap outside MinConcurrentRuns–MaxConcurrentRuns.
var ErrCapacityRange = errors.New("the run cap must be a whole number from 1 to 32, or empty for no cap")

// MachineCapacity returns the machine's cap on active runs; nil means no
// cap (no row, or a NULL value).
func (s *Store) MachineCapacity(ctx context.Context, machineID string) (*int, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT max_concurrent_runs FROM machine_capacity WHERE machine_id = $1`, machineID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !n.Valid) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v := int(n.Int64)
	return &v, nil
}

// SetMachineCapacity sets the machine's cap; nil clears it (no cap).
func (s *Store) SetMachineCapacity(ctx context.Context, machineID string, maxRuns *int) error {
	var v any
	if maxRuns != nil {
		if *maxRuns < MinConcurrentRuns || *maxRuns > MaxConcurrentRuns {
			return ErrCapacityRange
		}
		v = *maxRuns
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO machine_capacity (machine_id, max_concurrent_runs, updated_at)
		SELECT id, $2, $3 FROM machines WHERE id = $1
		ON CONFLICT (machine_id) DO UPDATE SET max_concurrent_runs = EXCLUDED.max_concurrent_runs, updated_at = EXCLUDED.updated_at`,
		machineID, v, s.now())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountActiveRuns counts the machine's active runs (starting, running and
// stale: a stale run's session is alive, so it holds its slot).
func (s *Store) CountActiveRuns(ctx context.Context, machineID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM runs WHERE machine_id = $1 AND status = ANY($2)`, machineID, ActiveRunStatuses).Scan(&n)
	return n, err
}

// WaitingQueues lists the machine's running queues that wait for a slot,
// oldest waiting_since first (ties by id): the FIFO slot order.
func (s *Store) WaitingQueues(ctx context.Context, machineID string) ([]Queue, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+queueCols+` FROM queues
		WHERE machine_id = $1 AND status = 'running' AND waiting_since IS NOT NULL
		ORDER BY waiting_since, id`, machineID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Queue
	for rows.Next() {
		q, err := scanQueue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// SetQueueWaiting sets (or, with nil, clears) when a queue started waiting
// for a slot.
func (s *Store) SetQueueWaiting(ctx context.Context, id string, since *time.Time) (Queue, error) {
	var v any
	if since != nil {
		v = since.UTC()
	}
	q, err := scanQueue(s.db.QueryRowContext(ctx, `UPDATE queues SET waiting_since = $2 WHERE id = $1 RETURNING `+queueCols, id, v))
	return q, notFound(err)
}
