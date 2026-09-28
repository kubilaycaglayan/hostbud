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

// slotHolders counts what holds a machine's run slots: active runs
// (starting, running and stale: a stale run's session is alive) and items
// whose verify command runs on the host (V2-M4).
const slotHolders = `SELECT (SELECT count(*) FROM runs WHERE machine_id = $1 AND status = ANY($2))
	+ (SELECT count(*) FROM queue_items WHERE machine_id = $1 AND status = 'verifying')`

// CountActiveRuns counts the machine's held run slots (slotHolders).
func (s *Store) CountActiveRuns(ctx context.Context, machineID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, slotHolders, machineID, ActiveRunStatuses).Scan(&n)
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

// ErrNoSlot means the machine's cap on active runs is reached.
var ErrNoSlot = errors.New("no free run slot on this machine")

// CreateRunInSlot is CreateRun behind the machine's cap: in one
// transaction it takes the machine's run-slot lock (an advisory lock, so
// every writer on any connection queues behind it), re-counts the active
// runs and inserts the run only while that count is below the cap
// (ErrNoSlot otherwise). The dispatcher decides first; this is the
// store's re-check, so concurrent callers can never exceed the cap.
func (s *Store) CreateRunInSlot(ctx context.Context, itemID string, tokenHash []byte, startedAt time.Time) (Run, error) {
	if len(tokenHash) != 32 {
		return Run{}, errors.New("run token hash must be a SHA-256")
	}
	id, err := NewULID(startedAt)
	if err != nil {
		return Run{}, err
	}
	var r Run
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		var machine string
		if err := tx.QueryRowContext(ctx, `SELECT machine_id FROM queue_items WHERE id = $1`, itemID).Scan(&machine); err != nil {
			return notFound(err)
		}
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('hostbud-run-slots:' || $1))`, machine); err != nil {
			return err
		}
		var limit sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT max_concurrent_runs FROM machine_capacity WHERE machine_id = $1`, machine).Scan(&limit)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if limit.Valid {
			var active int64
			if err := tx.QueryRowContext(ctx, slotHolders, machine, ActiveRunStatuses).Scan(&active); err != nil {
				return err
			}
			if active >= limit.Int64 {
				return ErrNoSlot
			}
		}
		r, err = scanRun(tx.QueryRowContext(ctx, `
			INSERT INTO runs (id, item_id, machine_id, token_hash, status, started_at)
			VALUES ($1, $2, $3, $4, 'starting', $5) RETURNING `+runCols, id, itemID, machine, tokenHash, startedAt.UTC()))
		return err
	})
	return r, err
}

// ParallelQueuesSetting returns the owner's parallel-queues switch for the
// machine; nil means not set (the HOSTBUD_PARALLEL_QUEUES default applies).
func (s *Store) ParallelQueuesSetting(ctx context.Context, machineID string) (*bool, error) {
	var v sql.NullBool
	err := s.db.QueryRowContext(ctx, `SELECT parallel_queues FROM machine_capacity WHERE machine_id = $1`, machineID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !v.Valid) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	on := v.Bool
	return &on, nil
}

// SetParallelQueuesSetting stores the owner's parallel-queues switch; the
// machine's cap is kept.
func (s *Store) SetParallelQueuesSetting(ctx context.Context, machineID string, on bool) error {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO machine_capacity (machine_id, parallel_queues, updated_at)
		SELECT id, $2, $3 FROM machines WHERE id = $1
		ON CONFLICT (machine_id) DO UPDATE SET parallel_queues = EXCLUDED.parallel_queues, updated_at = EXCLUDED.updated_at`,
		machineID, on, s.now())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
