package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// V2-M2 T1: the cap's bounds, NULL/no row = no cap.
func TestMachineCapacityBounds(t *testing.T) {
	ctx := context.Background()
	s, _ := queueFixture(t)
	if c, err := s.MachineCapacity(ctx, HostMachineID); err != nil || c != nil {
		t.Fatalf("no row: cap=%v err=%v, want nil (no cap)", c, err)
	}
	for _, bad := range []int{0, -1, 33, 1000} {
		if err := s.SetMachineCapacity(ctx, HostMachineID, &bad); !errors.Is(err, ErrCapacityRange) {
			t.Errorf("cap %d: err=%v, want ErrCapacityRange", bad, err)
		}
	}
	for _, good := range []int{1, 7, 32} {
		if err := s.SetMachineCapacity(ctx, HostMachineID, &good); err != nil {
			t.Fatalf("cap %d: %v", good, err)
		}
		if c, err := s.MachineCapacity(ctx, HostMachineID); err != nil || c == nil || *c != good {
			t.Fatalf("cap %d read back as %v, %v", good, c, err)
		}
	}
	if err := s.SetMachineCapacity(ctx, HostMachineID, nil); err != nil {
		t.Fatal(err)
	}
	if c, err := s.MachineCapacity(ctx, HostMachineID); err != nil || c != nil {
		t.Fatalf("cleared: cap=%v err=%v, want nil", c, err)
	}
	one := 1
	if err := s.SetMachineCapacity(ctx, "server-a", &one); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown machine: err=%v, want ErrNotFound", err)
	}
	// The CHECK holds even without the Go validation.
	if _, err := s.db.ExecContext(ctx, `UPDATE machine_capacity SET max_concurrent_runs = 33`); err == nil {
		t.Error("the schema accepted a cap of 33")
	}
}

