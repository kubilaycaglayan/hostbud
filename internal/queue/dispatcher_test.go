package queue

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"hostbud/internal/agents"
	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/store"
	"hostbud/internal/tmux"
)

// ---- fake clock ----

type fakeTimer struct {
	c       *fakeClock
	at      time.Time
	f       func()
	stopped bool
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := !t.stopped
	t.stopped = true
	return was
}

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return t
}

// advance moves time forward, firing due timers in order.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	end := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		sort.SliceStable(c.timers, func(i, j int) bool { return c.timers[i].at.Before(c.timers[j].at) })
		var due *fakeTimer
		for _, t := range c.timers {
			if !t.stopped && !t.at.After(end) {
				due = t
				break
			}
		}
		if due == nil {
			c.now = end
			c.mu.Unlock()
			return
		}
		due.stopped = true
		if due.at.After(c.now) {
			c.now = due.at
		}
		c.mu.Unlock()
		due.f()
	}
}

// ---- fake adapter ----

// scriptAdapter answers ReadGoalState from a per-session script.
type scriptAdapter struct {
	mu      sync.Mutex
	kind    string
	states  map[string]agents.GoalState // by agent session id
	readErr error
	armErr  error
	version string
	verErr  error
	reads   int
	armed   []string
	def     string        // the state of sessions without a script (default pending)
	usage   *agents.Usage // added to a run's totals on each usage read
}

func newScriptAdapter(kind string) *scriptAdapter {
	return &scriptAdapter{kind: kind, states: map[string]agents.GoalState{}, version: "2.1.283"}
}

func (a *scriptAdapter) Kind() string       { return a.kind }
func (a *scriptAdapter) MinVersion() string { return "2.1.283" }
func (a *scriptAdapter) CheckVersion(context.Context, string) (string, error) {
	return a.version, a.verErr
}
func (a *scriptAdapter) BuildCommand(item store.QueueItem, _ store.Run) ([]string, error) {
	return []string{a.kind, item.Instruction}, nil
}
func (a *scriptAdapter) ParseHook(_ string, body []byte) (agents.Binding, error) {
	var h struct {
		SessionID      string `json:"session_id"`
		TranscriptPath string `json:"transcript_path"`
		Reason         string `json:"reason"`
	}
	if err := json.Unmarshal(body, &h); err != nil || h.SessionID == "" {
		return agents.Binding{}, errors.New("no session id")
	}
	return agents.Binding{SessionID: h.SessionID, TranscriptPath: h.TranscriptPath, Reason: h.Reason}, nil
}
func (a *scriptAdapter) Arm(_ context.Context, _ string, b agents.Binding, _ store.Run, condition string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.armed = append(a.armed, b.SessionID+"="+condition)
	return a.armErr
}
func (a *scriptAdapter) ReadGoalState(_ context.Context, _ string, b agents.Binding, run store.Run, _ string) (agents.GoalState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reads++
	if a.readErr != nil {
		return agents.GoalState{}, a.readErr
	}
	st, ok := a.states[b.SessionID]
	if !ok {
		if a.def != "" {
			return agents.GoalState{Status: a.def}, nil
		}
		return agents.GoalState{Status: agents.Pending, Offset: run.TranscriptOffset}, nil
	}
	return st, nil
}
func (a *scriptAdapter) ReadUsage(_ context.Context, _ string, _ agents.Binding, run store.Run) (agents.Usage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u := agents.Usage{InputTokens: run.InputTokens, OutputTokens: run.OutputTokens, Offset: run.UsageOffset}
	if a.usage != nil {
		u.InputTokens += a.usage.InputTokens
		u.OutputTokens += a.usage.OutputTokens
		u.Offset += a.usage.Offset
	}
	return u, nil
}
func (a *scriptAdapter) set(session, status string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.states[session] = agents.GoalState{Status: status, Reason: "scripted"}
}

type adapterMap map[string]agents.Adapter

func (m adapterMap) Get(kind string) agents.Adapter { return m[kind] }

// ---- harness ----

const staleAfter = time.Hour

type dispEnv struct {
	t        *testing.T
	st       *memStore
	clock    *fakeClock
	claude   *scriptAdapter
	sessions *fakeSessions
	svc      *Service
	d        *Dispatcher
	bus      *events.Bus
	runEvts  <-chan events.Event
	cancel   context.CancelFunc
	done     chan struct{}
	queue    View
}

func newDispEnv(t *testing.T, conditions ...string) *dispEnv {
	t.Helper()
	e := &dispEnv{t: t, st: newMemStore(), clock: &fakeClock{now: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)},
		claude: newScriptAdapter("claude"), sessions: &fakeSessions{}, bus: events.NewBus()}
	e.st.addProject(store.Project{ID: "project_a", MachineID: store.HostMachineID, Name: "app", Path: "/home/dev/app"})
	e.runEvts, _ = e.bus.Subscribe(1000)
	e.svc = NewService(e.st, agents.NewRegistry(agents.NewClaude(nil, nil, nil), agents.NewCodex(nil, 0)), e.bus)
	starter := NewStarter(e.st, e.sessions, "http://127.0.0.1:9055", nil)
	e.d = NewDispatcher(e.st, adapterMap{"claude": e.claude, "codex": e.claude}, starter, e.svc, e.bus, staleAfter, nil)
	e.d.SetClock(e.clock)
	e.st.now = e.clock.Now
	var err error
	if e.queue, err = e.svc.Create(context.Background(), "project_a", "Q"); err != nil {
		t.Fatal(err)
	}
	for _, c := range conditions {
		if _, err := e.svc.AddItem(context.Background(), e.queue.ID, "claude", "", "/goal "+c); err != nil {
			t.Fatal(err)
		}
	}
	e.start()
	return e
}

