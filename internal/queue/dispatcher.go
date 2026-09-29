package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"hostbud/internal/agents"
	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/notify"
	"hostbud/internal/store"
)

// DispatchStore is what the dispatcher needs from the store.
type DispatchStore interface {
	Store
	StarterStore
	GateStore
	Run(ctx context.Context, id string) (store.Run, error)
	ActiveRunForItem(ctx context.Context, itemID string) (store.Run, error)
	ActiveRuns(ctx context.Context) ([]store.Run, error)
	WaitingQueues(ctx context.Context, machineID string) ([]store.Queue, error)
	SetQueueWaiting(ctx context.Context, id string, since *time.Time) (store.Queue, error)
	// V2-M3: the notifying transitions write their push outbox rows in the
	// same transaction (nil notice: a plain transition).
	TransitionQueueItemNotify(ctx context.Context, id string, from []string, to string, n *store.Notice) (store.QueueItem, error)
	TransitionQueueNotify(ctx context.Context, id string, from []string, to string, n *store.Notice) (store.Queue, error)
}

// Adapters looks up an agent adapter by kind (agents.Registry).
type Adapters interface {
	Get(kind string) agents.Adapter
}

// Clock is time for the dispatcher (a fake one in tests).
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is a stoppable timer.
type Timer interface{ Stop() bool }

type realClock struct{}

func (realClock) Now() time.Time                            { return time.Now() }
func (realClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }

// FollowUps are the extra goal-state reads after a pending turn_end:
// Claude's /goal evaluator is a parallel Stop hook whose verdict lands about
// 2 s after hostbud's hook fired (§12 S5), and no hook follows it.
var FollowUps = []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second}

// sessionGrace is how long a new run's session may be missing from the
// inventory before that counts as its end (a snapshot taken before the
// session was created can arrive after).
const sessionGrace = 15 * time.Second

// RunChanged is the run.changed payload.
type RunChanged struct {
	RunID   string          `json:"runId"`
	ItemID  string          `json:"itemId"`
	QueueID string          `json:"queueId"`
	Status  string          `json:"status"`
	Detail  string          `json:"detail,omitempty"`
	Flag    json.RawMessage `json:"flag,omitempty"`
}

// Dispatcher runs the v2 state machines (§5): it starts items, reacts to
// hooks, the inventory and timers, and never kills, detaches or types into a
// session. All work happens on one goroutine; store updates are guarded, so
// a duplicate signal can't move a run twice.
type Dispatcher struct {
	store      DispatchStore
	adapters   Adapters
	starter    *Starter
	service    *Service
	bus        *events.Bus
	clock      Clock
	log        *slog.Logger
	staleAfter time.Duration

	work    chan func(context.Context)
	timers  map[string]Timer   // stale timer per run
	follows map[string][]Timer // follow-up reads per run
	waiting map[string]bool    // queues last published as waiting for a slot

	dispatching, again bool // dispatch is running / was asked for again meanwhile

	notifications Notifications // V2-M3; nil = off
	llmFlags      bool

	// V2-M4: the verify runner, the context verify commands run under (Run's)
	// and the running ones.
	verifier  VerifyRunner
	base      context.Context
	verifying sync.WaitGroup
}

// Notifications gates V2-M3 notices (notify.Service): with every account
// off, transitions carry none and nothing new runs.
type Notifications interface {
	Enabled(ctx context.Context) bool
	// PushAvailable: VAPID keys are set, so transitions queue push deliveries.
	PushAvailable() bool
	// Wake tells the push sender that a transition queued deliveries.
	Wake()
}

// SetNotifications turns V2-M3 notices on (nil turns them off).
func (d *Dispatcher) SetNotifications(n Notifications) { d.notifications = n }

// SetLLMFlags enables explicit flag clears on run.changed only when the
// supervisor is configured. When off, old event payloads are unchanged.
func (d *Dispatcher) SetLLMFlags(enabled bool) { d.llmFlags = enabled }

// NewDispatcher wires the dispatcher to the service (Kick, overrides) and
// the hook receiver (Notify).
func NewDispatcher(st DispatchStore, adapters Adapters, starter *Starter, service *Service, bus *events.Bus, staleAfter time.Duration, log *slog.Logger) *Dispatcher {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	d := &Dispatcher{
		store: st, adapters: adapters, starter: starter, service: service, bus: bus, clock: realClock{}, log: log, staleAfter: staleAfter,
		work: make(chan func(context.Context), 4096), timers: map[string]Timer{}, follows: map[string][]Timer{}, waiting: map[string]bool{},
	}
	service.SetDispatcher(d)
	starter.inSlot = service.ParallelQueues
	return d
}

// SetClock replaces the clock (tests).
func (d *Dispatcher) SetClock(c Clock) {
	d.clock = c
	d.starter.now = c.Now
}

