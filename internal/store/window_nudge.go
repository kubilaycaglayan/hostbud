package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

const (
	WindowNudgeProviderClaude = "claude"
	WindowNudgeProviderCodex  = "codex"
	WindowNudgeClaimed        = "claimed"
	WindowNudgeSucceeded      = "succeeded"
	WindowNudgeFailed         = "failed"
	WindowNudgeInterrupted    = "interrupted"
)

var windowNudgeSchedules = map[string]bool{
	"01-06-11-16-21": true,
	"02-07-12-17-22": true,
	"03-08-13-18-23": true,
	"04-09-14-19-00": true,
	"05-10-15-20-00": true,
}

// WindowNudge is the singleton V2-M15 schedule and its latest outcome.
type WindowNudge struct {
	Enabled     bool       `json:"enabled"`
	MachineID   string     `json:"machineId"`
	Directory   string     `json:"directory"`
	Provider    string     `json:"provider"`
	Schedule    string     `json:"schedule"`
	NextRunAt   *time.Time `json:"nextRunAt,omitempty"`
	LastRunAt   *time.Time `json:"lastRunAt,omitempty"`
	LastOutcome string     `json:"lastOutcome,omitempty"`
	LastDetail  string     `json:"lastDetail,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// WindowNudgeOccurrence is one uniquely claimed scheduled time.
type WindowNudgeOccurrence struct {
	ScheduledAt time.Time
	MachineID   string
	Directory   string
	Provider    string
	Schedule    string
	Outcome     string
	Detail      string
}

func validateWindowNudge(n WindowNudge) error {
	if n.MachineID == "" || len(n.MachineID) > 128 || strings.IndexByte(n.MachineID, 0) >= 0 {
		return errors.New("window nudge machine is required")
	}
	if n.Directory == "" || len(n.Directory) > maxProjectPathBytes || strings.IndexByte(n.Directory, 0) >= 0 || !path.IsAbs(n.Directory) {
		return errors.New("window nudge directory must be an absolute path")
	}
	n.Directory = path.Clean(n.Directory)
	if n.Provider != WindowNudgeProviderClaude && n.Provider != WindowNudgeProviderCodex {
		return errors.New("window nudge provider must be claude or codex")
	}
	if !windowNudgeSchedules[n.Schedule] {
		return errors.New("window nudge schedule is invalid")
	}
	if n.Enabled && n.NextRunAt == nil {
		return errors.New("enabled window nudge requires a future occurrence")
	}
	if !n.Enabled && n.NextRunAt != nil {
		return errors.New("disabled window nudge cannot have a future occurrence")
	}
	return nil
}

// WindowNudge reads the singleton settings and latest result. An absent row
// is represented by the disabled host/Codex default, ready for UI setup.
func (s *Store) WindowNudge(ctx context.Context) (WindowNudge, error) {
	n := WindowNudge{MachineID: HostMachineID, Provider: WindowNudgeProviderCodex, Schedule: "01-06-11-16-21"}
	var next sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT enabled, machine_id, directory, provider, schedule, next_run_at, updated_at
		FROM window_nudge_settings WHERE id = 1`).Scan(&n.Enabled, &n.MachineID, &n.Directory, &n.Provider, &n.Schedule, &next, &n.UpdatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return n, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		return n, nil
	}
	if next.Valid {
		nextTime := next.Time.UTC()
		n.NextRunAt = &nextTime
	}
	var last sql.NullTime
	err = s.db.QueryRowContext(ctx, `
		SELECT scheduled_at, outcome, detail FROM window_nudge_occurrences
		ORDER BY scheduled_at DESC LIMIT 1`).Scan(&last, &n.LastOutcome, &n.LastDetail)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return n, err
	}
	if last.Valid {
		lastTime := last.Time.UTC()
		n.LastRunAt = &lastTime
	}
	return n, nil
}

