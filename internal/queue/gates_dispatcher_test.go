package queue

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"hostbud/internal/agents"
	"hostbud/internal/notify"
	"hostbud/internal/store"
)

// V2-M4 T2: the verify gate in the dispatcher (fake verifier, fake clock,
// in-memory store).

// gatedEnv is a dispatcher env whose queue Q has the given items.
func gatedEnv(t *testing.T, items ...store.ItemGates) (*dispEnv, *fakeVerifier) {
	t.Helper()
	e := newDispEnv(t)
	v := newFakeVerifier()
	e.d.SetVerifier(v)
	for i, g := range items {
		if _, err := e.svc.AddItem(e.ctx(), e.queue.ID, "claude", "", "/goal m"+string(rune('1'+i)), g); err != nil {
			t.Fatal(err)
		}
	}
	return e, v
}

// achieve binds item n's run and reports it achieved.
func (e *dispEnv) achieve(n int) store.Run {
	e.t.Helper()
	r := e.run(n)
	sid := "sess-" + r.ID
	e.hook(r, EventSessionStart, sid)
	e.claude.set(sid, agents.Achieved)
	e.hook(r, EventTurnEnd, sid)
	return r
}

// until syncs the dispatcher until cond holds (verify results arrive from
// another goroutine).
func (e *dispEnv) until(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s (items %v, queue %s)", what, e.itemStatuses(), e.queueStatus())
		}
		time.Sleep(2 * time.Millisecond)
		e.d.Sync()
	}
}

func (e *dispEnv) itemStatus(n int) string { return e.items()[n-1].Status }

func TestGatesNoneKeepsV2M3Path(t *testing.T) {
	e, v := gatedEnv(t, store.ItemGates{}, store.ItemGates{})
	e.startQueue()
	r := e.achieve(1)
	if got := e.itemStatuses(); !slices.Equal(got, []string{store.ItemDone, store.ItemRunning}) {
		t.Fatalf("statuses %v", got)
	}
	if v.callCount() != 0 || slices.ContainsFunc(e.runEventKinds(r), func(k string) bool { return strings.HasPrefix(k, "verify:") }) {
		t.Fatalf("an ungated item ran verify: %d calls, events %v", v.callCount(), e.runEventKinds(r))
	}
}

func TestVerifyPassesThenAdvances(t *testing.T) {
	e, v := gatedEnv(t, store.ItemGates{VerifyCommand: "make test"}, store.ItemGates{})
	e.startQueue()
	r := e.achieve(1)
	e.until("verify done", func() bool { return e.itemStatus(1) == store.ItemDone })
	if e.itemStatus(2) != store.ItemRunning || e.run(1).Status != store.RunAchieved {
		t.Fatalf("after a passing verify: %v, run %s", e.itemStatuses(), e.run(1).Status)
	}
	if !slices.Equal(v.calls, []string{"host|/home/dev/app|make test"}) {
		t.Fatalf("verify calls %v", v.calls)
	}
	kinds := e.runEventKinds(r)
	if !slices.Contains(kinds, "verify:verify_started") || !slices.Contains(kinds, "verify:verify_result") {
		t.Fatalf("events %v", kinds)
	}
	if got := e.st.verifyResults(r.ID); len(got) != 1 || got[0].Attempt != 1 || !got[0].Passed() {
		t.Fatalf("results %+v", got)
	}
}

func TestVerifyWaitsWhileRunningAndHoldsTheQueue(t *testing.T) {
	e, v := gatedEnv(t, store.ItemGates{VerifyCommand: "make test"}, store.ItemGates{})
	v.hold = true
	e.startQueue()
	e.achieve(1)
	<-v.started
	e.d.Sync()
	if e.itemStatus(1) != store.ItemVerifying || e.itemStatus(2) != store.ItemQueued || e.queueStatus() != store.QueueRunning {
		t.Fatalf("while verifying: %v, queue %s", e.itemStatuses(), e.queueStatus())
	}
	view, _ := e.svc.Get(e.ctx(), e.queue.ID)
	if vs := view.Items[0].Verify; vs == nil || !vs.Running || vs.Attempt != 1 {
		t.Fatalf("view while verifying: %+v", vs)
	}
	// Pausing while verifying: the gate finishes, nothing new starts.
	if _, err := e.svc.Pause(e.ctx(), e.queue.ID); err != nil {
		t.Fatal(err)
	}
	v.release <- struct{}{}
	e.until("verify done", func() bool { return e.itemStatus(1) == store.ItemDone })
	if e.itemStatus(2) != store.ItemQueued {
		t.Fatalf("a paused queue advanced after its gates: %v", e.itemStatuses())
	}
}

