package queue

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"hostbud/internal/store"
)

// V2-M4 T4: Re-run verify and the V2-M1 owner actions around the gates.

// failedVerifyEnv: item 1 (verify command, maybe approval) failed its
// verify, so it needs attention and the queue is paused.
func failedVerifyEnv(t *testing.T, approval bool) (*dispEnv, *fakeVerifier, store.Run) {
	t.Helper()
	e, v := gatedEnv(t, store.ItemGates{VerifyCommand: "test -f done.flag", RequiresApproval: approval}, store.ItemGates{})
	code := 1
	v.script(VerifyResult{Outcome: VerifyFailed, ExitCode: &code, Detail: "verify failed (exit 1)"})
	e.startQueue()
	r := e.achieve(1)
	e.until("needs attention", func() bool { return e.itemStatus(1) == store.ItemNeedsAttention })
	return e, v, r
}

func TestReverifyPassesWithoutANewRun(t *testing.T) {
	e, v, r := failedVerifyEnv(t, false)
	sessions := len(e.sessions.created())
	if _, err := e.svc.Reverify(e.ctx(), r.ItemID); err != nil {
		t.Fatal(err)
	}
	e.until("done", func() bool { return e.itemStatus(1) == store.ItemDone })
	if v.callCount() != 2 || len(e.sessions.created()) != sessions || e.run(1).ID != r.ID {
		t.Fatalf("reverify: %d verify calls, sessions %d→%d, run %s", v.callCount(), sessions, len(e.sessions.created()), e.run(1).ID)
	}
	if got := e.st.verifyResults(r.ID); len(got) != 2 || got[1].Attempt != 2 || !got[1].Passed() {
		t.Fatalf("results %+v", got)
	}
	if e.run(1).Detail != "" {
		t.Fatalf("a passing attempt keeps the old detail %q", e.run(1).Detail)
	}
	// The owner acted: the queue stays paused until Resume.
	if e.queueStatus() != store.QueuePaused || e.itemStatus(2) != store.ItemQueued {
		t.Fatalf("after reverify: %v, queue %s", e.itemStatuses(), e.queueStatus())
	}
	if _, err := e.svc.Resume(e.ctx(), e.queue.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if e.itemStatus(2) != store.ItemRunning {
		t.Fatalf("after Resume: %v", e.itemStatuses())
	}
}

func TestReverifyThenApprovalGate(t *testing.T) {
	e, _, r := failedVerifyEnv(t, true)
	if _, err := e.svc.Reverify(e.ctx(), r.ItemID); err != nil {
		t.Fatal(err)
	}
	e.until("awaiting approval", func() bool { return e.itemStatus(1) == store.ItemAwaitingApproval })
}

func TestReverifyPreconditions(t *testing.T) {
	e, v := gatedEnv(t, store.ItemGates{}, store.ItemGates{VerifyCommand: "true"})
	e.d.SetNotifications(nil)
	items := e.items()
	// Queued: not needs attention.
	if _, err := e.svc.Reverify(e.ctx(), items[1].ID); queueStatus(err) != http.StatusConflict || !strings.Contains(err.Error(), "only for items that need attention") {
		t.Fatalf("queued: %v", err)
	}
	// Needs attention without a verify command.
	all := []string{store.ItemQueued, store.ItemRunning, store.ItemNeedsAttention}
	_, _ = e.st.TransitionQueueItem(e.ctx(), items[0].ID, all, store.ItemNeedsAttention)
	if _, err := e.svc.Reverify(e.ctx(), items[0].ID); queueStatus(err) != http.StatusConflict || !strings.Contains(err.Error(), "no verify command") {
		t.Fatalf("no command: %v", err)
	}
	// Needs attention with a verify command, but the run failed.
	_, _ = e.st.TransitionQueueItem(e.ctx(), items[1].ID, all, store.ItemRunning)
	run, _ := e.st.CreateRun(e.ctx(), items[1].ID, HashToken("x"), e.clock.Now())
	_, _ = e.st.TransitionRun(e.ctx(), run.ID, store.ActiveRunStatuses, store.RunFailed, "failed", nil)
	_, _ = e.st.TransitionQueueItem(e.ctx(), items[1].ID, all, store.ItemNeedsAttention)
	if _, err := e.svc.Reverify(e.ctx(), items[1].ID); queueStatus(err) != http.StatusConflict || !strings.Contains(err.Error(), "didn't achieve its goal") {
		t.Fatalf("failed run: %v", err)
	}
	if v.callCount() != 0 {
		t.Fatalf("verify ran %d times", v.callCount())
	}
}

func TestOverridesRefusedInGates(t *testing.T) {
	e, v := gatedEnv(t, store.ItemGates{VerifyCommand: "make test"}, store.ItemGates{RequiresApproval: true})
	e.svc.SetVerifyTimeout(10 * time.Minute)
	v.hold = true
	e.startQueue()
	e.achieve(1)
	<-v.started
	e.d.Sync()
	for _, action := range []string{ActionRetry, ActionSkip, ActionMarkDone} {
		_, err := e.svc.Override(e.ctx(), e.items()[0].ID, action)
		if queueStatus(err) != http.StatusConflict || !strings.Contains(err.Error(), "wait for verify, at most 10m") {
			t.Fatalf("%s while verifying: %v", action, err)
		}
	}
	v.release <- struct{}{}
	e.until("item 2 awaiting", func() bool {
		return e.itemStatus(2) == store.ItemAwaitingApproval || e.itemStatus(2) == store.ItemRunning
	})
	e.achieve(2)
	for _, action := range []string{ActionRetry, ActionSkip, ActionMarkDone} {
		_, err := e.svc.Override(e.ctx(), e.items()[1].ID, action)
		if queueStatus(err) != http.StatusConflict || !strings.Contains(err.Error(), "Reject it first") {
			t.Fatalf("%s while awaiting approval: %v", action, err)
		}
	}
}

func TestMarkDoneSkipsRemainingGates(t *testing.T) {
	e, v, r := failedVerifyEnv(t, true)
	if _, err := e.svc.Override(e.ctx(), r.ItemID, ActionMarkDone); err != nil {
		t.Fatal(err)
	}
	if e.itemStatus(1) != store.ItemDone || v.callCount() != 1 {
		t.Fatalf("mark done: %v, %d verify calls", e.itemStatuses(), v.callCount())
	}
}

func TestRetryReappliesGatesOnTheNewRun(t *testing.T) {
	e, v, r := failedVerifyEnv(t, false)
	if _, err := e.svc.Override(e.ctx(), r.ItemID, ActionRetry); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Resume(e.ctx(), e.queue.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	second := e.run(1)
	if second.ID == r.ID {
		t.Fatal("Retry didn't start a new run")
	}
	e.achieve(1)
	e.until("done", func() bool { return e.itemStatus(1) == store.ItemDone })
	if v.callCount() != 2 {
		t.Fatalf("the new run skipped its verify gate: %d calls", v.callCount())
	}
	if got := e.st.verifyResults(second.ID); len(got) != 1 || got[0].Attempt != 1 {
		t.Fatalf("the new run's attempts %+v", got)
	}
}

// Every pair of Re-run verify, Retry, Skip and Mark done, raced: exactly
// one applies; the other gets 409.
func TestOwnerActionsRace(t *testing.T) {
	actions := []string{"reverify", ActionRetry, ActionSkip, ActionMarkDone}
	for i, a := range actions {
		for _, b := range actions[i+1:] {
			t.Run(a+"-vs-"+b, func(t *testing.T) {
				e, v, r := failedVerifyEnv(t, false)
				v.hold = true // a winning reverify stays verifying
				act := func(name string) error {
					if name == "reverify" {
						_, err := e.svc.Reverify(context.Background(), r.ItemID)
						return err
					}
					_, err := e.svc.Override(context.Background(), r.ItemID, name)
					return err
				}
				var wg sync.WaitGroup
				errs := make([]error, 2)
				for k, name := range []string{a, b} {
					wg.Add(1)
					go func() { defer wg.Done(); errs[k] = act(name) }()
				}
				wg.Wait()
				ok := slices.IndexFunc(errs, func(err error) bool { return err == nil })
				lost := slices.IndexFunc(errs, func(err error) bool { return queueStatus(err) == http.StatusConflict })
				if ok < 0 || lost < 0 || ok == lost {
					t.Fatalf("results %v", errs)
				}
				if !strings.Contains(errs[lost].Error(), "this item is") && !strings.Contains(errs[lost].Error(), "it is ") {
					t.Fatalf("the loser's 409 doesn't name the state: %v", errs[lost])
				}
				v.release <- struct{}{}
			})
		}
	}
}