func (e *dispEnv) start() {
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel, e.done = cancel, make(chan struct{})
	go func() { e.d.Run(ctx); close(e.done) }()
	e.t.Cleanup(e.stop)
	e.d.Sync()
}

func (e *dispEnv) stop() {
	if e.cancel != nil {
		e.cancel()
		<-e.done
		e.cancel = nil
	}
}

func (e *dispEnv) ctx() context.Context { return context.Background() }

func (e *dispEnv) items() []store.QueueItem {
	items, _ := e.st.QueueItems(e.ctx(), e.queue.ID)
	return items
}

func (e *dispEnv) itemStatuses() []string {
	var out []string
	for _, it := range e.items() {
		out = append(out, it.Status)
	}
	return out
}

func (e *dispEnv) queueStatus() string {
	q, _ := e.st.Queue(e.ctx(), e.queue.ID)
	return q.Status
}

// run returns the latest run of item n (1-based).
func (e *dispEnv) run(n int) store.Run {
	e.t.Helper()
	runs, _ := e.st.LatestRuns(e.ctx(), e.queue.ID)
	r, ok := runs[e.items()[n-1].ID]
	if !ok {
		e.t.Fatalf("item %d has no run", n)
	}
	return r
}

func (e *dispEnv) activeRuns() int {
	runs, _ := e.st.ActiveRuns(e.ctx())
	return len(runs)
}

func (e *dispEnv) hook(run store.Run, event, session string, extra ...string) {
	body := map[string]string{"session_id": session, "transcript_path": "/home/dev/.claude/projects/x/" + session + ".jsonl"}
	for i := 0; i+1 < len(extra); i += 2 {
		body[extra[i]] = extra[i+1]
	}
	b, _ := json.Marshal(body)
	e.d.Notify(Signal{RunID: run.ID, Event: event, Body: b, At: e.clock.Now()})
	e.d.Sync()
}

func (e *dispEnv) advance(d time.Duration) {
	e.clock.advance(d)
	e.d.Sync()
}

func (e *dispEnv) startQueue() {
	e.t.Helper()
	if _, err := e.svc.Start(e.ctx(), e.queue.ID); err != nil {
		e.t.Fatal(err)
	}
	e.d.Sync()
}

func (e *dispEnv) runEventKinds(run store.Run) []string {
	return e.st.eventKinds(run.ID)
}

func (e *dispEnv) runChanged() []string {
	var out []string
	for {
		select {
		case ev := <-e.runEvts:
			if ev.Type == events.RunChanged {
				p := ev.Payload.(RunChanged)
				out = append(out, p.Status)
			}
		default:
			return out
		}
	}
}

// ---- tests ----

func TestDispatcherRunsItemsInOrder(t *testing.T) {
	e := newDispEnv(t, "m1", "m2", "m3")
	e.startQueue()
	for n := 1; n <= 3; n++ {
		r := e.run(n)
		if r.Status != store.RunStarting || e.activeRuns() != 1 || r.SessionName != RunSessionName("app", "", n) {
			t.Fatalf("item %d: run %+v, %d active", n, r, e.activeRuns())
		}
		want := make([]string, 3)
		for i := range want {
			switch {
			case i < n-1:
				want[i] = store.ItemDone
			case i == n-1:
				want[i] = store.ItemRunning
			default:
				want[i] = store.ItemQueued
			}
		}
		if got := e.itemStatuses(); !slices.Equal(got, want) {
			t.Fatalf("item %d: statuses %v, want %v", n, got, want)
		}
		sid := "sess-" + string(rune('0'+n))
		e.hook(r, EventSessionStart, sid)
		if got := e.run(n).Status; got != store.RunRunning {
			t.Fatalf("after session_start: %s", got)
		}
		// Claude's verdict lands after the Stop hook: the turn_end read is
		// pending, a follow-up read two seconds later sees achieved.
		e.hook(r, EventTurnEnd, sid)
		if got := e.run(n).Status; got != store.RunRunning || e.activeRuns() != 1 {
			t.Fatalf("pending turn: %s", got)
		}
		e.claude.set(sid, agents.Achieved)
		e.advance(2 * time.Second)
		if got := e.run(n).Status; got != store.RunAchieved {
			t.Fatalf("follow-up read: %s", got)
		}
		if n < 3 && e.activeRuns() != 1 {
			t.Fatalf("next item didn't start after item %d", n)
		}
	}
	if e.queueStatus() != store.QueueFinished || !slices.Equal(e.itemStatuses(), []string{"done", "done", "done"}) || e.activeRuns() != 0 {
		t.Fatalf("end: queue %s, items %v", e.queueStatus(), e.itemStatuses())
	}
	if got := e.runEventKinds(e.run(1)); !slices.Equal(got, []string{"user:starting", "hook:running", "timer:achieved"}) {
		t.Fatalf("run events %v", got)
	}
	if len(e.sessions.specs) != 3 {
		t.Fatalf("sessions created: %d", len(e.sessions.specs))
	}
}