func TestVerifyFailsPausesWithDetail(t *testing.T) {
	e, v := gatedEnv(t, store.ItemGates{VerifyCommand: "make test"}, store.ItemGates{})
	on := &switchNotifications{on: true, push: true}
	e.d.SetNotifications(on)
	ch, _ := e.bus.Subscribe(1000)
	code := 2
	v.script(VerifyResult{Outcome: VerifyFailed, ExitCode: &code, Detail: "verify failed (exit 2)", Output: "FAIL: secret output"})
	e.startQueue()
	r := e.achieve(1)
	e.until("needs attention", func() bool { return e.itemStatus(1) == store.ItemNeedsAttention })
	if e.queueStatus() != store.QueuePaused || e.itemStatus(2) != store.ItemQueued {
		t.Fatalf("after a failed verify: %v, queue %s", e.itemStatuses(), e.queueStatus())
	}
	if got := e.run(1); got.Status != store.RunAchieved || got.Detail != "verify failed (exit 2)" {
		t.Fatalf("run after a failed verify: %+v", got)
	}
	view, _ := e.svc.Get(e.ctx(), e.queue.ID)
	if vs := view.Items[0].Verify; vs == nil || vs.Running || vs.Outcome != VerifyFailed || *vs.ExitCode != 2 || vs.Output != "FAIL: secret output" {
		t.Fatalf("view: %+v", vs)
	}
	_, keys, payloads := notices(ch)
	if keys["needs_attention"] != "run:"+r.ID+":verify:1" {
		t.Fatalf("notification keys %v", keys)
	}
	for _, p := range payloads {
		b, _ := json.Marshal(p)
		if strings.Contains(string(b), "make test") || strings.Contains(string(b), "secret output") {
			t.Fatalf("payload leaks the command or output: %s", b)
		}
		if p.Key == "run:"+r.ID+":verify:1" && p.Outcome != notify.OutcomeVerifyFailed {
			t.Fatalf("outcome %q", p.Outcome)
		}
	}
	if slices.Contains(e.st.outboxKeys(), "run:"+r.ID+":done") {
		t.Fatal("done notified before the gates passed")
	}
}

func TestApprovalGateWaitsAndFreesTheQueueSlot(t *testing.T) {
	e, _ := gatedEnv(t, store.ItemGates{RequiresApproval: true}, store.ItemGates{})
	on := &switchNotifications{on: true, push: true}
	e.d.SetNotifications(on)
	e.startQueue()
	r := e.achieve(1)
	if e.itemStatus(1) != store.ItemAwaitingApproval || e.itemStatus(2) != store.ItemQueued || e.queueStatus() != store.QueueRunning {
		t.Fatalf("awaiting approval: %v, queue %s", e.itemStatuses(), e.queueStatus())
	}
	if keys := e.st.outboxKeys(); !slices.Equal(keys, []string{"run:" + r.ID + ":approval"}) {
		t.Fatalf("outbox %v", keys)
	}
	n, _ := e.st.CountActiveRuns(e.ctx(), store.HostMachineID)
	if n != 0 {
		t.Fatalf("awaiting approval holds %d slots", n)
	}
}

func TestVerifyThenApproval(t *testing.T) {
	e, v := gatedEnv(t, store.ItemGates{VerifyCommand: "true", RequiresApproval: true})
	e.startQueue()
	e.achieve(1)
	e.until("awaiting approval", func() bool { return e.itemStatus(1) == store.ItemAwaitingApproval })
	if v.callCount() != 1 || e.queueStatus() != store.QueueRunning {
		t.Fatalf("verify calls %d, queue %s", v.callCount(), e.queueStatus())
	}
}

func TestLateAchievedAfterStaleEntersGatesQueueStaysPaused(t *testing.T) {
	e, v := gatedEnv(t, store.ItemGates{VerifyCommand: "true"}, store.ItemGates{})
	e.startQueue()
	r := e.run(1)
	e.hook(r, EventSessionStart, "s1")
	e.advance(staleAfter + time.Second)
	if e.run(1).Status != store.RunStale || e.queueStatus() != store.QueuePaused {
		t.Fatalf("not stale: %s, %s", e.run(1).Status, e.queueStatus())
	}
	e.claude.set("s1", agents.Achieved)
	e.d.Kick(e.queue.ID) // Resume's late read; the queue itself stays paused
	e.until("done", func() bool { return e.itemStatus(1) == store.ItemDone })
	if v.callCount() != 1 || e.itemStatus(2) != store.ItemQueued {
		t.Fatalf("late achieved: %d verify calls, %v", v.callCount(), e.itemStatuses())
	}
}