func (d *Dispatcher) enqueue(f func(context.Context)) {
	d.work <- f
}

// Kick looks for the next item of a running queue (service → dispatcher,
// on the owner's Start or Resume). Stale runs of the queue are read once
// first: a stale run sends no more hooks, so a goal it achieved later is
// only seen now (§5.4, late achieved).
func (d *Dispatcher) Kick(queueID string) {
	d.enqueue(func(ctx context.Context) {
		d.readStale(ctx, queueID)
		d.next(ctx, queueID, store.SourceUser)
	})
}

// CapacityChanged hands out slots after the owner changed the machine's
// cap: raising or clearing it starts waiting queues at once; lowering it
// stops nothing (V2-M2).
func (d *Dispatcher) CapacityChanged() {
	d.enqueue(func(ctx context.Context) {
		if d.slots() {
			d.dispatch(ctx, store.SourceUser)
		}
	})
}

func (d *Dispatcher) readStale(ctx context.Context, queueID string) {
	items, err := d.store.QueueItems(ctx, queueID)
	if err != nil {
		return
	}
	for _, it := range items {
		if it.Status != store.ItemNeedsAttention {
			continue
		}
		if run, err := d.store.ActiveRunForItem(ctx, it.ID); err == nil && run.Status == store.RunStale {
			d.read(ctx, run, store.SourceUser, false)
		}
	}
}

// Notify hands an accepted hook to the dispatcher (hooks → dispatcher).
func (d *Dispatcher) Notify(s Signal) {
	d.enqueue(func(ctx context.Context) { d.signal(ctx, s) })
}

