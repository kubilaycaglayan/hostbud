package queue

import (
	"testing"
	"time"

	"hostbud/internal/store"
)

func (e *dispEnv) setLoop(enabled bool, limit time.Duration) {
	e.t.Helper()
	if _, err := e.svc.SetLoop(e.ctx(), e.queue.ID, enabled, limit); err != nil {
		e.t.Fatal(err)
	}
}

// finish achieves item n's run, including the follow-up goal read.
func (e *dispEnv) finish(n int) {
	e.t.Helper()
	e.achieve(n)
	e.advance(2 * time.Second)
}

func (e *dispEnv) loopQueue() store.Queue {
	q, _ := e.st.Queue(e.ctx(), e.queue.ID)
	return q
}

func TestLoopQueueRunsItemsAgainUntilTheRuntimeLimit(t *testing.T) {
	e := newDispEnv(t, "m1", "m2")
	e.setLoop(true, 4*time.Hour)
	e.startQueue()
	if q := e.loopQueue(); q.LoopStartedAt == nil || !q.LoopStartedAt.Equal(e.clock.Now()) || q.LoopCount != 0 {
		t.Fatalf("loop clock = %v, count %d", q.LoopStartedAt, q.LoopCount)
	}
	step := 45 * time.Minute // under staleAfter
	// Pass 1 ends at 1h30.
	e.advance(step)
	e.finish(1)
	e.advance(step)
	e.finish(2)
	if got := e.itemStatuses(); got[0] != store.ItemRunning || got[1] != store.ItemQueued {
		t.Fatalf("pass 2 did not start: %v, queue %s", got, e.queueStatus())
	}
	if q := e.loopQueue(); q.LoopCount != 1 || q.Status != store.QueueRunning {
		t.Fatalf("after pass 1: count %d status %s", q.LoopCount, q.Status)
	}
	// Pass 2 ends at 3h: under the limit, so pass 3 starts.
	e.advance(step)
	e.finish(1)
	e.advance(step)
	e.finish(2)
	if q := e.loopQueue(); q.LoopCount != 2 || e.itemStatuses()[0] != store.ItemRunning {
		t.Fatalf("pass 3 did not start: count %d items %v", q.LoopCount, e.itemStatuses())
	}
	// The limit passes during pass 3, which still runs to its end; no pass 4.
	e.advance(step)
	e.finish(1)
	e.advance(step)
	if got := e.itemStatuses(); got[1] != store.ItemRunning {
		t.Fatalf("the limit cut a pass short: %v", got)
	}
	e.finish(2)
	if e.queueStatus() != store.QueueFinished {
		t.Fatalf("queue = %s, items %v", e.queueStatus(), e.itemStatuses())
	}
	if got := e.itemStatuses(); got[0] != store.ItemDone || got[1] != store.ItemDone {
		t.Fatalf("items after the last pass: %v", got)
	}
	if n := len(e.sessions.specs); n != 6 {
		t.Fatalf("sessions created = %d, want 6 (3 passes × 2)", n)
	}
}

func TestLoopQueueOffFinishesAfterOnePass(t *testing.T) {
	e := newDispEnv(t, "m1")
	e.startQueue()
	e.finish(1)
	if e.queueStatus() != store.QueueFinished || e.loopQueue().LoopCount != 0 {
		t.Fatalf("queue %s count %d", e.queueStatus(), e.loopQueue().LoopCount)
	}
}