func TestRestartDuringVerifyNeverRerunsIt(t *testing.T) {
	e, v := gatedEnv(t, store.ItemGates{VerifyCommand: "make test"}, store.ItemGates{})
	v.hold = true
	e.startQueue()
	r := e.achieve(1)
	<-v.started
	e.stop() // shutdown: the attempt stays open
	if e.itemStatus(1) != store.ItemVerifying || len(e.st.verifyResults(r.ID)) != 0 {
		t.Fatalf("after shutdown: %v, results %v", e.itemStatuses(), e.st.verifyResults(r.ID))
	}
	e.start() // recovery
	if e.itemStatus(1) != store.ItemNeedsAttention || e.queueStatus() != store.QueuePaused || v.callCount() != 1 {
		t.Fatalf("after restart: %v, queue %s, %d calls", e.itemStatuses(), e.queueStatus(), v.callCount())
	}
	if got := e.run(1).Detail; got != "hostbud restarted during verify — Re-run verify" {
		t.Fatalf("detail %q", got)
	}
	if res := e.st.verifyResults(r.ID); len(res) != 1 || res[0].Outcome != VerifyInterrupted || res[0].Attempt != 1 {
		t.Fatalf("results %+v", res)
	}
}

func TestRestartWithUnstartedVerifyStartsItOnce(t *testing.T) {
	e, v := gatedEnv(t, store.ItemGates{VerifyCommand: "make test"})
	e.startQueue()
	r := e.run(1)
	e.stop()
	// A verifying item without an open attempt (its last attempt finished).
	ended := e.clock.Now()
	_, _ = e.st.TransitionRun(e.ctx(), r.ID, store.ActiveRunStatuses, store.RunAchieved, "", &ended)
	_, _ = e.st.TransitionQueueItem(e.ctx(), r.ItemID, []string{store.ItemRunning}, store.ItemVerifying)
	e.start()
	e.until("done", func() bool { return e.itemStatus(1) == store.ItemDone })
	if v.callCount() != 1 {
		t.Fatalf("verify calls %d, want 1", v.callCount())
	}
}

func TestVerifyingHoldsASlotAwaitingApprovalFreesIt(t *testing.T) {
	e := newSlotEnv(t, intp(1), 1, "A", "B")
	v := newFakeVerifier()
	v.hold = true
	e.d.SetVerifier(v)
	a := e.queues["A"]
	aItems, _ := e.st.QueueItems(e.ctx(), a.ID)
	cmd, on := "make test", true
	if _, err := e.svc.UpdateItem(e.ctx(), aItems[0].ID, store.QueueItemUpdate{VerifyCommand: &cmd, RequiresApproval: &on}); err != nil {
		t.Fatal(err)
	}
	e.start("A")
	e.start("B")
	runA, _ := e.st.LatestRunForItem(e.ctx(), aItems[0].ID)
	sid := "sess-a"
	e.hook(runA, EventSessionStart, sid)
	e.claude.set(sid, agents.Achieved)
	e.hook(runA, EventTurnEnd, sid)
	<-v.started
	e.d.Sync()
	if got := e.startOrder(); !slices.Equal(got, []string{"A"}) {
		t.Fatalf("B started while A verifies (cap 1): %v", got)
	}
	v.release <- struct{}{}
	e.until("B starts", func() bool { return len(e.startOrder()) == 2 })
	if it, _ := e.st.QueueItem(e.ctx(), aItems[0].ID); it.Status != store.ItemAwaitingApproval {
		t.Fatalf("A item %s", it.Status)
	}
	if e.st.maxActive > 1 {
		t.Fatalf("slots exceeded the cap: %d", e.st.maxActive)
	}
	qa, _ := e.st.Queue(e.ctx(), a.ID)
	if qa.WaitingSince != nil {
		t.Fatalf("a queue in its gates is in line for a slot: %v", qa.WaitingSince)
	}
}

func TestPassingGateStartsTheNextItemWithSlots(t *testing.T) {
	e := newSlotEnv(t, intp(1), 2, "A")
	v := newFakeVerifier()
	e.d.SetVerifier(v)
	a := e.queues["A"]
	items, _ := e.st.QueueItems(e.ctx(), a.ID)
	cmd := "true"
	_, _ = e.svc.UpdateItem(e.ctx(), items[0].ID, store.QueueItemUpdate{VerifyCommand: &cmd})
	ch, _ := e.bus.Subscribe(1000)
	e.start("A")
	run, _ := e.st.LatestRunForItem(e.ctx(), items[0].ID)
	e.clock.advance(time.Minute)
	e.hook(run, EventSessionStart, "s")
	e.claude.set("s", agents.Achieved)
	e.hook(run, EventTurnEnd, "s")
	e.until("item 2 starts", func() bool { return len(e.startOrder()) == 2 })
	actions, _, _ := notices(ch)
	if !slices.Contains(actions, "verifying") || !slices.Contains(actions, "item_done") {
		t.Fatalf("actions %v", actions)
	}
}
