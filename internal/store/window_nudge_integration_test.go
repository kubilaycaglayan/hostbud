//go:build integration

package store

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIntegrationWindowNudgePersistsAndClaimsEachOccurrenceOnce(t *testing.T) {
	ctx := context.Background()
	repo, err := Open(ctx, testConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repo.Close() }()
	if _, err := repo.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	repo.now = func() time.Time { return clock }
	initial, err := repo.WindowNudge(ctx)
	if err != nil || initial.Enabled || initial.MachineID != HostMachineID || initial.Provider != WindowNudgeProviderCodex {
		t.Fatalf("default settings = %+v, %v", initial, err)
	}
	due, next := clock.Add(time.Hour), clock.Add(6*time.Hour)
	settings, err := repo.PutWindowNudge(ctx, WindowNudge{
		Enabled: true, MachineID: HostMachineID, Directory: "/home/dev/work/../app",
		Provider: WindowNudgeProviderClaude, Schedule: "01-06-11-16-21", NextRunAt: &due,
	})
	if err != nil || !settings.Enabled || settings.Directory != "/home/dev/app" || settings.NextRunAt == nil || !settings.NextRunAt.Equal(due) {
		t.Fatalf("saved settings = %+v, %v", settings, err)
	}
	if _, err := repo.PutWindowNudge(ctx, WindowNudge{Enabled: true, MachineID: HostMachineID, Directory: "/home/dev", Provider: "claude", Schedule: "01-06-11-16-21", NextRunAt: &clock}); err == nil {
		t.Fatal("accepted a due time that is not in the future")
	}
	clock = due
	var claimed atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := repo.ClaimWindowNudgeOccurrence(ctx, due, next)
			if err != nil {
				t.Errorf("claim occurrence: %v", err)
			}
			if ok {
				claimed.Add(1)
			}
		}()
	}
	wg.Wait()
	if claimed.Load() != 1 {
		t.Fatalf("successful claims = %d, want 1", claimed.Load())
	}
	if err := repo.FinishWindowNudgeOccurrence(ctx, due, WindowNudgeSucceeded, "Started Codex"); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.WindowNudge(ctx)
	if err != nil || loaded.LastOutcome != WindowNudgeSucceeded || loaded.LastDetail != "Started Codex" || loaded.LastRunAt == nil || !loaded.LastRunAt.Equal(due) {
		t.Fatalf("loaded outcome = %+v, %v", loaded, err)
	}

	// A due time found overdue during startup is advanced, without recording a
	// claim that could accidentally run late.
	past := clock.Add(-time.Minute)
	if _, err := repo.db.ExecContext(ctx, `UPDATE window_nudge_settings SET next_run_at = $1 WHERE id = 1`, formatTime(past)); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Minute)
	future := clock.Add(time.Hour)
	advanced, err := repo.SkipMissedWindowNudge(ctx, past, future)
	if err != nil || !advanced {
		t.Fatalf("skip missed occurrence = %v, %v", advanced, err)
	}
	if _, err := repo.db.ExecContext(ctx, `INSERT INTO window_nudge_occurrences (scheduled_at, machine_id, provider, outcome) VALUES ($1, $2, $3, 'claimed')`, formatTime(past.Add(-time.Hour)), HostMachineID, WindowNudgeProviderClaude); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecoverWindowNudgeOccurrences(ctx); err != nil {
		t.Fatal(err)
	}
	var recovered string
	if err := repo.db.QueryRowContext(ctx, `SELECT outcome FROM window_nudge_occurrences WHERE scheduled_at = $1`, formatTime(past.Add(-time.Hour))).Scan(&recovered); err != nil || recovered != WindowNudgeInterrupted {
		t.Fatalf("recovered occurrence = %q, %v", recovered, err)
	}
}
