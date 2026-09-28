package queue

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"hostbud/internal/agents"
	"hostbud/internal/events"
	"hostbud/internal/store"
)

// V2-M2 T4: the dispatcher with slots (HOSTBUD_PARALLEL_QUEUES=true), a
// fake clock and the fake session layer.

type slotEnv struct {
	*dispEnv
	queues map[string]View // by name
}

// newSlotEnv makes queues named by names on project_a, each with n items.
func newSlotEnv(t *testing.T, capacity *int, n int, names ...string) *slotEnv {
	t.Helper()
	e := &slotEnv{dispEnv: newDispEnv(t), queues: map[string]View{}}
	e.svc.SetParallelQueues(true)
	if capacity != nil {
		if _, err := e.svc.SetCapacity(e.ctx(), capacity); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range names {
		q, err := e.svc.Create(e.ctx(), "project_a", name)
		if err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= n; i++ {
			if _, err := e.svc.AddItem(e.ctx(), q.ID, "claude", "", fmt.Sprintf("/goal %s%d", name, i)); err != nil {
				t.Fatal(err)
			}
		}
		e.queues[name] = q
	}
	e.d.Sync()
	return e
}

func intp(n int) *int { return &n }

func (e *slotEnv) start(name string) {
	e.t.Helper()
	if _, err := e.svc.Start(e.ctx(), e.queues[name].ID); err != nil {
		e.t.Fatal(err)
	}
	e.d.Sync()
}

// nameOf maps a run to its queue's name.
func (e *slotEnv) nameOf(r store.Run) string {
	it, _ := e.st.QueueItem(e.ctx(), r.ItemID)
	for name, q := range e.queues {
		if q.ID == it.QueueID {
			return name
		}
	}
	return "?"
}

// startOrder lists the queues of every run so far, in creation order.
func (e *slotEnv) startOrder() []string {
	e.st.mu.Lock()
	order := slices.Clone(e.st.order)
	e.st.mu.Unlock()
	var out []string
	for _, id := range order {
		r, _ := e.st.Run(e.ctx(), id)
		out = append(out, e.nameOf(r))
	}
	return out
}

// active returns the queue names of the active runs.
func (e *slotEnv) active() []string {
	runs, _ := e.st.ActiveRuns(e.ctx())
	var out []string
	for _, r := range runs {
		out = append(out, e.nameOf(r))
	}
	slices.Sort(out)
	return out
}

func (e *slotEnv) activeRun(name string) store.Run {
	e.t.Helper()
	runs, _ := e.st.ActiveRuns(e.ctx())
	for _, r := range runs {
		if e.nameOf(r) == name {
			return r
		}
	}
	e.t.Fatalf("queue %s has no active run (active: %v)", name, e.active())
	return store.Run{}
}

// achieve binds the queue's active run and reports its goal achieved.
func (e *slotEnv) achieve(name string) {
	e.t.Helper()
	r := e.activeRun(name)
	session := "s-" + r.ID
	e.claude.set(session, agents.Achieved)
	e.hook(r, EventSessionStart, session)
	e.hook(r, EventTurnEnd, session)
}

func (e *slotEnv) waitingFor(name string) bool {
	v, err := e.svc.Get(e.ctx(), e.queues[name].ID)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, it := range v.Items {
		if it.WaitingForSlot {
			return true
		}
	}
	return false
}

// Cap 1, three queues started A, B, C with two items each: they take
// turns (A, B, C, A, B, C); none waits twice in a row.
func TestSlotsCapOneTakesTurns(t *testing.T) {
	e := newSlotEnv(t, intp(1), 2, "A", "B", "C")
	for _, name := range []string{"A", "B", "C"} {
		e.start(name)
	}
	if got := e.active(); !slices.Equal(got, []string{"A"}) {
		t.Fatalf("active %v, want [A]", got)
	}
	if !e.waitingFor("B") || !e.waitingFor("C") || e.waitingFor("A") {
		t.Fatal("B and C should wait for a free slot, A shouldn't")
	}
	for _, name := range []string{"A", "B", "C", "A", "B", "C"} {
		if got := e.active(); !slices.Equal(got, []string{name}) {
			t.Fatalf("active %v, want [%s] (order so far %v)", got, name, e.startOrder())
		}
		e.achieve(name)
	}
	if got := e.startOrder(); !slices.Equal(got, []string{"A", "B", "C", "A", "B", "C"}) {
		t.Fatalf("start order %v", got)
	}
	for _, name := range []string{"A", "B", "C"} {
		if q, _ := e.st.Queue(e.ctx(), e.queues[name].ID); q.Status != store.QueueFinished || q.WaitingSince != nil {
			t.Errorf("queue %s: %s, waiting since %v", name, q.Status, q.WaitingSince)
		}
	}
	if e.st.maxActive != 1 {
		t.Fatalf("max active runs %d, cap 1", e.st.maxActive)
	}
}

// No cap: every queue runs at once, each one sequential.
func TestSlotsNoCapRunsAllQueues(t *testing.T) {
	e := newSlotEnv(t, nil, 2, "A", "B", "C")
	for _, name := range []string{"A", "B", "C"} {
		e.start(name)
	}
	if got := e.active(); !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Fatalf("active %v", got)
	}
	e.achieve("B")
	if got := e.active(); !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Fatalf("after B achieved: active %v (B's second item should run)", got)
	}
	items, _ := e.st.QueueItems(e.ctx(), e.queues["A"].ID)
	if items[0].Status != store.ItemRunning || items[1].Status != store.ItemQueued {
		t.Fatalf("A isn't sequential: %s, %s", items[0].Status, items[1].Status)
	}
}