func TestPlainClaudePromptPausesForManualCompletion(t *testing.T) {
	e := newDispEnv(t)
	if _, err := e.svc.AddItem(context.Background(), e.queue.ID, "claude", "", "implement the requested change"); err != nil {
		t.Fatal(err)
	}
	e.startQueue()
	r := e.run(1)
	e.hook(r, EventSessionStart, "plain-session")
	e.hook(r, EventTurnEnd, "plain-session")
	got, err := e.st.Run(context.Background(), r.ID)
	if err != nil || got.Status != store.RunExited || !strings.Contains(got.Detail, "Review the work") {
		t.Fatalf("plain Claude run = %+v, %v", got, err)
	}
	if got := e.itemStatuses(); !slices.Equal(got, []string{store.ItemNeedsAttention}) || e.queueStatus() != store.QueuePaused || e.activeRuns() != 0 {
		t.Fatalf("plain Claude result: items %v, queue %s, active runs %d", got, e.queueStatus(), e.activeRuns())
	}
}

// Each turn end records the run's token usage and publishes it, before a
// plain prompt's run ends; the queue view carries the totals.
func TestDispatcherRecordsTokenUsage(t *testing.T) {
	e := newDispEnv(t, "m1")
	if _, err := e.svc.AddItem(context.Background(), e.queue.ID, "claude", "", "implement the requested change"); err != nil {
		t.Fatal(err)
	}
	e.claude.usage = &agents.Usage{InputTokens: 1000, OutputTokens: 50, Offset: 10}
	e.startQueue()
	r := e.run(1)
	e.hook(r, EventSessionStart, "s1")
	e.runChanged()
	e.hook(r, EventTurnEnd, "s1") // pending goal: usage only
	if got := e.run(1); got.Status != store.RunRunning || got.InputTokens != 1000 || got.OutputTokens != 50 || got.UsageOffset != 10 {
		t.Fatalf("after one turn: %+v", got)
	}
	var last RunChanged
	for {
		select {
		case ev := <-e.runEvts:
			if ev.Type == events.RunChanged {
				last = ev.Payload.(RunChanged)
			}
			continue
		default:
		}
		break
	}
	if last.InputTokens != 1000 || last.OutputTokens != 50 {
		t.Fatalf("run.changed = %+v", last)
	}
	e.claude.set("s1", agents.Achieved)
	e.hook(r, EventTurnEnd, "s1")
	if got := e.run(1); got.Status != store.RunAchieved || got.InputTokens != 2000 || got.OutputTokens != 100 {
		t.Fatalf("after achieved: %+v", got)
	}
	r2 := e.run(2)
	e.hook(r2, EventSessionStart, "s2")
	e.hook(r2, EventTurnEnd, "s2") // a plain prompt ends its run
	views, err := e.svc.List(e.ctx())
	if err != nil {
		t.Fatal(err)
	}
	run := views[0].Items[1].Run
	if run == nil || run.Status != store.RunExited || run.InputTokens != 1000 || run.OutputTokens != 50 {
		t.Fatalf("plain run summary: %+v", run)
	}
}

