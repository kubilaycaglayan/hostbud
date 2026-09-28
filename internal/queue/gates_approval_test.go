package queue

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"hostbud/internal/store"
)

// V2-M4 T3: Approve and Reject through the service and the dispatcher.

var owner = Actor{ID: "user_a", Email: "a@example.com"}

func awaitingEnv(t *testing.T) (*dispEnv, *fakeVerifier, store.Run) {
	t.Helper()
	e, v := gatedEnv(t, store.ItemGates{RequiresApproval: true}, store.ItemGates{})
	e.d.SetNotifications(&switchNotifications{on: true, push: true})
	e.startQueue()
	r := e.achieve(1)
	if e.itemStatus(1) != store.ItemAwaitingApproval {
		t.Fatalf("not awaiting approval: %v", e.itemStatuses())
	}
	return e, v, r
}

func TestApproveMarksDoneAndAdvances(t *testing.T) {
	e, _, r := awaitingEnv(t)
	if _, err := e.svc.Approve(e.ctx(), r.ItemID, owner); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if got := e.itemStatuses(); !slices.Equal(got, []string{store.ItemDone, store.ItemRunning}) {
		t.Fatalf("after Approve: %v", got)
	}
	if !slices.Contains(e.runEventKinds(r), "user:approved") {
		t.Fatalf("events %v", e.runEventKinds(r))
	}
	keys := e.st.outboxKeys()
	if !slices.Equal(keys[:2], []string{"run:" + r.ID + ":approval", "run:" + r.ID + ":done"}) {
		t.Fatalf("notifications %v: approval once, then done after Approve", keys)
	}
	// A second action loses with 409 naming the state.
	_, err := e.svc.Reject(e.ctx(), r.ItemID, owner)
	if queueStatus(err) != http.StatusConflict || !strings.Contains(err.Error(), "this item is done") {
		t.Fatalf("late Reject: %v", err)
	}
}

func TestRejectNeedsAttentionAndPauses(t *testing.T) {
	e, _, r := awaitingEnv(t)
	if _, err := e.svc.Reject(e.ctx(), r.ItemID, owner); err != nil {
		t.Fatal(err)
	}
	if e.itemStatus(1) != store.ItemNeedsAttention || e.itemStatus(2) != store.ItemQueued || e.queueStatus() != store.QueuePaused {
		t.Fatalf("after Reject: %v, queue %s", e.itemStatuses(), e.queueStatus())
	}
	if got := e.run(1).Detail; got != "rejected by a@example.com" {
		t.Fatalf("detail %q", got)
	}
	if !slices.Contains(e.runEventKinds(r), "user:rejected") {
		t.Fatalf("events %v", e.runEventKinds(r))
	}
	if slices.Contains(e.st.outboxKeys(), "run:"+r.ID+":done") {
		t.Fatal("a rejected item notified done")
	}
	if _, err := e.svc.Approve(e.ctx(), r.ItemID, owner); queueStatus(err) != http.StatusConflict {
		t.Fatalf("Approve after Reject: %v", err)
	}
}

func TestApproveRejectRaceAppliesOnce(t *testing.T) {
	e, _, r := awaitingEnv(t)
	var wg sync.WaitGroup
	results := make([]error, 50)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				_, results[i] = e.svc.Approve(context.Background(), r.ItemID, owner)
			} else {
				_, results[i] = e.svc.Reject(context.Background(), r.ItemID, owner)
			}
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range results {
		switch {
		case err == nil:
			ok++
		case queueStatus(err) != http.StatusConflict:
			t.Fatalf("race: %v", err)
		}
	}
	owners := 0
	for _, k := range e.runEventKinds(r) {
		if k == "user:approved" || k == "user:rejected" {
			owners++
		}
	}
	if ok != 1 || owners != 1 {
		t.Fatalf("%d successes, %d owner events; want 1 and 1", ok, owners)
	}
}

func TestApprovalOnlyFromAwaitingApproval(t *testing.T) {
	e, _ := gatedEnv(t, store.ItemGates{RequiresApproval: true})
	it := e.items()[0]
	for _, act := range []func(context.Context, string, Actor) (View, error){e.svc.Approve, e.svc.Reject} {
		if _, err := act(e.ctx(), it.ID, owner); queueStatus(err) != http.StatusConflict || !strings.Contains(err.Error(), "only for items awaiting approval") {
			t.Fatalf("on a queued item: %v", err)
		}
	}
	if _, err := e.svc.Approve(e.ctx(), "item_nope", owner); queueStatus(err) != http.StatusNotFound {
		t.Fatalf("unknown item: %v", err)
	}
}

func TestRestartWhileAwaitingApproval(t *testing.T) {
	e, v, r := awaitingEnv(t)
	e.stop()
	e.start()
	if e.itemStatus(1) != store.ItemAwaitingApproval || e.itemStatus(2) != store.ItemQueued || v.callCount() != 0 {
		t.Fatalf("after restart: %v, %d verify calls", e.itemStatuses(), v.callCount())
	}
	if n := slices.Index(e.st.outboxKeys(), "run:"+r.ID+":approval"); n < 0 || slices.Index(e.st.outboxKeys()[n+1:], "run:"+r.ID+":approval") >= 0 {
		t.Fatalf("approval notified %v", e.st.outboxKeys())
	}
	if _, err := e.svc.Approve(e.ctx(), r.ItemID, owner); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if e.itemStatus(2) != store.ItemRunning {
		t.Fatalf("after Approve: %v", e.itemStatuses())
	}
}

func TestApprovingAPausedQueueDoesNotAdvance(t *testing.T) {
	e, _, r := awaitingEnv(t)
	if _, err := e.svc.Pause(e.ctx(), e.queue.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Approve(e.ctx(), r.ItemID, owner); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if got := e.itemStatuses(); !slices.Equal(got, []string{store.ItemDone, store.ItemQueued}) {
		t.Fatalf("a paused queue advanced: %v", got)
	}
}
