package queue

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"hostbud/internal/agents"
	"hostbud/internal/events"
	"hostbud/internal/store"
)

type recordingDispatch struct {
	mu     sync.Mutex
	kicks  []string
	ended  []string
	endErr error

	capacityChanges int
}

func (d *recordingDispatch) Kick(queueID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.kicks = append(d.kicks, queueID)
}

func (d *recordingDispatch) CapacityChanged() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.capacityChanges++
}

func (d *recordingDispatch) ResolveApproval(context.Context, string, bool, Actor) error {
	return nil
}

func (d *recordingDispatch) Reverify(context.Context, string) error { return nil }

func (d *recordingDispatch) EndActiveRun(_ context.Context, item store.QueueItem, action string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ended = append(d.ended, item.ID+":"+action)
	return d.endErr
}

type serviceEnv struct {
	st       *memStore
	svc      *Service
	dispatch *recordingDispatch
	events   <-chan events.Event
}

func newServiceEnv(t *testing.T) *serviceEnv {
	t.Helper()
	st := newMemStore()
	st.addProject(store.Project{ID: "project_a", MachineID: store.HostMachineID, Name: "app", Path: "/home/dev/app"})
	bus := events.NewBus()
	ch, cancel := bus.Subscribe(100)
	t.Cleanup(cancel)
	svc := NewService(st, agents.NewRegistry(agents.NewClaude(nil, nil, nil), agents.NewCodex(nil, 0)), bus)
	d := &recordingDispatch{}
	svc.SetDispatcher(d)
	return &serviceEnv{st: st, svc: svc, dispatch: d, events: ch}
}

func (e *serviceEnv) drain() []Changed {
	var out []Changed
	for {
		select {
		case ev := <-e.events:
			if ev.Type == events.QueueChanged {
				out = append(out, ev.Payload.(Changed))
			}
		default:
			return out
		}
	}
}

func queueStatus(err error) int {
	var qe *Error
	if errors.As(err, &qe) {
		return qe.Status
	}
	return 0
}

