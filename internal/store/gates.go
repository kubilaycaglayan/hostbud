package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// V2-M4 completion gates (migrations/0009_completion_gates.sql): the verify
// runner's claimed attempts and results, recorded as run events with
// source 'verify' on the item's achieved run.

// Verify run-event kinds.
const (
	KindVerifyStarted = "verify_started"
	KindVerifyResult  = "verify_result"
)

// StartVerify claims a verify attempt: in one transaction the item moves
// from one of from to verifying and a verify_started event with the next
// attempt number (1, 2, …) is recorded on run, so an attempt is always
// stored before hostbud runs it. ErrConflict: the item is in another state.
func (s *Store) StartVerify(ctx context.Context, itemID, runID string, from []string) (int, QueueItem, error) {
	var attempt int
	var it QueueItem
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		it, err = scanItem(tx.QueryRowContext(ctx, `
			UPDATE queue_items SET status = 'verifying', updated_at = $2 WHERE id = $1 AND status = ANY($3) RETURNING `+itemCols,
			itemID, s.now(), from))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		} else if err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT count(*) + 1 FROM run_events WHERE run_id = $1 AND source = 'verify' AND kind = $2`, runID, KindVerifyStarted).Scan(&attempt); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]int{"attempt": attempt})
		return insertRunEvent(ctx, tx, runID, SourceVerify, KindVerifyStarted, payload, s.now())
	})
	if errors.Is(err, ErrConflict) {
		if _, e := s.QueueItem(ctx, itemID); e != nil {
			return 0, QueueItem{}, e
		}
	}
	return attempt, it, err
}

// FinishVerify records a verify attempt's result on run and moves the item
// from verifying to `to`, with its notice (nil: none), in one transaction.
// ErrConflict (nothing recorded): the item is no longer verifying.
func (s *Store) FinishVerify(ctx context.Context, itemID, runID string, result []byte, to string, n *Notice) (QueueItem, error) {
	if !slices.Contains(itemStatuses, to) {
		return QueueItem{}, fmt.Errorf("invalid item status %q", to)
	}
	if len(result) > MaxRunEventPayload {
		return QueueItem{}, ErrPayloadTooLarge
	}
	var it QueueItem
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		it, err = scanItem(tx.QueryRowContext(ctx, `
			UPDATE queue_items SET status = $2, updated_at = $3 WHERE id = $1 AND status = 'verifying' RETURNING `+itemCols,
			itemID, to, s.now()))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		} else if err != nil {
			return err
		}
		if err := insertRunEvent(ctx, tx, runID, SourceVerify, KindVerifyResult, result, s.now()); err != nil {
			return err
		}
		return enqueueNotice(ctx, tx, n, s.now())
	})
	return it, err
}

func insertRunEvent(ctx context.Context, tx *sql.Tx, runID, source, kind string, payload []byte, now any) error {
	if !json.Valid(payload) {
		return errors.New("run event payload must be JSON")
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO run_events (run_id, machine_id, source, kind, payload_json, created_at)
		SELECT r.id, r.machine_id, $2, $3, $4, $5 FROM runs r WHERE r.id = $1`, runID, source, kind, string(payload), now)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// VerifyEvents lists a run's verify events (attempts and results), oldest
// first.
func (s *Store) VerifyEvents(ctx context.Context, runID string) ([]RunEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+eventCols+` FROM run_events WHERE run_id = $1 AND source = 'verify' ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []RunEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ItemsWithStatus lists a machine's items in one state (restart recovery).
func (s *Store) ItemsWithStatus(ctx context.Context, machineID, status string) ([]QueueItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM queue_items WHERE machine_id = $1 AND status = $2 ORDER BY queue_id, position`, machineID, status)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []QueueItem
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// LatestRunForItem returns the item's newest run or ErrNotFound.
func (s *Store) LatestRunForItem(ctx context.Context, itemID string) (Run, error) {
	r, err := scanRun(s.db.QueryRowContext(ctx, `SELECT `+runCols+` FROM runs WHERE item_id = $1 ORDER BY id DESC LIMIT 1`, itemID))
	return r, notFound(err)
}

// Approval run-event kinds (source 'user').
const (
	KindApproved = "approved"
	KindRejected = "rejected"
)

// ResolveApproval applies the owner's Approve (to done) or Reject (to
// needs_attention) to an item awaiting approval: the guarded update, the
// owner's run event on run and the notice (nil: none) in one transaction,
// so of two racing actions exactly one applies. ErrConflict: the item is
// no longer awaiting approval.
func (s *Store) ResolveApproval(ctx context.Context, itemID, runID, to, kind string, payload []byte, n *Notice) (QueueItem, error) {
	if to != ItemDone && to != ItemNeedsAttention {
		return QueueItem{}, fmt.Errorf("invalid approval outcome %q", to)
	}
	var it QueueItem
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		it, err = scanItem(tx.QueryRowContext(ctx, `
			UPDATE queue_items SET status = $2, updated_at = $3 WHERE id = $1 AND status = 'awaiting_approval' RETURNING `+itemCols,
			itemID, to, s.now()))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		} else if err != nil {
			return err
		}
		if err := insertRunEvent(ctx, tx, runID, SourceUser, kind, payload, s.now()); err != nil {
			return err
		}
		return enqueueNotice(ctx, tx, n, s.now())
	})
	return it, err
}