// A stale run holds its slot; only an owner action (or a late achieved)
// frees it. Failed and exited runs free it at once.
func TestSlotsStaleHoldsOwnerActionFrees(t *testing.T) {
	e := newSlotEnv(t, intp(1), 1, "A", "B")
	e.start("A")
	e.start("B")
	e.advance(staleAfter + time.Second)
	a := e.activeRun("A")
	if a.Status != store.RunStale {
		t.Fatalf("A's run is %s, want stale", a.Status)
	}
	if got := e.active(); !slices.Equal(got, []string{"A"}) || !e.waitingFor("B") {
		t.Fatalf("B must keep waiting while A's stale run holds the slot (active %v)", got)
	}
	items, _ := e.st.QueueItems(e.ctx(), e.queues["A"].ID)
	if _, err := e.svc.Override(e.ctx(), items[0].ID, ActionSkip); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if got := e.active(); !slices.Equal(got, []string{"B"}) || e.waitingFor("B") {
		t.Fatalf("Skip on A should free the slot for B (active %v)", got)
	}

	// A failed run frees its slot at once.
	f := newSlotEnv(t, intp(1), 1, "A", "B")
	f.start("A")
	f.start("B")
	r := f.activeRun("A")
	f.claude.set("s-fail", agents.Failed)
	f.hook(r, EventSessionStart, "s-fail")
	f.hook(r, EventTurnEnd, "s-fail")
	if got := f.active(); !slices.Equal(got, []string{"B"}) {
		t.Fatalf("after A failed: active %v, want [B]", got)
	}

	// A late achieved on a stale run frees the slot; its queue stays paused.
	g := newSlotEnv(t, intp(1), 2, "A", "B")
	g.start("A")
	g.start("B")
	r = g.activeRun("A")
	g.hook(r, EventSessionStart, "s-late")
	g.advance(staleAfter + time.Second)
	g.claude.set("s-late", agents.Achieved)
	if _, err := g.svc.Resume(g.ctx(), g.queues["A"].ID); err == nil {
		// Resume reads the stale run once: late achieved.
		g.d.Sync()
	}
	if got := g.active(); !slices.Contains(got, "B") {
		t.Fatalf("after A's late achieved: active %v, want B running", got)
	}
}