// The active count is per machine and includes stale runs.
func TestCountActiveRunsIncludesStale(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, err := s.CreateQueue(ctx, p.ID, "Queue")
	if err != nil {
		t.Fatal(err)
	}
	statuses := []string{RunStarting, RunRunning, RunStale, RunAchieved, RunFailed, RunExited, RunCancelled}
	for i, status := range statuses {
		it, err := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal item")
		if err != nil {
			t.Fatal(err)
		}
		run, err := s.CreateRun(ctx, it.ID, tokenHash("count"+status), time.Now().Add(time.Duration(i)*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		if status != RunStarting {
			if _, err := s.TransitionRun(ctx, run.ID, []string{RunStarting}, status, "", nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n, err := s.CountActiveRuns(ctx, HostMachineID); err != nil || n != 3 {
		t.Fatalf("active runs = %d, %v; want 3 (starting, running, stale)", n, err)
	}
	if n, err := s.CountActiveRuns(ctx, "server-a"); err != nil || n != 0 {
		t.Fatalf("other machine = %d, %v; want 0", n, err)
	}
}

// Waiting queues come oldest waiting_since first, ties by id; only running
// queues with a waiting_since are listed.
func TestWaitingQueuesOrder(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	base := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	mk := func(name string, status string, since *time.Time) Queue {
		t.Helper()
		q, err := s.CreateQueue(ctx, p.ID, name)
		if err != nil {
			t.Fatal(err)
		}
		if status != QueueIdle {
			if _, err := s.TransitionQueue(ctx, q.ID, []string{QueueIdle}, status); err != nil {
				t.Fatal(err)
			}
		}
		if q, err = s.SetQueueWaiting(ctx, q.ID, since); err != nil {
			t.Fatal(err)
		}
		return q
	}
	at := func(d time.Duration) *time.Time { v := base.Add(d); return &v }
	late := mk("late", QueueRunning, at(2*time.Second))
	early := mk("early", QueueRunning, at(0))
	tieA := mk("tie-a", QueueRunning, at(time.Second))
	tieB := mk("tie-b", QueueRunning, at(time.Second))
	mk("paused", QueuePaused, at(-time.Hour))
	mk("not waiting", QueueRunning, nil)
	if early.WaitingSince == nil || !early.WaitingSince.Equal(base) {
		t.Fatalf("waiting_since read back as %v", early.WaitingSince)
	}
	got, err := s.WaitingQueues(ctx, HostMachineID)
	if err != nil {
		t.Fatal(err)
	}
	first, second := tieA, tieB
	if tieB.ID < tieA.ID {
		first, second = tieB, tieA
	}
	want := []string{early.ID, first.ID, second.ID, late.ID}
	if len(got) != len(want) {
		t.Fatalf("waiting = %d queues, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Errorf("waiting[%d] = %s, want %s", i, got[i].Name, want[i])
		}
	}
	if q, err := s.SetQueueWaiting(ctx, late.ID, nil); err != nil || q.WaitingSince != nil {
		t.Fatalf("clear waiting: %v, %v", q.WaitingSince, err)
	}
}

// Queue names are unique per project, case-insensitively; other projects
// may reuse a name.
func TestQueueNamesUniquePerProject(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	other, err := s.CreateProject(ctx, HostMachineID, "/home/dev/other", "other")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateQueue(ctx, p.ID, "Docs")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateQueue(ctx, p.ID, "docs"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("same name, other case: err=%v, want ErrDuplicate", err)
	}
	if _, err := s.CreateQueue(ctx, other.ID, "Docs"); err != nil {
		t.Fatalf("another project may reuse the name: %v", err)
	}
	b, err := s.CreateQueue(ctx, p.ID, "Tests")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameQueue(ctx, b.ID, "DOCS"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("rename onto a taken name: err=%v, want ErrDuplicate", err)
	}
	if _, err := s.RenameQueue(ctx, a.ID, "docs"); err != nil {
		t.Fatalf("renaming a queue to its own name in another case: %v", err)
	}
}

// V2-M2 T4: the store re-checks the cap under a per-machine lock, so
// concurrent callers never exceed it; no cap never refuses.
func TestCreateRunInSlotHoldsTheCapUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, err := s.CreateQueue(ctx, p.ID, "Queue")
	if err != nil {
		t.Fatal(err)
	}
	var items []QueueItem
	for i := range 20 {
		it, err := s.AddQueueItem(ctx, q.ID, "claude", "", fmt.Sprintf("/goal %d", i))
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, it)
	}
	// No cap: every run is created.
	for i := range 2 {
		if _, err := s.CreateRunInSlot(ctx, items[i].ID, tokenHash(fmt.Sprint("free", i)), time.Now()); err != nil {
			t.Fatalf("no cap: %v", err)
		}
	}
	three := 3
	if err := s.SetMachineCapacity(ctx, HostMachineID, &three); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	created, refused := 0, 0
	for i := 2; i < len(items); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateRunInSlot(ctx, items[i].ID, tokenHash(fmt.Sprint("race", i)), time.Now())
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				created++
			case errors.Is(err, ErrNoSlot):
				refused++
			default:
				t.Errorf("run %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if created != 1 || refused != len(items)-3 {
		t.Fatalf("created %d, refused %d; want 1 and %d (2 active + 1 = cap 3)", created, refused, len(items)-3)
	}
	if n, _ := s.CountActiveRuns(ctx, HostMachineID); n != 3 {
		t.Fatalf("active runs = %d, want 3", n)
	}
	// A stale run still holds its slot; an ended one frees it.
	runs, _ := s.ActiveRuns(ctx)
	if _, err := s.TransitionRun(ctx, runs[0].ID, []string{RunStarting}, RunStale, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRunInSlot(ctx, items[19].ID, tokenHash("after-stale"), time.Now()); !errors.Is(err, ErrNoSlot) {
		t.Fatalf("a stale run must hold its slot: %v", err)
	}
	if _, err := s.TransitionRun(ctx, runs[0].ID, []string{RunStale}, RunCancelled, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRunInSlot(ctx, items[19].ID, tokenHash("after-cancel"), time.Now()); err != nil {
		t.Fatalf("an ended run frees its slot: %v", err)
	}
}

// The parallel-queues setting: unset by default, stored per machine, and
// independent of the cap (setting one keeps the other).
func TestParallelQueuesSetting(t *testing.T) {
	ctx := context.Background()
	s, _ := queueFixture(t)
	if v, err := s.ParallelQueuesSetting(ctx, HostMachineID); err != nil || v != nil {
		t.Fatalf("no row: %v %v, want nil (env default)", v, err)
	}
	two := 2
	if err := s.SetMachineCapacity(ctx, HostMachineID, &two); err != nil {
		t.Fatal(err)
	}
	if v, err := s.ParallelQueuesSetting(ctx, HostMachineID); err != nil || v != nil {
		t.Fatalf("cap only: %v %v, want nil", v, err)
	}
	for _, on := range []bool{true, false, true} {
		if err := s.SetParallelQueuesSetting(ctx, HostMachineID, on); err != nil {
			t.Fatal(err)
		}
		if v, err := s.ParallelQueuesSetting(ctx, HostMachineID); err != nil || v == nil || *v != on {
			t.Fatalf("set %v read back as %v, %v", on, v, err)
		}
	}
	if c, err := s.MachineCapacity(ctx, HostMachineID); err != nil || c == nil || *c != 2 {
		t.Fatalf("the switch changed the cap: %v %v", c, err)
	}
	if err := s.SetMachineCapacity(ctx, HostMachineID, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.ParallelQueuesSetting(ctx, HostMachineID); v == nil || !*v {
		t.Fatalf("clearing the cap changed the switch: %v", v)
	}
	if err := s.SetParallelQueuesSetting(ctx, "server-a", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown machine: %v, want ErrNotFound", err)
	}
}
