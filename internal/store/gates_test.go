package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// V2-M4 T1: the gate columns, their limits and the widened CHECKs.
func TestQueueItemGatesStoreAndValidate(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, _ := s.CreateQueue(ctx, p.ID, "Milestones")

	plain, err := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal m1")
	if err != nil || plain.VerifyCommand != "" || plain.RequiresApproval || plain.Gated() {
		t.Fatalf("no gates by default: %+v, %v", plain, err)
	}
	var null bool
	if err := s.db.QueryRowContext(ctx, `SELECT verify_command IS NULL FROM queue_items WHERE id = $1`, plain.ID).Scan(&null); err != nil || !null {
		t.Fatalf("no verify command is stored as NULL: %v, %v", null, err)
	}

	gated, err := s.AddQueueItem(ctx, q.ID, "codex", "", "/goal m2", ItemGates{VerifyCommand: "  make test  ", RequiresApproval: true})
	if err != nil || gated.VerifyCommand != "make test" || !gated.RequiresApproval || !gated.Gated() {
		t.Fatalf("gated item: %+v, %v", gated, err)
	}
	if got, _ := s.QueueItem(ctx, gated.ID); got.VerifyCommand != "make test" || !got.RequiresApproval {
		t.Fatalf("read back: %+v", got)
	}

	blank, err := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal m3", ItemGates{VerifyCommand: "   "})
	if err != nil || blank.VerifyCommand != "" {
		t.Fatalf("blank verify command = none: %+v, %v", blank, err)
	}
	for _, bad := range []string{strings.Repeat("a", MaxVerifyCommandBytes+1), "make\ntest", "make\x00test", "make\rtest"} {
		if _, err := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal x", ItemGates{VerifyCommand: bad}); err == nil {
			t.Errorf("accepted verify command %q", bad)
		}
	}
	if _, err := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal x", ItemGates{VerifyCommand: strings.Repeat("a", MaxVerifyCommandBytes)}); err != nil {
		t.Errorf("4096-byte verify command refused: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE queue_items SET verify_command = '' WHERE id = $1`, plain.ID); err == nil {
		t.Error("the schema accepted an empty verify command (must be NULL)")
	}
}

func TestQueueItemGateEditMatrix(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, _ := s.CreateQueue(ctx, p.ID, "Milestones")
	it, _ := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal m1")
	verify, on, off, empty := "make test", true, false, ""
	instr := "/goal other"

	edited, err := s.UpdateQueueItem(ctx, it.ID, QueueItemUpdate{VerifyCommand: &verify, RequiresApproval: &on})
	if err != nil || edited.VerifyCommand != verify || !edited.RequiresApproval {
		t.Fatalf("gates on a queued item: %+v, %v", edited, err)
	}
	for _, c := range []struct {
		status   string
		gatesOK  bool
		othersOK bool
	}{
		{ItemQueued, true, true},
		{ItemNeedsAttention, true, false},
		{ItemRunning, false, false},
		{ItemVerifying, false, false},
		{ItemAwaitingApproval, false, false},
		{ItemDone, false, false},
		{ItemSkipped, false, false},
	} {
		if _, err := s.db.ExecContext(ctx, `UPDATE queue_items SET status = $2 WHERE id = $1`, it.ID, c.status); err != nil {
			t.Fatal(err)
		}
		_, err := s.UpdateQueueItem(ctx, it.ID, QueueItemUpdate{VerifyCommand: &empty, RequiresApproval: &off})
		if (err == nil) != c.gatesOK || (err != nil && !errors.Is(err, ErrConflict)) {
			t.Errorf("%s: gate edit = %v, want ok=%v", c.status, err, c.gatesOK)
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE queue_items SET verify_command = 'make test', requires_approval = TRUE, instruction = '/goal m1' WHERE id = $1`, it.ID)
		_, err = s.UpdateQueueItem(ctx, it.ID, QueueItemUpdate{Instruction: &instr, VerifyCommand: &empty})
		if (err == nil) != c.othersOK {
			t.Errorf("%s: instruction edit = %v, want ok=%v", c.status, err, c.othersOK)
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE queue_items SET instruction = '/goal m1' WHERE id = $1`, it.ID)
	}
}

func TestGateStatesAndVerifySourceAreValid(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, _ := s.CreateQueue(ctx, p.ID, "Milestones")
	it, _ := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal m1", ItemGates{VerifyCommand: "true", RequiresApproval: true})
	run, err := s.CreateRun(ctx, it.ID, tokenHash("gates"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range [][2]string{{ItemQueued, ItemRunning}, {ItemRunning, ItemVerifying}, {ItemVerifying, ItemAwaitingApproval}, {ItemAwaitingApproval, ItemDone}} {
		if _, err := s.TransitionQueueItem(ctx, it.ID, []string{step[0]}, step[1]); err != nil {
			t.Fatalf("%s → %s: %v", step[0], step[1], err)
		}
	}
	if _, err := s.AppendRunEvent(ctx, run.ID, SourceVerify, "verify_started", []byte(`{"attempt":1}`)); err != nil {
		t.Fatalf("verify source: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE queue_items SET status = 'approving' WHERE id = $1`, it.ID); err == nil {
		t.Error("the widened CHECK accepts an unknown item status")
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO run_events (run_id, machine_id, source, kind) VALUES ($1, 'host', 'shell', 'x')`, run.ID); err == nil {
		t.Error("the widened CHECK accepts an unknown source")
	}
}

func TestDeleteQueueRefusedWhileVerifying(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, _ := s.CreateQueue(ctx, p.ID, "Milestones")
	it, _ := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal m1", ItemGates{VerifyCommand: "true"})
	run, _ := s.CreateRun(ctx, it.ID, tokenHash("verifying"), time.Now())
	ended := time.Now()
	if _, err := s.TransitionRun(ctx, run.ID, ActiveRunStatuses, RunAchieved, "", &ended); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE queue_items SET status = 'verifying' WHERE id = $1`, it.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteQueue(ctx, q.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete while verifying = %v, want ErrConflict", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE queue_items SET status = 'awaiting_approval' WHERE id = $1`, it.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteQueue(ctx, q.ID); err != nil {
		t.Fatalf("delete while awaiting approval (nothing runs) = %v", err)
	}
}