// PutWindowNudge atomically saves configuration. The caller computes a strict
// future next time using internal/nudge and validates that the machine exists.
func (s *Store) PutWindowNudge(ctx context.Context, n WindowNudge) (WindowNudge, error) {
	if err := validateWindowNudge(n); err != nil {
		return WindowNudge{}, err
	}
	if n.Enabled && !n.NextRunAt.After(s.now()) {
		return WindowNudge{}, errors.New("next window nudge occurrence must be in the future")
	}
	n.Directory = path.Clean(n.Directory)
	var next any
	if n.NextRunAt != nil {
		next = formatTime(n.NextRunAt.UTC())
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO window_nudge_settings (id, machine_id, directory, provider, schedule, enabled, next_run_at, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET machine_id = excluded.machine_id,
			directory = excluded.directory, provider = excluded.provider,
			schedule = excluded.schedule, enabled = excluded.enabled,
			next_run_at = excluded.next_run_at, updated_at = excluded.updated_at`,
		n.MachineID, n.Directory, n.Provider, n.Schedule, n.Enabled, next, formatTime(s.now()))
	if err != nil {
		return WindowNudge{}, fmt.Errorf("save window nudge: %w", err)
	}
	return s.WindowNudge(ctx)
}

// ClaimWindowNudgeOccurrence consumes the configured due time and advances to
// next before returning it. The row lock and primary key make the claim
// at-most-once across app instances. A caller must not replay a claimed row.
func (s *Store) ClaimWindowNudgeOccurrence(ctx context.Context, due, next time.Time) (WindowNudgeOccurrence, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WindowNudgeOccurrence{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var n WindowNudgeOccurrence
	var enabled bool
	var savedDue sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT enabled, machine_id, directory, provider, schedule, next_run_at
		FROM window_nudge_settings WHERE id = 1 FOR UPDATE`).Scan(&enabled, &n.MachineID, &n.Directory, &n.Provider, &n.Schedule, &savedDue)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!enabled || !savedDue.Valid || !savedDue.Time.Equal(due) || due.After(s.now())) {
		return WindowNudgeOccurrence{}, false, nil
	}
	if err != nil {
		return WindowNudgeOccurrence{}, false, err
	}
	n.ScheduledAt, n.Outcome = due.UTC(), WindowNudgeClaimed
	if _, err = tx.ExecContext(ctx, `INSERT INTO window_nudge_occurrences (scheduled_at, machine_id, provider, outcome) VALUES ($1, $2, $3, $4)`, formatTime(n.ScheduledAt), n.MachineID, n.Provider, n.Outcome); err != nil {
		return WindowNudgeOccurrence{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE window_nudge_settings SET next_run_at = $1, updated_at = $2 WHERE id = 1`, formatTime(next.UTC()), formatTime(s.now())); err != nil {
		return WindowNudgeOccurrence{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return WindowNudgeOccurrence{}, false, err
	}
	return n, true, nil
}

// SkipMissedWindowNudge advances an overdue slot without creating an
// occurrence. It is used on process startup so downtime never causes a late
// nudge. The expected value prevents a concurrent update from being lost.
func (s *Store) SkipMissedWindowNudge(ctx context.Context, expected, next time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE window_nudge_settings SET next_run_at = $1, updated_at = $2
		WHERE id = 1 AND enabled = TRUE AND next_run_at = $3 AND next_run_at <= $2`,
		formatTime(next.UTC()), formatTime(s.now()), formatTime(expected.UTC()))
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

// FinishWindowNudgeOccurrence records the outcome of a claim. Detail is
// bounded and callers must pass a user-safe, path-free message.
func (s *Store) FinishWindowNudgeOccurrence(ctx context.Context, scheduled time.Time, outcome, detail string) error {
	if outcome != WindowNudgeSucceeded && outcome != WindowNudgeFailed && outcome != WindowNudgeInterrupted {
		return errors.New("invalid window nudge outcome")
	}
	if len(detail) > 1000 {
		detail = detail[:1000]
	}
	result, err := s.db.ExecContext(ctx, `UPDATE window_nudge_occurrences SET outcome = $1, detail = $2, finished_at = $3 WHERE scheduled_at = $4 AND outcome = 'claimed'`, outcome, detail, formatTime(s.now()), formatTime(scheduled.UTC()))
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrConflict
	}
	return nil
}

// RecoverWindowNudgeOccurrences marks claimed work interrupted. Side effects
// are never repeated after restart because their remote outcome may be unknown.
func (s *Store) RecoverWindowNudgeOccurrences(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE window_nudge_occurrences SET outcome = 'interrupted', detail = 'hostbud restarted during this occurrence', finished_at = $1 WHERE outcome = 'claimed'`, formatTime(s.now()))
	return err
}

const windowNudgeAdvisoryLock int64 = 0x574e55444745 // "WNUDGE"

// TryWindowNudgeLock elects one app instance as the scheduler. The returned
// release closes the dedicated connection and releases its PostgreSQL session
// lock, including on process shutdown.
func (s *Store) TryWindowNudgeLock(ctx context.Context) (release func(), acquired bool, err error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	var ok bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, windowNudgeAdvisoryLock).Scan(&ok); err != nil {
		_ = conn.Close()
		return nil, false, err
	}
	if !ok {
		_ = conn.Close()
		return nil, false, nil
	}
	return func() {
		// Closing the dedicated connection releases its session-scoped lock.
		_ = conn.Close()
	}, true, nil
}