// Lowering the cap stops nothing; raising or clearing it dispatches at once.
func TestSlotsCapChanges(t *testing.T) {
	e := newSlotEnv(t, nil, 2, "A", "B", "C")
	for _, name := range []string{"A", "B", "C"} {
		e.start(name)
	}
	if _, err := e.svc.SetCapacity(e.ctx(), intp(1)); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if got := e.active(); len(got) != 3 {
		t.Fatalf("lowering the cap stopped runs: active %v", got)
	}
	for _, r := range e.st.runs {
		if r.Status == store.RunCancelled {
			t.Fatal("lowering the cap cancelled a run")
		}
	}
	e.achieve("A")
	e.achieve("B")
	if got := e.active(); !slices.Equal(got, []string{"C"}) || !e.waitingFor("A") || !e.waitingFor("B") {
		t.Fatalf("over the cap nothing new starts: active %v", got)
	}
	if _, err := e.svc.SetCapacity(e.ctx(), intp(2)); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if got := e.active(); !slices.Equal(got, []string{"A", "C"}) {
		t.Fatalf("raising the cap to 2: active %v, want A (waited longest) and C", got)
	}
	if _, err := e.svc.SetCapacity(e.ctx(), nil); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if got := e.active(); !slices.Equal(got, []string{"A", "B", "C"}) {
		t.Fatalf("clearing the cap: active %v", got)
	}
	for _, bad := range []int{0, 33} {
		if _, err := e.svc.SetCapacity(e.ctx(), intp(bad)); queueStatus(err) != 400 {
			t.Errorf("cap %d: %v, want 400", bad, err)
		}
	}
}

// The waiting state is derived and published: queue.changed carries
// waitingForSlot when a queue starts waiting and when it gets its slot.
func TestSlotsPublishWaitingChanges(t *testing.T) {
	e := newSlotEnv(t, intp(1), 1, "A", "B")
	ch, cancel := e.bus.Subscribe(1000)
	defer cancel()
	e.start("A")
	e.start("B")
	e.achieve("A")
	var states []bool
	for {
		select {
		case ev := <-ch:
			if c, ok := ev.Payload.(Changed); ok && ev.Type == events.QueueChanged && c.QueueID == e.queues["B"].ID && c.Queue != nil && c.Queue.Status == store.QueueRunning && len(c.Queue.Items) > 0 {
				w := c.Queue.Items[0].WaitingForSlot
				if len(states) == 0 || states[len(states)-1] != w {
					states = append(states, w)
				}
			}
			continue
		default:
		}
		break
	}
	if !slices.Equal(states, []bool{true, false}) {
		t.Fatalf("B's published waitingForSlot went %v, want [true false]", states)
	}
}

// After a restart with runs active, the count includes them before any
// dispatch, and the waiting order survives.
func TestSlotsRestartKeepsCountAndOrder(t *testing.T) {
	e := newSlotEnv(t, intp(1), 1, "A", "B", "C")
	e.start("A")
	e.start("B")
	e.start("C")
	e.stop()
	e.dispEnv.start()
	if got := e.active(); !slices.Equal(got, []string{"A"}) {
		t.Fatalf("after restart: active %v, want [A]", got)
	}
	e.achieve("A")
	if got := e.active(); !slices.Equal(got, []string{"B"}) {
		t.Fatalf("after A: active %v, want B (waited before C)", got)
	}
	if e.st.maxActive != 1 {
		t.Fatalf("max active %d over cap 1", e.st.maxActive)
	}
}

// 50 concurrent signals (turn ends and session ends from many goroutines)
// never push the machine over its cap.
func TestSlotsConcurrentSignalsNeverExceedTheCap(t *testing.T) {
	names := []string{"A", "B", "C", "D", "E", "F"}
	e := newSlotEnv(t, intp(2), 5, names...)
	for _, name := range names {
		e.start(name)
	}
	e.claude.mu.Lock()
	e.claude.def = agents.Achieved // every bound session reads as achieved
	e.claude.mu.Unlock()
	for round := 0; round < 20; round++ {
		runs, _ := e.st.ActiveRuns(e.ctx())
		if len(runs) == 0 {
			break
		}
		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			r := runs[(i*7+round)%len(runs)]
			wg.Add(1)
			go func() {
				defer wg.Done()
				event := []string{EventSessionStart, EventTurnEnd, EventTurnEnd}[(i+round)%3]
				e.d.Notify(Signal{RunID: r.ID, Event: event, Body: []byte(`{"session_id":"s-` + r.ID + `"}`), At: e.clock.Now()})
			}()
		}
		wg.Wait()
		e.d.Sync()
		if n, _ := e.st.CountActiveRuns(e.ctx(), store.HostMachineID); n > 2 {
			t.Fatalf("round %d: %d active runs over cap 2", round, n)
		}
	}
	if e.st.maxActive > 2 {
		t.Fatalf("max active runs %d over cap 2", e.st.maxActive)
	}
	if got := len(e.startOrder()); got < 10 {
		t.Fatalf("only %d runs started; the queues didn't advance", got)
	}
}