// V2-M6 T2: a dependent queue remains queued while its predecessor is
// active, then starts immediately when the predecessor reports achieved.
func TestQueueDependencyWaitsForTrackedGoal(t *testing.T) {
	e := newDispEnv(t, "predecessor")
	e.svc.SetParallelQueues(true)
	e.startQueue()
	prior := e.run(1)
	e.hook(prior, EventSessionStart, "session-prior")
	dependent, err := e.svc.Create(e.ctx(), "project_a", "Dependent", prior.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dependent.AfterRunID == nil || *dependent.AfterRunID != prior.ID {
		t.Fatalf("dependency = %v, want %s", dependent.AfterRunID, prior.ID)
	}
	predecessor, err := e.st.Run(e.ctx(), prior.ID)
	if err != nil || predecessor.Status == store.RunAchieved {
		t.Fatalf("predecessor before start = %+v, %v", predecessor, err)
	}
	_, err = e.svc.AddItem(e.ctx(), dependent.ID, "claude", "", "/goal dependent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Start(e.ctx(), dependent.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	view, err := e.svc.Get(e.ctx(), dependent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := view.Items[0].Status; got != store.ItemQueued {
		t.Fatalf("dependent item started early: %s", got)
	}
	e.claude.set("session-prior", agents.Achieved)
	e.hook(prior, EventTurnEnd, "session-prior")
	e.d.Sync()
	view, err = e.svc.Get(e.ctx(), dependent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := view.Items[0].Status; got != store.ItemRunning {
		t.Fatalf("dependent item status after achieved: %s", got)
	}
}

// A queue linked to any existing session (not a queue run) waits while that
// session's agent works, starts once it is idle, and the link gates only the
// first item.
func TestQueueDependencyWaitsForExistingSession(t *testing.T) {
	e := newDispEnv(t)
	e.svc.SetParallelQueues(true)
	snapshot := func(sessions ...tmux.Session) {
		e.d.enqueue(func(ctx context.Context) {
			e.d.sessionsChanged(ctx, store.HostMachineID, inventory.SessionsChanged{Sessions: sessions})
		})
		e.d.Sync()
	}
	dependent, err := e.svc.CreateLinked(e.ctx(), "project_a", "After manual", store.QueueLink{Session: "manual-work"})
	if err != nil {
		t.Fatal(err)
	}
	if dependent.AfterSession == nil || *dependent.AfterSession != "manual-work" {
		t.Fatalf("session link = %v", dependent.AfterSession)
	}
	for _, instruction := range []string{"first", "second"} {
		if _, err := e.svc.AddItem(e.ctx(), dependent.ID, "claude", "", instruction); err != nil {
			t.Fatal(err)
		}
	}
	status := func() string {
		t.Helper()
		v, err := e.svc.Get(e.ctx(), dependent.ID)
		if err != nil {
			t.Fatal(err)
		}
		return v.Items[0].Status
	}
	if _, err := e.svc.Start(e.ctx(), dependent.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if got := status(); got != store.ItemQueued {
		t.Fatalf("started before any inventory snapshot: %s", got)
	}
	snapshot(tmux.Session{Name: "manual-work", Status: tmux.AgentWorking, Agents: []string{"claude"}})
	if got := status(); got != store.ItemQueued {
		t.Fatalf("started while the session works: %s", got)
	}
	snapshot(tmux.Session{Name: "manual-work", Agents: []string{"codex"}})
	if got := status(); got != store.ItemQueued {
		t.Fatalf("started while an agent without hook status is open: %s", got)
	}
	snapshot(tmux.Session{Name: "manual-work", Status: tmux.AgentBlocked, Agents: []string{"claude"}})
	if got := status(); got != store.ItemRunning {
		t.Fatalf("item after the session went idle: %s", got)
	}
	if len(e.sessions.specs) != 1 {
		t.Fatalf("sessions created = %d, want 1", len(e.sessions.specs))
	}
	// Later work in the linked session doesn't gate the queue's next items.
	snapshot(tmux.Session{Name: "manual-work", Status: tmux.AgentWorking}, tmux.Session{Name: e.sessions.specs[0].Name})
	q := mustQueue(t, e, dependent.ID)
	ready := make(chan bool, 1)
	e.d.enqueue(func(ctx context.Context) { ready <- e.d.predecessorReady(ctx, q) })
	if !<-ready {
		t.Fatal("a begun queue is still gated by its linked session")
	}
}

// "Start after" on an existing queue: linking a running queue to a session
// holds its next item until that session is idle, even though earlier items
// already ran; clearing the link lets a held queue go on at once.
func TestSetLinkOnExistingQueueGatesNextItem(t *testing.T) {
	e := newDispEnv(t, "m1", "m2", "m3")
	e.svc.SetParallelQueues(true)
	snapshot := func(sessions ...tmux.Session) {
		e.d.enqueue(func(ctx context.Context) {
			e.d.sessionsChanged(ctx, store.HostMachineID, inventory.SessionsChanged{Sessions: sessions})
		})
		e.d.Sync()
	}
	achieve := func(n int, sid string) {
		r := e.run(n)
		e.hook(r, EventSessionStart, sid)
		e.claude.set(sid, agents.Achieved)
		e.hook(r, EventTurnEnd, sid)
	}
	e.startQueue()
	snapshot(tmux.Session{Name: "manual-work", Status: tmux.AgentWorking, Agents: []string{"claude"}})
	view, err := e.svc.SetLink(e.ctx(), e.queue.ID, store.QueueLink{Session: "manual-work"})
	if err != nil {
		t.Fatal(err)
	}
	if view.AfterSession == nil || *view.AfterSession != "manual-work" || view.AfterReleased {
		t.Fatalf("link = %v released %t", view.AfterSession, view.AfterReleased)
	}
	achieve(1, "s1")
	if got := e.itemStatuses(); !slices.Equal(got, []string{store.ItemDone, store.ItemQueued, store.ItemQueued}) {
		t.Fatalf("next item started while the linked session works: %v", got)
	}
	snapshot(tmux.Session{Name: "manual-work", Status: tmux.AgentBlocked, Agents: []string{"claude"}})
	if got := e.itemStatuses(); !slices.Equal(got, []string{store.ItemDone, store.ItemRunning, store.ItemQueued}) {
		t.Fatalf("after the session went idle: %v", got)
	}
	if q := mustQueue(t, e, e.queue.ID); !q.AfterReleased {
		t.Fatal("the link was not released by the started item")
	}
	// Later work in the session doesn't hold the item after.
	snapshot(tmux.Session{Name: "manual-work", Status: tmux.AgentWorking, Agents: []string{"claude"}})
	achieve(2, "s2")
	if got := e.itemStatuses(); !slices.Equal(got, []string{store.ItemDone, store.ItemDone, store.ItemRunning}) {
		t.Fatalf("a released link still gates: %v", got)
	}
}

func TestSetLinkClearsAndValidates(t *testing.T) {
	e := newDispEnv(t, "m1", "m2")
	e.svc.SetParallelQueues(true)
	e.startQueue()
	if _, err := e.svc.SetLink(e.ctx(), e.queue.ID, store.QueueLink{Session: "busy"}); err != nil {
		t.Fatal(err)
	}
	e.d.enqueue(func(ctx context.Context) {
		e.d.sessionsChanged(ctx, store.HostMachineID, inventory.SessionsChanged{Sessions: []tmux.Session{{Name: "busy", Status: tmux.AgentWorking, Agents: []string{"claude"}}}})
	})
	e.d.Sync()
	r := e.run(1)
	e.hook(r, EventSessionStart, "s1")
	e.claude.set("s1", agents.Achieved)
	e.hook(r, EventTurnEnd, "s1")
	if got := e.itemStatuses(); !slices.Equal(got, []string{store.ItemDone, store.ItemQueued}) {
		t.Fatalf("held item: %v", got)
	}
	view, err := e.svc.SetLink(e.ctx(), e.queue.ID, store.QueueLink{})
	if err != nil || view.AfterSession != nil || view.AfterRunID != nil {
		t.Fatalf("cleared link = %+v, %v", view.Queue, err)
	}
	e.d.Sync()
	if got := e.itemStatuses(); !slices.Equal(got, []string{store.ItemDone, store.ItemRunning}) {
		t.Fatalf("after clearing the link: %v", got)
	}
	// An ended goal, both links at once and a missing queue are refused.
	var qe *Error
	if _, err := e.svc.SetLink(e.ctx(), e.queue.ID, store.QueueLink{RunID: r.ID}); !errors.As(err, &qe) || qe.Status != http.StatusConflict {
		t.Fatalf("link to an ended goal: %v", err)
	}
	if _, err := e.svc.SetLink(e.ctx(), e.queue.ID, store.QueueLink{RunID: "x", Session: "y"}); !errors.As(err, &qe) || qe.Status != http.StatusBadRequest {
		t.Fatalf("both links: %v", err)
	}
	if _, err := e.svc.SetLink(e.ctx(), "queue_missing", store.QueueLink{}); err == nil {
		t.Fatal("missing queue linked")
	}
}

// A linked session missing from a snapshot taken before the queue was
// created is not known to be gone (the inventory hadn't listed it yet): the
// queue waits for a newer snapshot.
func TestQueueLinkedSessionMissingFromOlderSnapshotWaits(t *testing.T) {
	e := newDispEnv(t)
	e.svc.SetParallelQueues(true)
	snapshot := func(sessions ...tmux.Session) {
		e.d.enqueue(func(ctx context.Context) {
			e.d.sessionsChanged(ctx, store.HostMachineID, inventory.SessionsChanged{Sessions: sessions})
		})
		e.d.Sync()
	}
	snapshot(tmux.Session{Name: "unrelated"})
	e.clock.advance(time.Second)
	dependent, err := e.svc.CreateLinked(e.ctx(), "project_a", "After new session", store.QueueLink{Session: "just-created"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddItem(e.ctx(), dependent.ID, "claude", "", "first"); err != nil {
		t.Fatal(err)
	}
	status := func() string {
		t.Helper()
		v, err := e.svc.Get(e.ctx(), dependent.ID)
		if err != nil {
			t.Fatal(err)
		}
		return v.Items[0].Status
	}
	if _, err := e.svc.Start(e.ctx(), dependent.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if got := status(); got != store.ItemQueued {
		t.Fatalf("started from a snapshot older than the queue: %s", got)
	}
	e.clock.advance(time.Second)
	snapshot(tmux.Session{Name: "unrelated"}, tmux.Session{Name: "just-created", Status: tmux.AgentWorking, Agents: []string{"claude"}})
	if got := status(); got != store.ItemQueued {
		t.Fatalf("started while the session works: %s", got)
	}
	e.clock.advance(time.Second)
	snapshot(tmux.Session{Name: "unrelated"})
	if got := status(); got != store.ItemRunning {
		t.Fatalf("item after the session was gone from a newer snapshot: %s", got)
	}
}

func TestExistingSessionIdle(t *testing.T) {
	d := &Dispatcher{}
	if d.sessionIdle("host", "x", time.Time{}) {
		t.Fatal("idle before any snapshot")
	}
	at := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	d.sessions = map[string]map[string]tmux.Session{"host": {
		"shell":   {Name: "shell"},
		"working": {Name: "working", Status: tmux.AgentWorking},
		"blocked": {Name: "blocked", Status: tmux.AgentBlocked, Agents: []string{"claude"}},
		"ended":   {Name: "ended", Status: tmux.AgentEnded},
		"agent":   {Name: "agent", Agents: []string{"claude"}},
	}}
	d.sessionsAt = map[string]time.Time{"host": at}
	for name, want := range map[string]bool{"shell": true, "working": false, "blocked": true, "ended": true, "agent": false, "gone": true} {
		if got := d.sessionIdle("host", name, at.Add(-time.Second)); got != want {
			t.Errorf("sessionIdle(%s) = %v, want %v", name, got, want)
		}
	}
	if d.sessionIdle("host", "gone", at) {
		t.Error("a session missing from a snapshot not newer than the link counts as gone")
	}
	// Each machine has its own snapshot: a server without one knows nothing.
	if d.sessionIdle("s-abc123", "shell", at.Add(-time.Second)) {
		t.Error("a server session counted idle from the host's snapshot")
	}
}

func mustQueue(t *testing.T, e *dispEnv, id string) store.Queue {
	t.Helper()
	q, err := e.st.Queue(e.ctx(), id)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestDispatcherFailClosed(t *testing.T) {
	for _, c := range []struct {
		name   string
		act    func(e *dispEnv, r store.Run)
		status string
		detail string
	}{
		{"failed", func(e *dispEnv, r store.Run) { e.claude.set("s", agents.Failed); e.hook(r, EventTurnEnd, "s") }, store.RunFailed, "can't be achieved"},
		{"unknown format", func(e *dispEnv, r store.Run) { e.claude.set("s", agents.Unknown); e.hook(r, EventTurnEnd, "s") }, store.RunExited, "unrecognised goal state for Claude Code 2.1.283"},
		{"session end", func(e *dispEnv, r store.Run) { e.hook(r, EventSessionEnd, "s") }, store.RunExited, "ended without achieving"},
		{"clear", func(e *dispEnv, r store.Run) { e.hook(r, EventSessionEnd, "s", "reason", "clear") }, store.RunExited, "/clear"},
		{"changed session id", func(e *dispEnv, r store.Run) { e.hook(r, EventSessionStart, "other") }, store.RunExited, "the agent session changed"},
		{"stale", func(e *dispEnv, _ store.Run) { e.advance(staleAfter) }, store.RunStale, "no signal"},
		{"session closed", func(e *dispEnv, _ store.Run) {
			e.clock.advance(time.Minute)
			e.bus.Publish(events.Event{Type: events.SessionsChanged, Payload: inventory.SessionsChanged{Sessions: []tmux.Session{{Name: "unrelated"}}}})
			e.d.Sync()
		}, store.RunExited, "session was closed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newDispEnv(t, "m1", "m2")
			e.startQueue()
			r := e.run(1)
			e.hook(r, EventSessionStart, "s")
			c.act(e, e.run(1))
			got := e.run(1)
			if got.Status != c.status || !strings.Contains(got.Detail, c.detail) {
				t.Fatalf("run %s %q, want %s ~%q", got.Status, got.Detail, c.status, c.detail)
			}
			if st := e.itemStatuses(); !slices.Equal(st, []string{store.ItemNeedsAttention, store.ItemQueued}) || e.queueStatus() != store.QueuePaused {
				t.Fatalf("items %v, queue %s", st, e.queueStatus())
			}
			if len(e.sessions.specs) != 1 {
				t.Fatal("the next item started")
			}
		})
	}
}

func TestDispatcherIgnoresTheSameSessionStartTwice(t *testing.T) {
	e := newDispEnv(t, "m1")
	e.startQueue()
	r := e.run(1)
	e.hook(r, EventSessionStart, "s")
	e.hook(r, EventSessionStart, "s")
	if got := e.run(1); got.Status != store.RunRunning || got.AgentSessionID != "s" {
		t.Fatalf("run %+v", got)
	}
	if len(e.claude.armed) != 1 || e.claude.armed[0] != "s=m1" {
		t.Fatalf("armed %v", e.claude.armed)
	}
}

func TestDispatcherStaleThenLateAchieved(t *testing.T) {
	e := newDispEnv(t, "m1", "m2")
	e.startQueue()
	r := e.run(1)
	e.hook(r, EventSessionStart, "s")
	e.advance(staleAfter - time.Minute)
	e.hook(r, EventTurnEnd, "s") // a signal moves the deadline
	e.advance(staleAfter - time.Minute)
	if got := e.run(1).Status; got != store.RunRunning {
		t.Fatalf("stale too early: %s", got)
	}
	e.advance(2 * time.Minute)
	if got := e.run(1).Status; got != store.RunStale || e.queueStatus() != store.QueuePaused {
		t.Fatalf("not stale: %s, queue %s", got, e.queueStatus())
	}
	// The token isn't revoked: a late hook is still processed, and a late
	// achieved marks the item done but keeps the queue paused.
	e.claude.set("s", agents.Achieved)
	e.hook(e.run(1), EventTurnEnd, "s")
	if got := e.run(1).Status; got != store.RunAchieved {
		t.Fatalf("late achieved: %s", got)
	}
	if st := e.itemStatuses(); !slices.Equal(st, []string{store.ItemDone, store.ItemQueued}) || e.queueStatus() != store.QueuePaused || len(e.sessions.specs) != 1 {
		t.Fatalf("items %v, queue %s, sessions %d", st, e.queueStatus(), len(e.sessions.specs))
	}
	if _, err := e.svc.Resume(e.ctx(), e.queue.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if len(e.sessions.specs) != 2 || e.run(2).Status != store.RunStarting {
		t.Fatal("resume didn't start item 2")
	}
}

// A stale run sends no more hooks: Resume reads it once, so a goal it
// achieved meanwhile is seen and the queue moves on.
func TestDispatcherResumeReadsStaleRuns(t *testing.T) {
	e := newDispEnv(t, "m1", "m2")
	e.startQueue()
	e.hook(e.run(1), EventSessionStart, "s")
	e.advance(staleAfter)
	if e.run(1).Status != store.RunStale {
		t.Fatal("not stale")
	}
	e.claude.set("s", agents.Achieved)
	e.advance(time.Hour) // no hook, no read
	if e.run(1).Status != store.RunStale {
		t.Fatal("a stale run was read without a signal")
	}
	if _, err := e.svc.Resume(e.ctx(), e.queue.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if got := e.run(1).Status; got != store.RunAchieved || !slices.Equal(e.itemStatuses(), []string{store.ItemDone, store.ItemRunning}) {
		t.Fatalf("resume: run %s, items %v", got, e.itemStatuses())
	}
	if got := e.runEventKinds(e.run(1)); got[len(got)-1] != "user:achieved" {
		t.Fatalf("events %v", got)
	}
}

func TestDispatcherStaleStartingRunAndFinalRead(t *testing.T) {
	e := newDispEnv(t, "m1", "m2")
	e.startQueue()
	e.advance(staleAfter)
	if got := e.run(1); got.Status != store.RunStale || !strings.Contains(got.Detail, "trust the folder") {
		t.Fatalf("starting run: %+v", got)
	}
	// A bound run whose goal was achieved without a hook is picked up by the
	// read before stale.
	e2 := newDispEnv(t, "m1", "m2")
	e2.startQueue()
	e2.hook(e2.run(1), EventSessionStart, "s")
	e2.claude.set("s", agents.Achieved)
	e2.advance(staleAfter)
	if got := e2.run(1).Status; got != store.RunAchieved || e2.run(2).Status != store.RunStarting {
		t.Fatalf("final read: %s", got)
	}
}

func TestDispatcherPausedQueueKeepsTracking(t *testing.T) {
	e := newDispEnv(t, "m1", "m2")
	e.startQueue()
	e.hook(e.run(1), EventSessionStart, "s")
	if _, err := e.svc.Pause(e.ctx(), e.queue.ID); err != nil {
		t.Fatal(err)
	}
	e.claude.set("s", agents.Achieved)
	e.hook(e.run(1), EventTurnEnd, "s")
	if got := e.run(1).Status; got != store.RunAchieved || !slices.Equal(e.itemStatuses(), []string{store.ItemDone, store.ItemQueued}) || len(e.sessions.specs) != 1 {
		t.Fatalf("paused: run %s, items %v, sessions %d", got, e.itemStatuses(), len(e.sessions.specs))
	}
	if _, err := e.svc.Resume(e.ctx(), e.queue.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if len(e.sessions.specs) != 2 {
		t.Fatal("resume didn't start the next item")
	}
}

func TestDispatcherOwnerActions(t *testing.T) {
	e := newDispEnv(t, "m1", "m2")
	e.startQueue()
	first := e.run(1)
	e.hook(first, EventSessionStart, "s")
	e.advance(staleAfter) // stale: needs attention, run still active
	if e.run(1).Status != store.RunStale {
		t.Fatal("not stale")
	}
	// Retry: the stale run is cancelled (token revoked), the item is queued
	// again, the queue stays paused until Resume, which starts a new run
	// with a new token in a suffixed session; the old session is left open.
	if _, err := e.svc.Override(e.ctx(), e.items()[0].ID, ActionRetry); err != nil {
		t.Fatal(err)
	}
	old, _ := e.st.Run(e.ctx(), first.ID)
	if old.Status != store.RunCancelled || e.queueStatus() != store.QueuePaused || e.itemStatuses()[0] != store.ItemQueued {
		t.Fatalf("retry: run %s, queue %s, items %v", old.Status, e.queueStatus(), e.itemStatuses())
	}
	if _, err := e.svc.Resume(e.ctx(), e.queue.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	retried := e.run(1)
	if retried.ID == first.ID || string(retried.TokenHash) == string(first.TokenHash) || retried.SessionName != "app-q1-1" {
		t.Fatalf("retried run %+v", retried)
	}
	// Skip and mark done need the item to need attention.
	e.claude.set("s2", agents.Failed)
	e.hook(retried, EventSessionStart, "s2")
	e.hook(retried, EventTurnEnd, "s2")
	if _, err := e.svc.Override(e.ctx(), e.items()[0].ID, ActionMarkDone); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Resume(e.ctx(), e.queue.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if st := e.itemStatuses(); !slices.Equal(st, []string{store.ItemDone, store.ItemRunning}) {
		t.Fatalf("after mark done: %v", st)
	}
	e.hook(e.run(2), EventSessionEnd, "s3")
	if _, err := e.svc.Override(e.ctx(), e.items()[1].ID, ActionSkip); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Resume(e.ctx(), e.queue.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if e.queueStatus() != store.QueueFinished || !slices.Equal(e.itemStatuses(), []string{store.ItemDone, store.ItemSkipped}) {
		t.Fatalf("end: %s %v", e.queueStatus(), e.itemStatuses())
	}
}

// Concurrent signals: two achieved turn ends advance the queue once.
func TestDispatcherDuplicateSignalsAdvanceOnce(t *testing.T) {
	e := newDispEnv(t, "m1", "m2", "m3")
	e.startQueue()
	r := e.run(1)
	e.hook(r, EventSessionStart, "s")
	e.claude.set("s", agents.Achieved)
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.d.Notify(Signal{RunID: r.ID, Event: EventTurnEnd, Body: []byte(`{"session_id":"s"}`), At: e.clock.Now()})
		}()
	}
	wg.Wait()
	e.d.Sync()
	if len(e.sessions.specs) != 2 || !slices.Equal(e.itemStatuses(), []string{store.ItemDone, store.ItemRunning, store.ItemQueued}) || e.activeRuns() != 1 {
		t.Fatalf("sessions %d, items %v", len(e.sessions.specs), e.itemStatuses())
	}
}

func TestDispatcherRestartRecovery(t *testing.T) {
	e := newDispEnv(t, "m1", "m2")
	e.startQueue()
	e.hook(e.run(1), EventSessionStart, "s")
	e.stop()
	// Achieved while hostbud was down: the restart reads once and advances.
	e.claude.set("s", agents.Achieved)
	e.start()
	if got := e.run(1).Status; got != store.RunAchieved || e.run(2).Status != store.RunStarting {
		t.Fatalf("after restart: %s", got)
	}
	// The stale timer is re-armed from the last signal.
	e.stop()
	e.clock.advance(staleAfter + time.Minute)
	e.start()
	e.advance(0) // the re-armed timer is already due
	if got := e.run(2).Status; got != store.RunStale {
		t.Fatalf("re-armed stale timer: %s", got)
	}
}

func TestDispatcherStartFailures(t *testing.T) {
	e := newDispEnv(t, "m1", "m2")
	e.claude.verErr = errors.New("claude not found on the host — install Claude Code first")
	e.startQueue()
	if got := e.run(1); got.Status != store.RunFailed || got.Detail != "claude not found on the host — install Claude Code first" {
		t.Fatalf("version failure: %+v", got)
	}
	if !slices.Equal(e.itemStatuses(), []string{store.ItemNeedsAttention, store.ItemQueued}) || e.queueStatus() != store.QueuePaused {
		t.Fatalf("items %v queue %s", e.itemStatuses(), e.queueStatus())
	}
	// A failed Arm (Codex goal) leaves an untrackable run.
	e2 := newDispEnv(t, "m1")
	e2.claude.armErr = errors.New("could not set the Codex goal: the app server didn't confirm it")
	e2.startQueue()
	e2.hook(e2.run(1), EventSessionStart, "s")
	if got := e2.run(1); got.Status != store.RunExited || !strings.Contains(got.Detail, "Codex goal") {
		t.Fatalf("arm failure: %+v", got)
	}
}

func TestDispatcherReadErrorsAreRetried(t *testing.T) {
	e := newDispEnv(t, "m1")
	e.startQueue()
	e.hook(e.run(1), EventSessionStart, "s")
	e.claude.readErr = errors.New("sftp: timeout")
	e.hook(e.run(1), EventTurnEnd, "s")
	if got := e.run(1).Status; got != store.RunRunning {
		t.Fatalf("a read error changed the run: %s", got)
	}
	e.claude.readErr = nil
	e.claude.set("s", agents.Achieved)
	e.advance(2 * time.Second)
	if got := e.run(1).Status; got != store.RunAchieved {
		t.Fatalf("retry: %s", got)
	}
}

func TestDispatcherPublishesEveryTransition(t *testing.T) {
	e := newDispEnv(t, "m1")
	e.runChanged()
	e.startQueue()
	e.hook(e.run(1), EventSessionStart, "s")
	e.claude.set("s", agents.Achieved)
	e.hook(e.run(1), EventTurnEnd, "s")
	if got := e.runChanged(); !slices.Equal(got, []string{store.RunStarting, store.RunRunning, store.RunAchieved}) {
		t.Fatalf("run.changed %v", got)
	}
	if got := e.runEventKinds(e.run(1)); !slices.Equal(got, []string{"user:starting", "hook:running", "hook:achieved"}) {
		t.Fatalf("run events %v", got)
	}
}

// hostbud only ever creates sessions: no kill, detach, send-keys or
// capture-pane anywhere in the queue code.
func TestQueueCodeNeverTouchesSessions(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f) //nolint:gosec // this package's own sources
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"kill-session", "kill-server", "KillSession", ".Kill(", "send-keys", "SendKeys", "capture-pane", "detach-client"} {
			if strings.Contains(string(src), bad) {
				t.Errorf("%s contains %q", f, bad)
			}
		}
	}
}

// Queue runs on servers (V2-M13 follow-up): a server queue's run session
// starts on the server with the server hook URL, next to a host run; each
// machine's inventory snapshot only ends that machine's runs.
func TestServerQueueRunsOnTheServer(t *testing.T) {
	e := newDispEnv(t, "host goal")
	e.d.starter.SetServerHookURL("https://hostbud.example.com")
	e.svc.SetParallelQueues(true)
	e.st.addProject(store.Project{ID: "project_s", MachineID: "s-abc123", Name: "remote", Path: "/home/dev/remote"})
	sq, err := e.svc.Create(e.ctx(), "project_s", "Remote")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddItem(e.ctx(), sq.ID, "claude", "", "/goal remote"); err != nil {
		t.Fatal(err)
	}
	e.startQueue()
	if _, err := e.svc.Start(e.ctx(), sq.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if len(e.sessions.specs) != 2 {
		t.Fatalf("sessions created = %d, want 2", len(e.sessions.specs))
	}
	host, remote := e.sessions.specs[0], e.sessions.specs[1]
	if host.Machine != store.HostMachineID || host.Env[EnvURL] != "http://127.0.0.1:9055" {
		t.Fatalf("host spec %+v", host)
	}
	if remote.Machine != "s-abc123" || remote.Path != "/home/dev/remote" || remote.Env[EnvURL] != "https://hostbud.example.com" {
		t.Fatalf("server spec %+v", remote)
	}
	e.clock.advance(sessionGrace + time.Second)
	snapshot := func(machine string, names ...string) {
		var sessions []tmux.Session
		for _, n := range names {
			sessions = append(sessions, tmux.Session{Name: n})
		}
		e.d.enqueue(func(ctx context.Context) {
			e.d.sessionsChanged(ctx, machine, inventory.SessionsChanged{Sessions: sessions})
		})
		e.d.Sync()
	}
	// The server lists only its own session: the host run stays active.
	snapshot("s-abc123", remote.Name)
	if r := e.run(1); !r.Active() {
		t.Fatalf("a server snapshot ended the host run: %+v", r)
	}
	// The server's session is gone from the server's snapshot: its run ends.
	snapshot(store.HostMachineID, host.Name, remote.Name)
	snapshot("s-abc123")
	runs, _ := e.st.LatestRuns(e.ctx(), sq.ID)
	for _, r := range runs {
		if r.Active() {
			t.Fatalf("server run still active after its session left the server: %+v", r)
		}
	}
	if r := e.run(1); !r.Active() {
		t.Fatalf("host run ended: %+v", r)
	}
}
