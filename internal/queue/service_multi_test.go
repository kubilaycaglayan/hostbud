package queue

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"hostbud/internal/store"
)

// V2-M2 T2: several queues behind HOSTBUD_PARALLEL_QUEUES.

func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return slices.Sorted(maps.Keys(m))
}

// With the switch off, create, start and resume answer with V2-M1's fields
// exactly (no warnings key).
func TestSwitchOffKeepsV2M1Responses(t *testing.T) {
	e := newServiceEnv(t)
	ctx := context.Background()
	v2m1 := []string{"createdAt", "id", "items", "machineId", "name", "projectId", "projectName", "projectPath", "status", "updatedAt"}
	q, err := e.svc.Create(ctx, "project_a", "Q")
	if err != nil {
		t.Fatal(err)
	}
	if got := jsonKeys(t, q); !slices.Equal(got, v2m1) {
		t.Fatalf("create keys %v, want %v", got, v2m1)
	}
	_, _ = e.svc.AddItem(ctx, q.ID, "claude", "", "/goal m1")
	v, err := e.svc.Start(ctx, q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := jsonKeys(t, v); !slices.Equal(got, v2m1) {
		t.Fatalf("start keys %v", got)
	}
	_, _ = e.svc.Pause(ctx, q.ID)
	if v, err = e.svc.Resume(ctx, q.ID); err != nil {
		t.Fatal(err)
	}
	if got := jsonKeys(t, v); !slices.Equal(got, v2m1) {
		t.Fatalf("resume keys %v", got)
	}
	if e.svc.ParallelQueues() {
		t.Fatal("the switch is on by default")
	}
}

// Switch on: several queues per project with unique names; an unnamed
// queue takes the first free "Queue n".
func TestSwitchOnSeveralQueuesUniqueNames(t *testing.T) {
	e := newServiceEnv(t)
	e.svc.SetParallelQueues(true)
	ctx := context.Background()
	a, err := e.svc.Create(ctx, "project_a", "Docs")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Create(ctx, "project_a", "Tests"); err != nil {
		t.Fatalf("second queue with the switch on: %v", err)
	}
	var qe *Error
	_, err = e.svc.Create(ctx, "project_a", "docs")
	if queueStatus(err) != http.StatusConflict || !errors.As(err, &qe) || !strings.Contains(qe.Message, `"docs"`) || qe.Hint == "" {
		t.Fatalf("duplicate name: %v", err)
	}
	if _, err := e.svc.Rename(ctx, a.ID, "TESTS"); queueStatus(err) != http.StatusConflict {
		t.Fatalf("rename onto a taken name: %v", err)
	}
	for _, want := range []string{"Queue", "Queue 2", "Queue 3"} {
		v, err := e.svc.Create(ctx, "project_a", "")
		if err != nil || v.Name != want {
			t.Fatalf("unnamed queue: %q, %v; want %q", v.Name, err, want)
		}
	}
}

// Switch off: several queues can still be created (to organize work),
// with unique names and "Queue n" defaults, but only one may run.
func TestSwitchOffCreatesQueuesButRunsOne(t *testing.T) {
	e := newServiceEnv(t)
	ctx := context.Background()
	a, err := e.svc.Create(ctx, "project_a", "Alpha")
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.svc.Create(ctx, "project_a", "Beta")
	if err != nil || b.Status != store.QueueIdle {
		t.Fatalf("second queue with the switch off: %+v, %v", b, err)
	}
	if v, err := e.svc.Create(ctx, "project_a", ""); err != nil || v.Name != "Queue" {
		t.Fatalf("unnamed queue with the switch off: %q, %v", v.Name, err)
	}
	if _, err := e.svc.Create(ctx, "project_a", "beta"); queueStatus(err) != http.StatusConflict {
		t.Fatalf("duplicate name with the switch off: %v", err)
	}
	_, _ = e.svc.AddItem(ctx, a.ID, "claude", "", "/goal a1")
	_, _ = e.svc.AddItem(ctx, b.ID, "claude", "", "/goal b1")
	if _, err := e.svc.Start(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Start(ctx, b.ID); queueStatus(err) != http.StatusConflict || !strings.Contains(err.Error(), "pause queue Alpha") {
		t.Fatalf("start a second queue with the switch off: %v", err)
	}
	if got, _ := e.svc.Get(ctx, b.ID); got.Status != store.QueueIdle {
		t.Fatalf("refused queue status %s", got.Status)
	}
}

// Switch off with queues left over from switch-on: they stay listed and
// editable, but one may start or resume only while no other queue is
// running or has an active run. Nothing is cancelled.
func TestSwitchOffRefusesSecondActiveQueue(t *testing.T) {
	e := newServiceEnv(t)
	ctx := context.Background()
	e.svc.SetParallelQueues(true)
	a, _ := e.svc.Create(ctx, "project_a", "Alpha")
	b, _ := e.svc.Create(ctx, "project_a", "Beta")
	e.svc.SetParallelQueues(false)
	itA, _ := e.svc.AddItem(ctx, a.ID, "claude", "", "/goal a1")
	_, _ = e.svc.AddItem(ctx, b.ID, "claude", "", "/goal b1")
	if list, err := e.svc.List(ctx); err != nil || len(list) != 2 {
		t.Fatalf("leftover queues listed: %d, %v", len(list), err)
	}
	if _, err := e.svc.Rename(ctx, b.ID, "Beta 2"); err != nil {
		t.Fatalf("leftover queue editable: %v", err)
	}
	if _, err := e.svc.Start(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	want := "Parallel queues are off — pause queue Alpha and wait for its run to end, or turn on Run queues in parallel in the Queue panel"
	if _, err := e.svc.Start(ctx, b.ID); queueStatus(err) != http.StatusConflict || err.Error() != want {
		t.Fatalf("start while Alpha runs: %v", err)
	}
	// Alpha paused but its run still active: still refused.
	_, _ = e.st.TransitionQueueItem(ctx, itA.ID, []string{store.ItemQueued}, store.ItemRunning)
	run, _ := e.st.CreateRun(ctx, itA.ID, make([]byte, 32), time.Now())
	if _, err := e.svc.Pause(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Start(ctx, b.ID); queueStatus(err) != http.StatusConflict {
		t.Fatalf("start while Alpha's run is active: %v", err)
	}
	if got := e.st.runs[run.ID].Status; got != store.RunStarting {
		t.Fatalf("Alpha's run was touched: %s", got)
	}
	// The run ends: Beta may start; now Alpha's resume is refused.
	_, _ = e.st.TransitionRun(ctx, run.ID, store.ActiveRunStatuses, store.RunAchieved, "", nil)
	if _, err := e.svc.Start(ctx, b.ID); err != nil {
		t.Fatalf("start after Alpha's run ended: %v", err)
	}
	if _, err := e.svc.Resume(ctx, a.ID); queueStatus(err) != http.StatusConflict || !strings.Contains(err.Error(), "pause queue Beta 2") {
		t.Fatalf("resume Alpha while Beta runs: %v", err)
	}
	// With the switch on the same resume goes through.
	e.svc.SetParallelQueues(true)
	if _, err := e.svc.Resume(ctx, a.ID); err != nil {
		t.Fatalf("resume with the switch on: %v", err)
	}
}

func warningOf(v View) []string {
	var out []string
	for _, w := range v.Warnings {
		for _, q := range w.Queues {
			out = append(out, w.Code+":"+q.Name)
		}
	}
	return out
}

// Two busy queues whose projects resolve to one path warn on both; it
// never blocks. Idle queues and other paths don't warn.
func TestSharedDirectoryWarning(t *testing.T) {
	for _, tc := range []struct {
		name, otherPath string
		sameProject     bool
		warn            bool
	}{
		{name: "same project", sameProject: true, warn: true},
		{name: "two projects, one path", otherPath: "/home/dev/tools/../app/", warn: true},
		{name: "different paths", otherPath: "/home/dev/other", warn: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newServiceEnv(t)
			e.svc.SetParallelQueues(true)
			ctx := context.Background()
			project := "project_a"
			if !tc.sameProject {
				project = "project_b"
				e.st.addProject(store.Project{ID: project, MachineID: store.HostMachineID, Name: "b", Path: tc.otherPath})
			}
			a, _ := e.svc.Create(ctx, "project_a", "Alpha")
			b, _ := e.svc.Create(ctx, project, "Beta")
			for _, q := range []View{a, b} {
				_, _ = e.svc.AddItem(ctx, q.ID, "claude", "", "/goal "+q.Name)
			}
			va, err := e.svc.Start(ctx, a.ID)
			if err != nil || len(va.Warnings) != 0 {
				t.Fatalf("first start: warnings %v, %v", va.Warnings, err)
			}
			e.drain()
			vb, err := e.svc.Start(ctx, b.ID)
			if err != nil {
				t.Fatalf("second start is never blocked: %v", err)
			}
			if got, want := len(vb.Warnings) > 0, tc.warn; got != want {
				t.Fatalf("start response warned=%v, want %v (%v)", got, want, vb.Warnings)
			}
			list, _ := e.svc.List(ctx)
			for _, v := range list {
				if got := len(v.Warnings) > 0; got != tc.warn {
					t.Errorf("list: %s warned=%v, want %v", v.Name, got, tc.warn)
				}
			}
			if !tc.warn {
				return
			}
			if got := warningOf(vb); !slices.Equal(got, []string{"shared_directory:Alpha"}) {
				t.Errorf("Beta's warning %v", got)
			}
			if !strings.Contains(vb.Warnings[0].Message, "/home/dev/app") {
				t.Errorf("message %q doesn't name the directory", vb.Warnings[0].Message)
			}
			// Alpha's panel hears about it through queue.changed.
			var alphaWarned bool
			for _, c := range e.drain() {
				if c.QueueID == a.ID && c.Queue != nil && len(c.Queue.Warnings) > 0 {
					alphaWarned = true
				}
			}
			if !alphaWarned {
				t.Error("no queue.changed told Alpha about the shared directory")
			}
			// Pausing Beta (no active run) clears both warnings.
			if _, err := e.svc.Pause(ctx, b.ID); err != nil {
				t.Fatal(err)
			}
			if got, _ := e.svc.Get(ctx, a.ID); len(got.Warnings) != 0 {
				t.Errorf("Alpha still warned after Beta paused: %v", got.Warnings)
			}
		})
	}
}

// The owner's switch (Queue panel): stored, applied at once, loaded over
// the env default at startup; every queue hears the new value; switching
// on hands out slots, switching off stops nothing.
func TestSetParallelStoresAndPublishes(t *testing.T) {
	e := newServiceEnv(t)
	ctx := context.Background()
	a, _ := e.svc.Create(ctx, "project_a", "Alpha")
	e.drain()
	if on, err := e.svc.SetParallel(ctx, true); err != nil || !on || !e.svc.ParallelQueues() {
		t.Fatalf("switch on: %v %v", on, err)
	}
	if got := e.drain(); len(got) != 1 || got[0].QueueID != a.ID || !got[0].ParallelQueues {
		t.Fatalf("queue.changed after switch on: %+v", got)
	}
	if e.dispatch.capacityChanges != 1 {
		t.Fatalf("switch on handed out slots %d times, want 1", e.dispatch.capacityChanges)
	}
	if _, err := e.svc.Create(ctx, "project_a", "Beta"); err != nil {
		t.Fatalf("second queue: %v", err)
	}
	_, _ = e.svc.AddItem(ctx, a.ID, "claude", "", "/goal a1")
	if _, err := e.svc.Start(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if on, err := e.svc.SetParallel(ctx, false); err != nil || on || e.svc.ParallelQueues() {
		t.Fatalf("switch off: %v %v", on, err)
	}
	got := e.drain()
	if len(got) != 2 || got[0].ParallelQueues || got[1].ParallelQueues {
		t.Fatalf("queue.changed after switch off: %+v", got)
	}
	if q, _ := e.svc.Get(ctx, a.ID); q.Status != store.QueueRunning {
		t.Fatalf("switch off changed a running queue: %s", q.Status)
	}
	if e.dispatch.capacityChanges != 1 {
		t.Fatalf("switch off handed out slots")
	}

	// A restart: the env default is on, the stored switch (off) wins.
	restarted := NewService(e.st, nil, nil)
	restarted.SetParallelQueues(true)
	if err := restarted.LoadParallelQueues(ctx); err != nil || restarted.ParallelQueues() {
		t.Fatalf("stored off over env on: %v %v", restarted.ParallelQueues(), err)
	}
	// Nothing stored: the env default holds.
	fresh := NewService(newMemStore(), nil, nil)
	fresh.SetParallelQueues(true)
	if err := fresh.LoadParallelQueues(ctx); err != nil || !fresh.ParallelQueues() {
		t.Fatalf("env default lost: %v %v", fresh.ParallelQueues(), err)
	}
}

// A queue's default prompt: off with ", commit regularly." until set; the
// view carries it only once it differs; stored per queue, validated, and
// published as the queue's queue.changed.
func TestSetDefaultPromptStoresAndPublishes(t *testing.T) {
	e := newServiceEnv(t)
	ctx := context.Background()
	a, _ := e.svc.Create(ctx, "project_a", "Alpha")
	b, _ := e.svc.Create(ctx, "project_a", "Beta")
	if a.DefaultPrompt != nil {
		t.Fatalf("new queue: %+v, want none (the default)", a.DefaultPrompt)
	}
	e.drain()
	want := store.DefaultPrompt{Enabled: true, Text: ", commit regularly."}
	v, err := e.svc.SetDefaultPrompt(ctx, a.ID, want)
	if err != nil || v.DefaultPrompt == nil || *v.DefaultPrompt != want {
		t.Fatalf("set: %+v %v", v.DefaultPrompt, err)
	}
	// Beta shares the directory, so it hears a peer_changed as well.
	if got := e.drain(); len(got) == 0 || got[0].Action != "default_prompt_changed" || got[0].QueueID != a.ID || got[0].Queue == nil || *got[0].Queue.DefaultPrompt != want {
		t.Fatalf("queue.changed after set: %+v", got)
	}
	if other, _ := e.svc.Get(ctx, b.ID); other.DefaultPrompt != nil {
		t.Fatalf("Beta changed: %+v", other.DefaultPrompt)
	}
	_, err = e.svc.SetDefaultPrompt(ctx, a.ID, store.DefaultPrompt{Enabled: true, Text: "two\nlines"})
	var qe *Error
	if !errors.As(err, &qe) || qe.Status != http.StatusBadRequest {
		t.Fatalf("two lines: %v, want a 400", err)
	}
	if _, err := e.svc.SetDefaultPrompt(ctx, "queue_missing", want); !errors.As(err, &qe) || qe.Status != http.StatusNotFound {
		t.Fatalf("unknown queue: %v, want a 404", err)
	}
	if got := e.drain(); len(got) != 0 {
		t.Fatalf("a refused prompt published %+v", got)
	}
}