func TestServiceCreateOneQueueAndEvents(t *testing.T) {
	e := newServiceEnv(t)
	ctx := context.Background()
	if _, err := e.svc.Create(ctx, "project_missing", "Q"); queueStatus(err) != http.StatusBadRequest {
		t.Fatalf("unknown project: %v", err)
	}
	v, err := e.svc.Create(ctx, "project_a", "Milestones")
	if err != nil || v.Status != store.QueueIdle || v.ProjectName != "app" || v.ProjectPath != "/home/dev/app" || v.Items == nil {
		t.Fatalf("create: %+v, %v", v, err)
	}
	if got := e.drain(); len(got) != 1 || got[0].Action != "created" || got[0].Queue == nil || got[0].Queue.ID != v.ID {
		t.Fatalf("events: %+v", got)
	}
	_, err = e.svc.Create(ctx, "project_a", "Second")
	if queueStatus(err) != http.StatusConflict || err.Error() != "V2-M1 supports one queue; several queues arrive with V2-M2" {
		t.Fatalf("second queue: %v", err)
	}
	// Reads never publish.
	if _, err := e.svc.List(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Get(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.drain(); len(got) != 0 {
		t.Fatalf("reads published %+v", got)
	}
	if _, err := e.svc.Get(ctx, "queue_missing"); queueStatus(err) != http.StatusNotFound {
		t.Fatalf("missing queue: %v", err)
	}
}

func TestServiceItemValidationAndEditingRules(t *testing.T) {
	e := newServiceEnv(t)
	ctx := context.Background()
	q, _ := e.svc.Create(ctx, "project_a", "Q")
	for _, bad := range [][3]string{
		{"gemini", "", "/goal x"},
		{"claude", `--model 'x`, "/goal x"},
		{"claude", "", ""},
		{"codex", "", "two\nlines"},
	} {
		if _, err := e.svc.AddItem(ctx, q.ID, bad[0], bad[1], bad[2]); queueStatus(err) != http.StatusBadRequest {
			t.Errorf("AddItem(%q) = %v", bad, err)
		}
	}
	var ids []string
	for i, agent := range []string{"claude", "codex", "claude"} {
		it, err := e.svc.AddItem(ctx, q.ID, agent, "--yolo", "/goal m"+string(rune('1'+i)))
		if err != nil || it.Position != i+1 {
			t.Fatalf("add %d: %+v, %v", i, it, err)
		}
		ids = append(ids, it.ID)
	}
	instr := "/goal m1 and docs"
	if it, err := e.svc.UpdateItem(ctx, ids[0], store.QueueItemUpdate{Instruction: &instr}); err != nil || it.Instruction != instr || it.Agent != "claude" {
		t.Fatalf("edit queued: %+v, %v", it, err)
	}
	bad := ""
	if _, err := e.svc.UpdateItem(ctx, ids[0], store.QueueItemUpdate{Instruction: &bad}); queueStatus(err) != http.StatusBadRequest {
		t.Fatalf("invalid edit: %v", err)
	}
	for _, status := range []string{store.ItemRunning, store.ItemDone, store.ItemNeedsAttention, store.ItemSkipped} {
		_, _ = e.st.TransitionQueueItem(ctx, ids[1], []string{store.ItemQueued, store.ItemRunning, store.ItemDone, store.ItemNeedsAttention, store.ItemSkipped}, status)
		if _, err := e.svc.UpdateItem(ctx, ids[1], store.QueueItemUpdate{Instruction: &instr}); queueStatus(err) != http.StatusConflict {
			t.Errorf("edit %s item: %v", status, err)
		}
		if err := e.svc.DeleteItem(ctx, ids[1]); queueStatus(err) != http.StatusConflict {
			t.Errorf("delete %s item: %v", status, err)
		}
	}
	_, _ = e.st.TransitionQueueItem(ctx, ids[1], []string{store.ItemSkipped}, store.ItemNeedsAttention)
	var qe *Error
	if _, err := e.svc.UpdateItem(ctx, ids[1], store.QueueItemUpdate{Instruction: &instr}); !errors.As(err, &qe) || !strings.Contains(qe.Hint, "Retry") {
		t.Fatalf("needs-attention edit hint: %v", err)
	}
	// Reorder moves only queued items; the others keep their place.
	if _, err := e.svc.Reorder(ctx, q.ID, []string{ids[2], ids[1], ids[0]}); queueStatus(err) != http.StatusConflict {
		t.Fatalf("reorder with a non-queued item: %v", err)
	}
	v, err := e.svc.Reorder(ctx, q.ID, []string{ids[2], ids[0]})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, it := range v.Items {
		order = append(order, it.ID)
	}
	if !slices.Equal(order, []string{ids[2], ids[1], ids[0]}) {
		t.Fatalf("order %v", order)
	}
	if err := e.svc.DeleteItem(ctx, ids[2]); err != nil {
		t.Fatal(err)
	}
	if got := e.drain(); len(got) < 5 {
		t.Fatalf("changes published: %d", len(got))
	}
}

func TestServiceStartPauseResume(t *testing.T) {
	e := newServiceEnv(t)
	ctx := context.Background()
	q, _ := e.svc.Create(ctx, "project_a", "Q")
	if _, err := e.svc.Start(ctx, q.ID); queueStatus(err) != http.StatusConflict {
		t.Fatalf("start empty queue: %v", err)
	}
	if _, err := e.svc.Pause(ctx, q.ID); queueStatus(err) != http.StatusConflict || !strings.Contains(err.Error(), "idle") {
		t.Fatalf("pause idle queue: %v", err)
	}
	if _, err := e.svc.Resume(ctx, q.ID); queueStatus(err) != http.StatusConflict {
		t.Fatalf("resume idle queue: %v", err)
	}
	it, _ := e.svc.AddItem(ctx, q.ID, "claude", "", "/goal m1")
	v, err := e.svc.Start(ctx, q.ID)
	if err != nil || v.Status != store.QueueRunning || !slices.Equal(e.dispatch.kicks, []string{q.ID}) {
		t.Fatalf("start: %+v, %v, kicks %v", v.Status, err, e.dispatch.kicks)
	}
	if _, err := e.svc.Start(ctx, q.ID); queueStatus(err) != http.StatusConflict || !strings.Contains(err.Error(), "running") {
		t.Fatalf("start running queue: %v", err)
	}
	if v, err = e.svc.Pause(ctx, q.ID); err != nil || v.Status != store.QueuePaused {
		t.Fatalf("pause: %v", err)
	}
	if _, err := e.svc.Start(ctx, q.ID); queueStatus(err) != http.StatusConflict || !strings.Contains(err.Error(), "Resume") {
		t.Fatalf("start paused queue: %v", err)
	}
	if v, err = e.svc.Resume(ctx, q.ID); err != nil || v.Status != store.QueueRunning || len(e.dispatch.kicks) != 2 {
		t.Fatalf("resume: %v, kicks %v", err, e.dispatch.kicks)
	}
	// A finished queue starts again once something is queued.
	_, _ = e.st.TransitionQueueItem(ctx, it.ID, []string{store.ItemQueued}, store.ItemDone)
	_, _ = e.st.TransitionQueue(ctx, q.ID, []string{store.QueueRunning}, store.QueueFinished)
	if _, err := e.svc.Start(ctx, q.ID); queueStatus(err) != http.StatusConflict {
		t.Fatalf("start finished queue with nothing queued: %v", err)
	}
	_, _ = e.svc.AddItem(ctx, q.ID, "codex", "", "/goal m2")
	if v, err = e.svc.Start(ctx, q.ID); err != nil || v.Status != store.QueueRunning {
		t.Fatalf("restart finished queue: %v", err)
	}
}

func TestServiceOwnerOverridesOnlyFromNeedsAttention(t *testing.T) {
	e := newServiceEnv(t)
	ctx := context.Background()
	q, _ := e.svc.Create(ctx, "project_a", "Q")
	_, _ = e.st.TransitionQueue(ctx, q.ID, []string{store.QueueIdle}, store.QueuePaused)
	for _, c := range []struct{ action, want string }{
		{ActionRetry, store.ItemQueued}, {ActionSkip, store.ItemSkipped}, {ActionMarkDone, store.ItemDone},
	} {
		it, _ := e.svc.AddItem(ctx, q.ID, "claude", "", "/goal "+c.action)
		if _, err := e.svc.Override(ctx, it.ID, c.action); queueStatus(err) != http.StatusConflict {
			t.Errorf("%s on a queued item: %v", c.action, err)
		}
		_, _ = e.st.TransitionQueueItem(ctx, it.ID, []string{store.ItemQueued}, store.ItemNeedsAttention)
		v, err := e.svc.Override(ctx, it.ID, c.action)
		if err != nil {
			t.Fatalf("%s: %v", c.action, err)
		}
		got, _ := e.st.QueueItem(ctx, it.ID)
		if got.Status != c.want || v.Status != store.QueuePaused {
			t.Errorf("%s: item %s, queue %s", c.action, got.Status, v.Status)
		}
		if !slices.Contains(e.dispatch.ended, it.ID+":"+c.action) {
			t.Errorf("%s: the item's active run wasn't ended first (%v)", c.action, e.dispatch.ended)
		}
	}
	it, _ := e.svc.AddItem(ctx, q.ID, "claude", "", "/goal x")
	if _, err := e.svc.Override(ctx, it.ID, "delete-everything"); queueStatus(err) != http.StatusNotFound {
		t.Fatalf("unknown action: %v", err)
	}
	// If the active run can't be ended, nothing changes.
	_, _ = e.st.TransitionQueueItem(ctx, it.ID, []string{store.ItemQueued}, store.ItemNeedsAttention)
	e.dispatch.endErr = errors.New("database is down")
	if _, err := e.svc.Override(ctx, it.ID, ActionSkip); err == nil {
		t.Fatal("override went ahead after the run couldn't be cancelled")
	}
	if got, _ := e.st.QueueItem(ctx, it.ID); got.Status != store.ItemNeedsAttention {
		t.Fatalf("item %s after a failed override", got.Status)
	}
}

func TestServiceDeleteQueue(t *testing.T) {
	e := newServiceEnv(t)
	ctx := context.Background()
	q, _ := e.svc.Create(ctx, "project_a", "Q")
	it, _ := e.svc.AddItem(ctx, q.ID, "claude", "", "/goal m1")
	run, _ := e.st.CreateRun(ctx, it.ID, HashToken("t"), time.Now())
	err := e.svc.Delete(ctx, q.ID)
	if queueStatus(err) != http.StatusConflict || err.Error() != "A run is still active in this queue — pause it and wait for the run to end, or mark its item done/skip it first" {
		t.Fatalf("delete with an active run: %v", err)
	}
	_, _ = e.st.TransitionRun(ctx, run.ID, store.ActiveRunStatuses, store.RunExited, "", nil)
	e.drain()
	if err := e.svc.Delete(ctx, q.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.drain(); len(got) != 1 || got[0].Action != "deleted" || got[0].Queue != nil {
		t.Fatalf("delete event: %+v", got)
	}
	if _, err := e.svc.Create(ctx, "project_a", "Again"); err != nil {
		t.Fatalf("a new queue after deleting the only one: %v", err)
	}
}
