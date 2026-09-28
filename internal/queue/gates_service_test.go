package queue

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"hostbud/internal/store"
)

// V2-M4 T1: verify command validation and the gate edit matrix.
func TestServiceGateValidation(t *testing.T) {
	e := newServiceEnv(t)
	ctx := context.Background()
	q, _ := e.svc.Create(ctx, "project_a", "Q")
	for _, bad := range []string{`make 'test`, `sh -c "x`, `make test\`, strings.Repeat("a", 4097), "make\ntest", "make\x00"} {
		_, err := e.svc.AddItem(ctx, q.ID, "claude", "", "/goal x", store.ItemGates{VerifyCommand: bad})
		var qe *Error
		if !errors.As(err, &qe) || qe.Status != http.StatusBadRequest || !strings.Contains(qe.Hint, "sh -c") {
			t.Errorf("AddItem(verify %q) = %v", bad, err)
		}
	}
	it, err := e.svc.AddItem(ctx, q.ID, "claude", "", "/goal x", store.ItemGates{VerifyCommand: `sh -c 'make test && make lint'`, RequiresApproval: true})
	if err != nil || it.VerifyCommand != `sh -c 'make test && make lint'` || !it.RequiresApproval {
		t.Fatalf("valid gates: %+v, %v", it, err)
	}
	empty := ""
	if it, err := e.svc.UpdateItem(ctx, it.ID, store.QueueItemUpdate{VerifyCommand: &empty}); err != nil || it.VerifyCommand != "" {
		t.Fatalf("empty verify command = none: %+v, %v", it, err)
	}
	bad := `make 'x`
	if _, err := e.svc.UpdateItem(ctx, it.ID, store.QueueItemUpdate{VerifyCommand: &bad}); queueStatus(err) != http.StatusBadRequest {
		t.Fatalf("invalid verify edit: %v", err)
	}
}

func TestServiceGateEditMatrix(t *testing.T) {
	e := newServiceEnv(t)
	ctx := context.Background()
	q, _ := e.svc.Create(ctx, "project_a", "Q")
	it, _ := e.svc.AddItem(ctx, q.ID, "claude", "", "/goal x")
	verify, on, instr := "make test", true, "/goal y"
	all := []string{store.ItemQueued, store.ItemRunning, store.ItemVerifying, store.ItemAwaitingApproval, store.ItemDone, store.ItemNeedsAttention, store.ItemSkipped}
	for _, c := range []struct {
		status   string
		gates    int
		other    int
		contains string
	}{
		{store.ItemQueued, 0, 0, ""},
		{store.ItemNeedsAttention, 0, http.StatusConflict, "only its gates"},
		{store.ItemRunning, http.StatusConflict, http.StatusConflict, "gates are fixed while the item is running"},
		{store.ItemVerifying, http.StatusConflict, http.StatusConflict, "gates are fixed while the item is verifying"},
		{store.ItemAwaitingApproval, http.StatusConflict, http.StatusConflict, "gates are fixed while the item is awaiting approval"},
		{store.ItemDone, http.StatusConflict, http.StatusConflict, "can't be edited"},
		{store.ItemSkipped, http.StatusConflict, http.StatusConflict, "can't be edited"},
	} {
		if _, err := e.st.TransitionQueueItem(ctx, it.ID, all, c.status); err != nil {
			t.Fatal(err)
		}
		_, err := e.svc.UpdateItem(ctx, it.ID, store.QueueItemUpdate{VerifyCommand: &verify, RequiresApproval: &on})
		if queueStatus(err) != c.gates {
			t.Errorf("%s: gate edit = %v, want %d", c.status, err, c.gates)
		}
		_, err = e.svc.UpdateItem(ctx, it.ID, store.QueueItemUpdate{Instruction: &instr})
		if queueStatus(err) != c.other || (err != nil && !strings.Contains(err.Error(), c.contains)) {
			t.Errorf("%s: instruction edit = %v, want %d %q", c.status, err, c.other, c.contains)
		}
		if c.status == store.ItemQueued {
			orig := "/goal x"
			_, _ = e.svc.UpdateItem(ctx, it.ID, store.QueueItemUpdate{Instruction: &orig})
		}
	}
}

func TestServiceDeleteQueueRefusedWhileVerifying(t *testing.T) {
	e := newServiceEnv(t)
	ctx := context.Background()
	q, _ := e.svc.Create(ctx, "project_a", "Q")
	it, _ := e.svc.AddItem(ctx, q.ID, "claude", "", "/goal m1", store.ItemGates{VerifyCommand: "true"})
	_, _ = e.st.TransitionQueueItem(ctx, it.ID, []string{store.ItemQueued}, store.ItemVerifying)
	if err := e.svc.Delete(ctx, q.ID); queueStatus(err) != http.StatusConflict {
		t.Fatalf("delete while verifying: %v", err)
	}
}