// EndActiveRun cancels an item's active run before an owner override. The
// token is revoked (cancelled ends the run); the session stays open.
func (d *Dispatcher) EndActiveRun(ctx context.Context, item store.QueueItem, action string) error {
	done := make(chan error, 1)
	d.enqueue(func(ctx context.Context) {
		run, err := d.store.ActiveRunForItem(ctx, item.ID)
		if errors.Is(err, store.ErrNotFound) {
			done <- nil
			return
		}
		if err != nil {
			done <- err
			return
		}
		_, err = d.finish(ctx, run, store.RunCancelled, "cancelled by the owner ("+action+")", store.SourceUser, false)
		done <- err
		if err == nil && d.slots() {
			d.dispatch(ctx, store.SourceUser) // its slot is free
		}
	})
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Run processes work until ctx ends. It first recovers runs that were
// active when hostbud stopped (§5, restart safety).
func (d *Dispatcher) Run(ctx context.Context) {
	sessions, cancel := d.bus.Subscribe(256)
	defer func() { cancel() }() // the latest subscription
	d.base = ctx
	defer d.verifying.Wait()
	d.recover(ctx)
	for {
		select {
		case <-ctx.Done():
			for id := range d.timers {
				d.stopTimers(id)
			}
			return
		case f := <-d.work:
			d.safely(ctx, f)
		case e, ok := <-sessions:
			if !ok {
				sessions, cancel = d.bus.Subscribe(256)
				continue
			}
			if e.Type == events.SessionsChanged {
				if payload, ok := e.Payload.(inventory.SessionsChanged); ok {
					d.safely(ctx, func(ctx context.Context) { d.sessionsChanged(ctx, payload) })
				}
			}
		}
	}
}

func (d *Dispatcher) safely(ctx context.Context, f func(context.Context)) {
	defer func() {
		if r := recover(); r != nil {
			d.log.Error("queue dispatcher panic", "panic", fmt.Sprint(r))
		}
	}()
	f(ctx)
}

// recover re-arms timers of active runs, reads each bound running run once
// (a goal achieved during the restart is picked up), and kicks running
// queues that have no active run.
func (d *Dispatcher) recover(ctx context.Context) {
	runs, err := d.store.ActiveRuns(ctx)
	if err != nil {
		d.log.Error("queue recovery: list active runs", "err", err)
	}
	for _, run := range runs {
		if run.Status != store.RunStale {
			d.armStale(run)
		}
		if run.AgentSessionID != "" && run.Status != store.RunStarting {
			d.read(ctx, run, store.SourcePoller, false)
		}
	}
	d.recoverGates(ctx)
	if d.slots() {
		// Active runs are in the store before any dispatch, so the count
		// includes them; waiting_since keeps the order across the restart.
		d.dispatch(ctx, store.SourcePoller)
		return
	}
	queues, err := d.store.Queues(ctx, store.HostMachineID)
	if err != nil {
		d.log.Error("queue recovery: list queues", "err", err)
		return
	}
	for _, q := range queues {
		if q.Status == store.QueueRunning {
			d.advance(ctx, q.ID, store.SourcePoller)
		}
	}
}

// Sync waits until all work queued before it is done (tests).
func (d *Dispatcher) Sync() {
	done := make(chan struct{})
	d.enqueue(func(context.Context) { close(done) })
	<-done
}

// ---------------------------------------------------------------- starting

// queueBusy reports whether the queue has an active run or an item in its
// gates (V2-M4).
func (d *Dispatcher) queueBusy(ctx context.Context, queueID string) (bool, error) {
	items, err := d.store.QueueItems(ctx, queueID)
	if err != nil {
		return false, err
	}
	for _, it := range items {
		if it.Status == store.ItemQueued || it.Status == store.ItemDone || it.Status == store.ItemSkipped {
			continue
		}
		if it.Status == store.ItemVerifying || it.Status == store.ItemAwaitingApproval {
			return true, nil // V2-M4: the queue waits for the item's gates
		}
		if _, err := d.store.ActiveRunForItem(ctx, it.ID); err == nil {
			return true, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return false, err
		}
	}
	return false, nil
}

// advance starts the first queued item of a running queue with no active
// run, or finishes the queue when nothing is queued (§5.1).
func (d *Dispatcher) advance(ctx context.Context, queueID, source string) {
	q, err := d.store.Queue(ctx, queueID)
	if err != nil || q.Status != store.QueueRunning {
		return
	}
	if !d.predecessorReady(ctx, q) {
		return
	}
	if busy, err := d.queueBusy(ctx, queueID); err != nil || busy {
		return
	}
	item, err := d.store.FirstQueuedItem(ctx, queueID)
	if errors.Is(err, store.ErrNotFound) {
		d.finishQueue(ctx, queueID)
		return
	} else if err != nil {
		d.log.Error("queue: next item", "err", err)
		return
	}
	d.startItem(ctx, q, item, source)
}

// predecessorReady keeps a dependent queue out of both dispatcher paths
// until its linked goal achieves. Stale is active because it can achieve late.
func (d *Dispatcher) predecessorReady(ctx context.Context, q store.Queue) bool {
	if q.AfterRunID == nil {
		return true
	}
	run, err := d.store.Run(ctx, *q.AfterRunID)
	if err == nil && run.Status == store.RunAchieved {
		return true
	}
	if err == nil && (run.Status == store.RunStarting || run.Status == store.RunRunning || run.Status == store.RunStale) {
		return false
	}
	if errors.Is(err, store.ErrNotFound) || err == nil {
		_, _ = d.service.Pause(ctx, q.ID)
	}
	return false
}

// startItem creates the item's run and session. It reports false only when
// the store's locked re-check found no free slot (the item is queued
// again and the queue keeps its place in line).
func (d *Dispatcher) startItem(ctx context.Context, q store.Queue, item store.QueueItem, source string) bool {
	if _, err := d.store.TransitionQueueItem(ctx, item.ID, []string{store.ItemQueued}, store.ItemRunning); err != nil {
		return true
	}
	project, err := d.store.Project(ctx, q.ProjectID)
	if err != nil {
		d.log.Error("queue: project", "err", err)
		return true
	}
	var adapter RunAgent
	if a := d.adapters.Get(item.Agent); a != nil {
		adapter = a
	}
	run, err := d.starter.Start(ctx, source, project, d.sessionLabel(ctx, q), item, adapter)
	if errors.Is(err, store.ErrNoSlot) {
		_, _ = d.store.TransitionQueueItem(ctx, item.ID, []string{store.ItemRunning}, store.ItemQueued)
		return false
	}
	if d.slots() {
		_, _ = d.store.SetQueueWaiting(ctx, q.ID, nil) // out of line: it holds a slot now
	}
	if err != nil {
		d.log.Error("queue: start run", "err", err)
		if run.ID == "" {
			return true
		}
	}
	d.publishRun(ctx, run, q.ID)
	if run.Status == store.RunFailed {
		d.needsAttention(ctx, run, item, q.ID, source)
		return true
	}
	d.armStale(run)
	d.service.Publish(ctx, "run_started", q.ID)
	return true
}

// ---------------------------------------------------------------- slots (V2-M2)

// slots reports whether V2-M2 slot dispatch is on (HOSTBUD_PARALLEL_QUEUES).
// Off, V2-M1's paths run unchanged and the cap isn't consulted.
func (d *Dispatcher) slots() bool { return d.service.ParallelQueues() }

// next runs when a queue may take its next item: V2-M1 starts it at once;
// with slots the queue goes to the back of the line (waiting_since = now)
// and the machine's free slots are handed out, so with a cap queues take
// turns and none starves.
func (d *Dispatcher) next(ctx context.Context, queueID, source string) {
	if !d.slots() {
		d.advance(ctx, queueID, source)
		return
	}
	if q, err := d.store.Queue(ctx, queueID); err == nil && q.Status == store.QueueRunning {
		d.toBack(ctx, queueID)
	}
	d.dispatch(ctx, source)
}

// toBack sets a queue's waiting_since to now, or just after the latest one
// in line when the clock hasn't moved past it (same instant, clock skew),
// so it really goes to the back.
func (d *Dispatcher) toBack(ctx context.Context, queueID string) {
	at := d.clock.Now().UTC().Truncate(time.Microsecond) // the column's precision
	if waiting, err := d.store.WaitingQueues(ctx, store.HostMachineID); err == nil {
		for _, q := range waiting {
			if q.ID != queueID && !q.WaitingSince.Before(at) {
				at = q.WaitingSince.Add(time.Microsecond)
			}
		}
	}
	_, _ = d.store.SetQueueWaiting(ctx, queueID, &at)
}

// dispatch is the machine-wide decision point (it only runs on the
// dispatcher goroutine): free slots go to the running queues without an
// active run, oldest waiting_since first; the store re-checks the cap when
// it creates each run. It then publishes queue.changed for every queue
// whose "waiting for a free slot" state changed.
func (d *Dispatcher) dispatch(ctx context.Context, source string) {
	// A start that fails inside the loop pauses its queue, which asks for
	// another dispatch: run it after this pass instead of nested.
	if d.dispatching {
		d.again = true
		return
	}
	d.dispatching = true
	defer func() { d.dispatching = false }()
	machine := store.HostMachineID
	for again := true; again; {
		d.again = false
		d.dispatchOnce(ctx, machine, source)
		again = d.again
	}
	d.publishWaiting(ctx, machine)
}

func (d *Dispatcher) dispatchOnce(ctx context.Context, machine, source string) {
	queues, err := d.store.Queues(ctx, machine)
	if err != nil {
		d.log.Error("queue: list queues", "err", err)
		return
	}
	for _, q := range queues {
		if q.Status == store.QueueRunning && !d.predecessorReady(ctx, q) {
			continue
		}
		// A running queue out of line: one from before V2-M2, or one whose
		// start raced a restart. It joins at the back.
		if q.Status == store.QueueRunning && q.WaitingSince == nil {
			if busy, err := d.queueBusy(ctx, q.ID); err == nil && !busy {
				d.toBack(ctx, q.ID)
			}
		}
	}
	waiting, err := d.store.WaitingQueues(ctx, machine)
	if err != nil {
		d.log.Error("queue: waiting queues", "err", err)
		return
	}
	for _, q := range waiting {
		if !d.predecessorReady(ctx, q) {
			continue
		}
		if busy, err := d.queueBusy(ctx, q.ID); err != nil {
			continue
		} else if busy {
			_, _ = d.store.SetQueueWaiting(ctx, q.ID, nil)
			continue
		}
		item, err := d.store.FirstQueuedItem(ctx, q.ID)
		if errors.Is(err, store.ErrNotFound) {
			_, _ = d.store.SetQueueWaiting(ctx, q.ID, nil)
			d.finishQueue(ctx, q.ID)
			continue
		} else if err != nil {
			d.log.Error("queue: next item", "err", err)
			continue
		}
		if !d.slotFree(ctx, machine) || !d.startItem(ctx, q, item, source) {
			return
		}
	}
}

// slotFree reports whether the machine has a free run slot (no cap: always).
// A store error counts as no slot: nothing starts on a guess.
func (d *Dispatcher) slotFree(ctx context.Context, machine string) bool {
	limit, err := d.store.MachineCapacity(ctx, machine)
	if err != nil {
		return false
	}
	if limit == nil {
		return true
	}
	active, err := d.store.CountActiveRuns(ctx, machine)
	return err == nil && active < *limit
}

// publishWaiting publishes queue.changed for each queue whose "waiting for
// a free slot" state changed since the last dispatch.
func (d *Dispatcher) publishWaiting(ctx context.Context, machine string) {
	queues, err := d.store.Queues(ctx, machine)
	if err != nil {
		return
	}
	now := map[string]bool{}
	for _, q := range queues {
		if head, err := d.service.waitingForSlot(ctx, q); err == nil && head != "" {
			now[q.ID] = true
		}
	}
	for id := range now {
		if !d.waiting[id] {
			d.service.Publish(ctx, "waiting_for_slot", id)
		}
	}
	for id := range d.waiting {
		if !now[id] {
			if _, err := d.store.Queue(ctx, id); err == nil {
				d.service.Publish(ctx, "slot_free", id)
			}
		}
	}
	d.waiting = now
}

// sessionLabel is "" for the project's first (oldest) queue, which keeps
// V2-M1's session names, and the queue's name for any other (V2-M2 T3).
func (d *Dispatcher) sessionLabel(ctx context.Context, q store.Queue) string {
	queues, err := d.store.Queues(ctx, q.MachineID)
	if err != nil {
		return ""
	}
	for _, other := range queues { // oldest first (created_at, id)
		if other.ProjectID == q.ProjectID {
			if other.ID == q.ID {
				return ""
			}
			return q.Name
		}
	}
	return ""
}

// ---------------------------------------------------------------- signals

// context of one run: its item, queue and adapter.
type runCtx struct {
	run       store.Run
	item      store.QueueItem
	queueID   string
	adapter   agents.Adapter
	condition string
}

func (d *Dispatcher) load(ctx context.Context, runID string) (runCtx, bool) {
	run, err := d.store.Run(ctx, runID)
	if err != nil {
		return runCtx{}, false
	}
	item, err := d.store.QueueItem(ctx, run.ItemID)
	if err != nil {
		return runCtx{}, false
	}
	condition, _ := agents.Condition(item.Instruction)
	return runCtx{run: run, item: item, queueID: item.QueueID, adapter: d.adapters.Get(item.Agent), condition: condition}, true
}

func (d *Dispatcher) signal(ctx context.Context, s Signal) {
	rc, ok := d.load(ctx, s.RunID)
	if !ok || !rc.run.Active() || rc.adapter == nil {
		return
	}
	at := s.At
	run, err := d.store.UpdateRun(ctx, rc.run.ID, store.RunUpdate{LastSignalAt: &at})
	if err != nil {
		return
	}
	rc.run = run
	if run.Status != store.RunStale {
		d.armStale(run)
	}
	d.stopFollowUps(run.ID)
	b, parseErr := rc.adapter.ParseHook(s.Event, s.Body)
	switch s.Event {
	case EventSessionStart:
		if parseErr != nil {
			return
		}
		d.bind(ctx, rc, b)
	case EventTurnEnd:
		if rc.run.AgentSessionID == "" {
			if parseErr != nil || !d.bind(ctx, rc, b) {
				return
			}
			rc, _ = d.load(ctx, s.RunID)
		}
		d.read(ctx, rc.run, store.SourceHook, true)
	case EventSessionEnd:
		detail := "the agent session ended without achieving the goal"
		if parseErr == nil && b.Reason == "clear" {
			detail = "the agent session was cleared (/clear) — the goal can't be tracked"
		}
		d.ended(ctx, rc, store.SourceHook, detail)
	}
}

// bind records the agent session id on the first SessionStart and arms the
// goal. The same id again is ignored; another id means the run can no
// longer be tracked (§5.2). It reports whether the run is bound.
func (d *Dispatcher) bind(ctx context.Context, rc runCtx, b agents.Binding) bool {
	switch rc.run.AgentSessionID {
	case b.SessionID:
		return true
	case "":
	default:
		d.untrackable(ctx, rc, store.SourceHook, "the agent session changed (/clear or restart) — the goal can't be tracked")
		return false
	}
	run, err := d.store.UpdateRun(ctx, rc.run.ID, store.RunUpdate{AgentSessionID: &b.SessionID, TranscriptPath: &b.TranscriptPath})
	if err != nil {
		return false
	}
	if run.Status == store.RunStarting {
		if run, err = d.store.TransitionRun(ctx, run.ID, []string{store.RunStarting}, store.RunRunning, "", nil); err != nil {
			return false
		}
		d.event(ctx, run, store.SourceHook, store.RunRunning, "")
		d.publishRun(ctx, run, rc.queueID)
		d.service.Publish(ctx, "run_running", rc.queueID)
	}
	if err := rc.adapter.Arm(ctx, run.MachineID, b, run, rc.condition); err != nil {
		rc.run = run
		d.untrackable(ctx, rc, store.SourceHook, err.Error())
		return false
	}
	return true
}

func bindingOf(run store.Run) agents.Binding {
	return agents.Binding{SessionID: run.AgentSessionID, TranscriptPath: run.TranscriptPath}
}

// read asks the adapter for the goal state and applies it. A failed read
// (an SFTP timeout) is retried on the next signal. followUp schedules the
// extra reads after a pending turn end.
func (d *Dispatcher) read(ctx context.Context, run store.Run, source string, followUp bool) {
	rc, ok := d.load(ctx, run.ID)
	if !ok || !rc.run.Active() || rc.run.AgentSessionID == "" || rc.adapter == nil {
		return
	}
	state, err := rc.adapter.ReadGoalState(ctx, rc.run.MachineID, bindingOf(rc.run), rc.run, rc.condition)
	if err != nil {
		d.log.Info("goal state read failed; retrying on the next signal", "run", rc.run.ID)
		d.log.Debug("goal state read failed", "run", rc.run.ID, "err", err)
		if followUp {
			d.scheduleFollowUps(rc.run.ID)
		}
		return
	}
	switch state.Status {
	case agents.Achieved:
		d.achieved(ctx, rc, source)
	case agents.Failed:
		d.fail(ctx, rc, source, withReason("the agent says the goal can't be achieved", state.Reason))
	case agents.Unknown:
		d.untrackable(ctx, rc, source, d.unknownDetail(rc, state.Reason))
	default:
		if state.Offset != rc.run.TranscriptOffset {
			offset := state.Offset
			_, _ = d.store.UpdateRun(ctx, rc.run.ID, store.RunUpdate{TranscriptOffset: &offset})
		}
		if followUp {
			d.scheduleFollowUps(rc.run.ID)
		}
	}
}

func (d *Dispatcher) unknownDetail(rc runCtx, reason string) string {
	name := map[string]string{"claude": "Claude Code", "codex": "Codex"}[rc.item.Agent]
	detail := fmt.Sprintf("unrecognised goal state for %s %s — check the session", name, rc.run.ClientVersion)
	return withReason(detail, reason)
}

func withReason(detail, reason string) string {
	if reason == "" {
		return detail
	}
	return detail + " (" + reason + ")"
}

// ended handles the end of the agent session (SessionEnd, or the session
// gone from tmux): one last read, then exited unless achieved.
func (d *Dispatcher) ended(ctx context.Context, rc runCtx, source, detail string) {
	if rc.run.AgentSessionID != "" {
		state, err := rc.adapter.ReadGoalState(ctx, rc.run.MachineID, bindingOf(rc.run), rc.run, rc.condition)
		switch {
		case err == nil && state.Status == agents.Achieved:
			d.achieved(ctx, rc, source)
			return
		case err == nil && state.Status == agents.Failed:
			d.fail(ctx, rc, source, withReason("the agent says the goal can't be achieved", state.Reason))
			return
		case err != nil:
			detail = "the agent session ended and its goal state couldn't be read (" + err.Error() + ")"
		}
	}
	d.untrackable(ctx, rc, source, detail)
}

// sessionsChanged treats a run whose session left tmux like a SessionEnd.
func (d *Dispatcher) sessionsChanged(ctx context.Context, payload inventory.SessionsChanged) {
	names := map[string]bool{}
	for _, s := range payload.Sessions {
		names[s.Name] = true
	}
	runs, err := d.store.ActiveRuns(ctx)
	if err != nil {
		return
	}
	for _, run := range runs {
		if run.SessionName == "" || names[run.SessionName] || d.clock.Now().Sub(run.StartedAt) < sessionGrace {
			continue
		}
		if rc, ok := d.load(ctx, run.ID); ok && rc.adapter != nil {
			d.ended(ctx, rc, store.SourcePoller, "the run's tmux session was closed")
		}
	}
}

// ---------------------------------------------------------------- timers

func (d *Dispatcher) armStale(run store.Run) {
	if d.staleAfter <= 0 {
		return
	}
	since := run.StartedAt
	if run.LastSignalAt != nil {
		since = *run.LastSignalAt
	}
	wait := max(since.Add(d.staleAfter).Sub(d.clock.Now()), 0)
	if t, ok := d.timers[run.ID]; ok {
		t.Stop()
	}
	id := run.ID
	d.timers[id] = d.clock.AfterFunc(wait, func() {
		d.enqueue(func(ctx context.Context) { d.staleCheck(ctx, id) })
	})
}

// staleCheck marks a run stale after HOSTBUD_RUN_STALE_AFTER without a
// signal, after one last read (§5.2). It only flags the run.
func (d *Dispatcher) staleCheck(ctx context.Context, runID string) {
	rc, ok := d.load(ctx, runID)
	if !ok || (rc.run.Status != store.RunStarting && rc.run.Status != store.RunRunning) {
		return
	}
	since := rc.run.StartedAt
	if rc.run.LastSignalAt != nil {
		since = *rc.run.LastSignalAt
	}
	if d.clock.Now().Sub(since) < d.staleAfter {
		d.armStale(rc.run) // a signal moved the deadline
		return
	}
	delete(d.timers, runID)
	if rc.run.AgentSessionID != "" && rc.adapter != nil {
		if state, err := rc.adapter.ReadGoalState(ctx, rc.run.MachineID, bindingOf(rc.run), rc.run, rc.condition); err == nil && state.Status == agents.Achieved {
			d.achieved(ctx, rc, store.SourceTimer)
			return
		}
	}
	detail := fmt.Sprintf("no signal from the agent for %s — the run may be stuck, or busy with long background work", d.staleAfter)
	if rc.run.Status == store.RunStarting {
		detail = "No SessionStart hook arrived — the client may be waiting for you to trust the folder (or, for Codex, hostbud's hooks). Open the session to check."
	}
	run, err := d.store.TransitionRun(ctx, runID, []string{store.RunStarting, store.RunRunning}, store.RunStale, detail, nil)
	if err != nil {
		return
	}
	d.event(ctx, run, store.SourceTimer, store.RunStale, detail)
	d.publishRun(ctx, run, rc.queueID)
	d.needsAttention(ctx, run, rc.item, rc.queueID, store.SourceTimer)
}

func (d *Dispatcher) scheduleFollowUps(runID string) {
	d.stopFollowUps(runID)
	for _, after := range FollowUps {
		id := runID
		d.follows[runID] = append(d.follows[runID], d.clock.AfterFunc(after, func() {
			d.enqueue(func(ctx context.Context) {
				if run, err := d.store.Run(ctx, id); err == nil {
					d.read(ctx, run, store.SourceTimer, false)
				}
			})
		}))
	}
}

func (d *Dispatcher) stopFollowUps(runID string) {
	for _, t := range d.follows[runID] {
		t.Stop()
	}
	delete(d.follows, runID)
}

func (d *Dispatcher) stopTimers(runID string) {
	if t, ok := d.timers[runID]; ok {
		t.Stop()
		delete(d.timers, runID)
	}
	d.stopFollowUps(runID)
}

// ---------------------------------------------------------------- outcomes

// finish moves an active run to a final state (the token is revoked with
// it) and stops its timers. It never touches the session.
func (d *Dispatcher) finish(ctx context.Context, run store.Run, to, detail, source string, publish bool) (store.Run, error) {
	ended := d.clock.Now()
	done, err := d.store.TransitionRun(ctx, run.ID, store.ActiveRunStatuses, to, detail, &ended)
	if err != nil {
		return run, err
	}
	d.stopTimers(run.ID)
	d.event(ctx, done, source, to, detail)
	item, _ := d.store.QueueItem(ctx, run.ItemID)
	d.publishRun(ctx, done, item.QueueID)
	if publish {
		d.service.Publish(ctx, "run_"+to, item.QueueID)
	}
	if to != store.RunAchieved && to != store.RunStale {
		d.pauseDependents(ctx, run.ID)
	}
	return done, nil
}

func (d *Dispatcher) pauseDependents(ctx context.Context, runID string) {
	views, err := d.service.List(ctx)
	if err != nil {
		return
	}
	for _, q := range views {
		if q.AfterRunID != nil && *q.AfterRunID == runID && q.Status == store.QueueRunning {
			_, _ = d.service.Pause(ctx, q.ID)
		}
	}
}

// achieved: the only outcome that advances the queue (§5.4). A late
// achieved after stale marks the item done but leaves the queue paused.
func (d *Dispatcher) achieved(ctx context.Context, rc runCtx, source string) {
	wasStale := rc.run.Status == store.RunStale
	run, err := d.finish(ctx, rc.run, store.RunAchieved, "", source, false)
	if err != nil {
		return
	}
	d.releaseDependents(ctx, run.ID)
	if rc.item.Gated() {
		// V2-M4: the item enters its gates; the queue waits (a late achieved
		// leaves it paused, so the gates never advance it).
		d.enterGates(ctx, run, rc.item, rc.queueID, source, []string{store.ItemRunning, store.ItemNeedsAttention})
		return
	}
	notice := d.notice(ctx, notify.KindDone, rc.run, rc.item, rc.queueID)
	if _, err := d.store.TransitionQueueItemNotify(ctx, rc.item.ID, []string{store.ItemRunning, store.ItemNeedsAttention}, store.ItemDone, d.outbox(notice)); err != nil {
		d.service.Publish(ctx, "run_achieved", rc.queueID)
		return
	}
	d.wakePush(notice)
	d.service.PublishNotice(ctx, "item_done", rc.queueID, notice)
	switch {
	case !wasStale:
		d.next(ctx, rc.queueID, source)
	case d.slots():
		d.dispatch(ctx, source) // the stale run's slot is free; its queue stays paused
	}
}

func (d *Dispatcher) releaseDependents(ctx context.Context, runID string) {
	views, err := d.service.List(ctx)
	if err != nil {
		return
	}
	for _, q := range views {
		if q.AfterRunID != nil && *q.AfterRunID == runID && q.Status == store.QueueRunning {
			d.Kick(q.ID)
		}
	}
}

func (d *Dispatcher) fail(ctx context.Context, rc runCtx, source, detail string) {
	if run, err := d.finish(ctx, rc.run, store.RunFailed, detail, source, false); err == nil {
		d.needsAttention(ctx, run, rc.item, rc.queueID, source)
	}
}

// untrackable ends a run hostbud can no longer follow as exited (§5.4).
func (d *Dispatcher) untrackable(ctx context.Context, rc runCtx, source, detail string) {
	if run, err := d.finish(ctx, rc.run, store.RunExited, detail, source, false); err == nil {
		d.needsAttention(ctx, run, rc.item, rc.queueID, source)
	}
}

// needsAttention sets the item to needs_attention and pauses its queue.
// With slots, an ended run's slot goes to the next waiting queue at once (a
// stale run keeps its slot: it is still active).
func (d *Dispatcher) needsAttention(ctx context.Context, run store.Run, item store.QueueItem, queueID, source string) {
	notice := d.notice(ctx, notify.KindAttention, run, item, queueID)
	if _, err := d.store.TransitionQueueItemNotify(ctx, item.ID, []string{store.ItemRunning}, store.ItemNeedsAttention, d.outbox(notice)); err != nil {
		notice = nil // not this transition's: another signal got there first
	}
	d.wakePush(notice)
	_, _ = d.store.TransitionQueue(ctx, queueID, []string{store.QueueRunning}, store.QueuePaused)
	d.service.PublishNotice(ctx, "needs_attention", queueID, notice)
	if d.slots() {
		d.dispatch(ctx, source)
	}
}

// finishQueue ends a running queue with nothing left to run.
func (d *Dispatcher) finishQueue(ctx context.Context, queueID string) {
	var notice *notify.Payload
	if runs, err := d.store.LatestRuns(ctx, queueID); err == nil {
		// The last run: started last, then ended last, then the later item.
		var last store.Run
		var lastItem store.QueueItem
		for _, r := range runs {
			item, err := d.store.QueueItem(ctx, r.ItemID)
			if err != nil {
				continue
			}
			if last.ID == "" || laterRun(r, item, last, lastItem) {
				last, lastItem = r, item
			}
		}
		if last.ID != "" {
			notice = d.notice(ctx, notify.KindFinished, last, lastItem, queueID)
		}
	}
	if _, err := d.store.TransitionQueueNotify(ctx, queueID, []string{store.QueueRunning}, store.QueueFinished, d.outbox(notice)); err == nil {
		d.wakePush(notice)
		d.service.PublishNotice(ctx, "finished", queueID, notice)
	}
}

// outbox is a notice as push outbox rows (nil without push or notice).
func (d *Dispatcher) outbox(p *notify.Payload) *store.Notice {
	if p == nil || d.notifications == nil || !d.notifications.PushAvailable() {
		return nil
	}
	b, err := p.JSON()
	if err != nil {
		d.log.Warn("notification not queued for push", "kind", p.Kind, "err", err)
		return nil
	}
	return &store.Notice{Kind: p.Kind, Key: p.Key, Payload: b}
}

func (d *Dispatcher) wakePush(p *notify.Payload) {
	if p != nil && d.notifications != nil && d.notifications.PushAvailable() {
		d.notifications.Wake()
	}
}

func laterRun(a store.Run, ai store.QueueItem, b store.Run, bi store.QueueItem) bool {
	if !a.StartedAt.Equal(b.StartedAt) {
		return a.StartedAt.After(b.StartedAt)
	}
	ae, be := a.StartedAt, b.StartedAt
	if a.EndedAt != nil {
		ae = *a.EndedAt
	}
	if b.EndedAt != nil {
		be = *b.EndedAt
	}
	if !ae.Equal(be) {
		return ae.After(be)
	}
	return ai.Position > bi.Position
}

// notice builds the V2-M3 notification for a transition (the run that
// ended, its item and queue); nil while every account is off. The payload
// holds only allowlisted fields (notify.Build).
func (d *Dispatcher) notice(ctx context.Context, kind string, run store.Run, item store.QueueItem, queueID string) *notify.Payload {
	if d.notifications == nil || !d.notifications.Enabled(ctx) {
		return nil
	}
	name := ""
	if q, err := d.store.Queue(ctx, queueID); err == nil {
		if p, err := d.store.Project(ctx, q.ProjectID); err == nil {
			name = p.Name
		}
	}
	p, err := notify.Build(notify.Event{
		Kind: kind, RunID: run.ID, QueueID: queueID, ItemID: item.ID, Project: name, Position: item.Position, Outcome: run.Status,
	})
	if err != nil {
		d.log.Warn("notification not built", "kind", kind, "err", err)
		return nil
	}
	return &p
}

func (d *Dispatcher) event(ctx context.Context, run store.Run, source, kind, detail string) {
	payload, _ := json.Marshal(map[string]string{"status": run.Status, "detail": detail})
	if _, err := d.store.AppendRunEvent(ctx, run.ID, source, kind, payload); err != nil {
		d.log.Warn("run event not recorded", "run", run.ID, "err", err)
	}
}

func (d *Dispatcher) publishRun(_ context.Context, run store.Run, queueID string) {
	if d.bus == nil {
		return
	}
	change := RunChanged{
		RunID: run.ID, ItemID: run.ItemID, QueueID: queueID, Status: run.Status, Detail: run.Detail,
	}
	if d.llmFlags {
		change.Flag = json.RawMessage("null")
	}
	d.bus.Publish(events.Event{Type: events.RunChanged, Machine: run.MachineID, Payload: change})
	d.log.Info("run changed", "run", run.ID, "status", run.Status)
}