func TestLoopQueueSpacesQuickPassesAndSurvivesRestart(t *testing.T) {
	e := newDispEnv(t)
	if _, err := e.svc.AddItem(e.ctx(), e.queue.ID, "claude", "", "", store.ItemGates{
		ExecutionMode: "session", TargetSession: "work", Command: "make check",
	}); err != nil {
		t.Fatal(err)
	}
	e.setLoop(true, time.Hour)
	e.startQueue()
	commands := func() int {
		e.sessions.mu.Lock()
		defer e.sessions.mu.Unlock()
		return len(e.sessions.commands)
	}
	if n := commands(); n != 1 {
		t.Fatalf("commands after start = %d", n)
	}
	q := e.loopQueue()
	if q.ScheduledAt == nil || !q.ScheduledAt.Equal(e.clock.Now().Add(store.MinLoopPassInterval)) || q.Status != store.QueueRunning {
		t.Fatalf("next pass due %v, status %s", q.ScheduledAt, q.Status)
	}
	// A restart keeps the pending pass.
	e.stop()
	starter := NewStarter(e.st, e.sessions, "http://127.0.0.1:9055", nil)
	e.d = NewDispatcher(e.st, adapterMap{"claude": e.claude, "codex": e.claude}, starter, e.svc, e.bus, staleAfter, nil)
	e.d.SetClock(e.clock)
	e.start()
	e.advance(store.MinLoopPassInterval - time.Second)
	if n := commands(); n != 1 {
		t.Fatalf("next pass started early: %d commands", n)
	}
	e.advance(time.Second)
	if n := commands(); n != 2 {
		t.Fatalf("commands at the due time = %d", n)
	}
	// Pausing cancels the pending pass.
	if _, err := e.svc.Pause(e.ctx(), e.queue.ID); err != nil {
		t.Fatal(err)
	}
	e.advance(time.Hour)
	if n := commands(); n != 2 {
		t.Fatalf("paused queue kept looping: %d commands", n)
	}
}

func TestLoopQueueNeedsAttentionStillPauses(t *testing.T) {
	e := newDispEnv(t, "m1")
	e.setLoop(true, 5*time.Hour)
	e.startQueue()
	r := e.run(1)
	e.hook(r, EventSessionEnd, "sess-x")
	if e.queueStatus() != store.QueuePaused || e.itemStatuses()[0] != store.ItemNeedsAttention {
		t.Fatalf("queue %s items %v", e.queueStatus(), e.itemStatuses())
	}
	if e.loopQueue().LoopCount != 0 {
		t.Fatal("a failed item started another pass")
	}
}

func TestStartRequeuesAFinishedLoopQueue(t *testing.T) {
	e := newDispEnv(t, "m1")
	e.setLoop(true, time.Minute)
	e.startQueue()
	e.advance(2 * time.Minute)
	e.finish(1)
	if e.queueStatus() != store.QueueFinished {
		t.Fatalf("queue = %s", e.queueStatus())
	}
	e.startQueue()
	if got := e.itemStatuses(); got[0] != store.ItemRunning {
		t.Fatalf("restart did not requeue: %v", got)
	}
	if q := e.loopQueue(); !q.LoopStartedAt.Equal(e.clock.Now()) || q.LoopCount != 0 {
		t.Fatalf("restart did not reset the loop clock: %v %d", q.LoopStartedAt, q.LoopCount)
	}
}

func TestSetLoopValidatesTheLimit(t *testing.T) {
	e := newDispEnv(t)
	for _, d := range []time.Duration{0, time.Millisecond, store.MaxLoopMaxRuntime + time.Second} {
		if _, err := e.svc.SetLoop(e.ctx(), e.queue.ID, true, d); err == nil {
			t.Errorf("accepted %s", d)
		}
	}
	v, err := e.svc.SetLoop(e.ctx(), e.queue.ID, true, 90*time.Minute)
	if err != nil || v.Loop == nil || !v.Loop.Enabled || v.Loop.MaxRuntimeSeconds != 5400 || v.Loop.Pass != 1 {
		t.Fatalf("SetLoop = %+v, %v", v.Loop, err)
	}
	if v, _ = e.svc.SetLoop(e.ctx(), e.queue.ID, false, store.DefaultLoopMaxRuntime); v.Loop != nil {
		t.Fatalf("default loop settings still shown: %+v", v.Loop)
	}
}
