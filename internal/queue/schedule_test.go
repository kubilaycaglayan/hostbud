package queue

import (
	"context"
	"testing"
	"time"

	"hostbud/internal/store"
)

func TestScheduledQueueWaitsUntilDueAndSurvivesDispatcherRecovery(t *testing.T) {
	e := newDispEnv(t, "scheduled")
	delay := 15 * time.Minute
	if _, err := e.svc.Start(e.ctx(), e.queue.ID, delay); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	q, _ := e.st.Queue(e.ctx(), e.queue.ID)
	if q.ScheduledAt == nil || !q.ScheduledAt.Equal(e.clock.Now().Add(delay)) {
		t.Fatalf("scheduledAt = %v", q.ScheduledAt)
	}
	if got := e.itemStatuses(); got[0] != store.ItemQueued {
		t.Fatalf("started before due: %v", got)
	}
	e.stop()
	starter := NewStarter(e.st, e.sessions, "http://127.0.0.1:9055", nil)
	e.d = NewDispatcher(e.st, adapterMap{"claude": e.claude, "codex": e.claude}, starter, e.svc, e.bus, staleAfter, nil)
	e.d.SetClock(e.clock)
	e.start()
	e.advance(delay - time.Second)
	if got := e.itemStatuses(); got[0] != store.ItemQueued {
		t.Fatalf("started early: %v", got)
	}
	e.advance(time.Second)
	if got := e.itemStatuses(); got[0] != store.ItemRunning {
		t.Fatalf("at due time: %v", got)
	}
}

func TestQueueItemDispatchesCommandToExistingSession(t *testing.T) {
	e := newDispEnv(t)
	item, err := e.svc.AddItem(e.ctx(), e.queue.ID, "claude", "", "", store.ItemGates{
		ExecutionMode: "session", TargetSession: "work", Command: "printf '%s' 'literal; text'",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Start(e.ctx(), e.queue.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	got, _ := e.st.QueueItem(e.ctx(), item.ID)
	if got.Status != store.ItemDone {
		t.Fatalf("item status = %s", got.Status)
	}
	e.sessions.mu.Lock()
	defer e.sessions.mu.Unlock()
	if len(e.sessions.commands) != 1 || e.sessions.commands[0] != "host/work/printf '%s' 'literal; text'" {
		t.Fatalf("dispatches = %q", e.sessions.commands)
	}
	if _, err := e.svc.AddItem(context.Background(), e.queue.ID, "claude", "", "", store.ItemGates{ExecutionMode: "session", TargetSession: "bad:name", Command: "echo x"}); err == nil {
		t.Fatal("accepted an invalid session target")
	}
}
