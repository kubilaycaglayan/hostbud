package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"hostbud/internal/notify"
	"hostbud/internal/store"
)

// V2-M4 completion gates in the dispatcher: after an achieved run, an item
// with gates goes achieved ⇒ verifying (if it has a verify command) ⇒
// awaiting_approval (if it needs approval) ⇒ done. The run stays achieved;
// its token is revoked at achieved as before and no stale timer runs.
//
// While an item is in its gates the queue stays running and waits (its
// next item doesn't start). verifying holds the queue's run slot (a
// command runs on the host); awaiting_approval frees it. A queue that was
// paused meanwhile (the owner, or a late achieved after stale) doesn't
// advance when the gates pass.

// GateStore is what the gates need from the store.
type GateStore interface {
	StartVerify(ctx context.Context, itemID, runID string, from []string) (int, store.QueueItem, error)
	FinishVerify(ctx context.Context, itemID, runID string, result []byte, to string, n *store.Notice) (store.QueueItem, error)
	VerifyEvents(ctx context.Context, runID string) ([]store.RunEvent, error)
	ItemsWithStatus(ctx context.Context, machineID, status string) ([]store.QueueItem, error)
	LatestRunForItem(ctx context.Context, itemID string) (store.Run, error)
	ResolveApproval(ctx context.Context, itemID, runID, to, kind string, payload []byte, n *store.Notice) (store.QueueItem, error)
}

// VerifyRunner runs a verify command (Verifier; a fake in tests).
type VerifyRunner interface {
	Run(ctx context.Context, machine, dir, command string) (VerifyResult, bool)
	Timeout() time.Duration
}

// SetVerifier connects the verify runner. Without one, a verify command
// fails the gate with an actionable detail.
func (d *Dispatcher) SetVerifier(v VerifyRunner) { d.verifier = v }

// enterGates moves an item whose run achieved into its first gate, from
// one of from (running, or needs_attention for a late achieved).
func (d *Dispatcher) enterGates(ctx context.Context, run store.Run, item store.QueueItem, queueID, source string, from []string) {
	if item.VerifyCommand != "" {
		d.startVerify(ctx, run, item.ID, queueID, from)
		return
	}
	d.awaitApproval(ctx, run, item, queueID, from)
}

// startVerify claims the next attempt (stored before it runs) and runs the
// command off the dispatcher goroutine; the result comes back as work.
func (d *Dispatcher) startVerify(ctx context.Context, run store.Run, itemID, queueID string, from []string) bool {
	attempt, item, err := d.store.StartVerify(ctx, itemID, run.ID, from)
	if err != nil {
		if !errors.Is(err, store.ErrConflict) {
			d.log.Error("queue: start verify", "item", itemID, "err", err)
		}
		return false
	}
	d.log.Info("verify started", "item", item.ID, "attempt", attempt)
	d.service.Publish(ctx, "verifying", queueID)
	dir, machine := "", run.MachineID
	if q, err := d.store.Queue(ctx, queueID); err == nil {
		if p, err := d.store.Project(ctx, q.ProjectID); err == nil {
			dir = p.Path
		}
	}
	finish := func(res VerifyResult) {
		res.Attempt = attempt
		d.enqueue(func(ctx context.Context) { d.verifyDone(ctx, run, item, queueID, res) })
	}
	switch {
	case dir == "":
		finish(VerifyResult{Outcome: VerifyMissingDir, Detail: "the item's project is gone — Mark done or Skip the item"})
		return true
	case d.verifier == nil:
		finish(VerifyResult{Outcome: VerifySSHFailed, Detail: "verify didn't run: no verify runner is configured"})
		return true
	}
	base := d.base
	if base == nil {
		base = ctx
	}
	d.verifying.Add(1)
	go func() {
		defer d.verifying.Done()
		res, ok := d.verifier.Run(base, machine, dir, item.VerifyCommand)
		if !ok || base.Err() != nil {
			return // shutdown: the attempt stays open; recovery flags it
		}
		finish(res)
	}()
	return true
}

// verifyDone records an attempt's result and moves the item on: the
// approval gate or done when it passed, else needs attention with the
// queue paused.
func (d *Dispatcher) verifyDone(ctx context.Context, run store.Run, item store.QueueItem, queueID string, res VerifyResult) {
	exit := -1
	if res.ExitCode != nil {
		exit = *res.ExitCode
	}
	d.log.Info("verify finished", "item", item.ID, "attempt", res.Attempt, "exit", exit, "duration_ms", res.DurationMs)
	payload := verifyPayload(res)
	current, err := d.store.QueueItem(ctx, item.ID)
	if err != nil {
		return
	}
	if !res.Passed() {
		notice := d.gateNotice(ctx, notify.OutcomeVerifyFailed, res.Attempt, run, current, queueID)
		if _, err := d.store.FinishVerify(ctx, item.ID, run.ID, payload, store.ItemNeedsAttention, d.outbox(notice)); err != nil {
			return
		}
		d.setRunDetail(ctx, run, queueID, res.Detail)
		d.gateAttention(ctx, queueID, notice)
		return
	}
	d.setRunDetail(ctx, run, queueID, "")
	if current.RequiresApproval {
		notice := d.gateNotice(ctx, notify.OutcomeAwaitingApproval, 0, run, current, queueID)
		if _, err := d.store.FinishVerify(ctx, item.ID, run.ID, payload, store.ItemAwaitingApproval, d.outbox(notice)); err != nil {
			return
		}
		d.approvalPending(ctx, queueID, notice)
		return
	}
	notice := d.notice(ctx, notify.KindDone, run, current, queueID)
	if _, err := d.store.FinishVerify(ctx, item.ID, run.ID, payload, store.ItemDone, d.outbox(notice)); err != nil {
		return
	}
	d.gatesPassed(ctx, queueID, notice)
}

// awaitApproval moves the item into the approval gate.
func (d *Dispatcher) awaitApproval(ctx context.Context, run store.Run, item store.QueueItem, queueID string, from []string) {
	notice := d.gateNotice(ctx, notify.OutcomeAwaitingApproval, 0, run, item, queueID)
	if _, err := d.store.TransitionQueueItemNotify(ctx, item.ID, from, store.ItemAwaitingApproval, d.outbox(notice)); err != nil {
		d.service.Publish(ctx, "run_achieved", queueID)
		return
	}
	d.approvalPending(ctx, queueID, notice)
}

// approvalPending: the queue waits for the owner; its slot is free.
func (d *Dispatcher) approvalPending(ctx context.Context, queueID string, notice *notify.Payload) {
	d.wakePush(notice)
	d.service.PublishNotice(ctx, "awaiting_approval", queueID, notice)
	if d.slots() {
		d.dispatch(ctx, store.SourceVerify)
	}
}

// gatesPassed: the item is done; the queue takes its next item (with
// slots it goes to the back of the line now) unless it was paused.
func (d *Dispatcher) gatesPassed(ctx context.Context, queueID string, notice *notify.Payload) {
	d.wakePush(notice)
	d.service.PublishNotice(ctx, "item_done", queueID, notice)
	d.next(ctx, queueID, store.SourceVerify)
}

// gateAttention pauses the queue after a failed gate (V2-M1 rules) and
// hands out the freed slot.
func (d *Dispatcher) gateAttention(ctx context.Context, queueID string, notice *notify.Payload) {
	d.wakePush(notice)
	_, _ = d.store.TransitionQueue(ctx, queueID, []string{store.QueueRunning}, store.QueuePaused)
	d.service.PublishNotice(ctx, "needs_attention", queueID, notice)
	if d.slots() {
		d.dispatch(ctx, store.SourceVerify)
	}
}

// setRunDetail shows a gate's outcome as the run's detail (the panel's
// "why"); "" clears it after a passing attempt.
func (d *Dispatcher) setRunDetail(ctx context.Context, run store.Run, queueID, detail string) {
	updated, err := d.store.UpdateRun(ctx, run.ID, store.RunUpdate{Detail: &detail})
	if err == nil {
		d.publishRun(ctx, updated, queueID)
	}
}

// gateNotice is the needs-attention notice of a gate (outcome
// awaiting_approval or verify_failed); nil while every account is off.
func (d *Dispatcher) gateNotice(ctx context.Context, outcome string, attempt int, run store.Run, item store.QueueItem, queueID string) *notify.Payload {
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
		Kind: notify.KindAttention, RunID: run.ID, QueueID: queueID, ItemID: item.ID, Project: name, Position: item.Position,
		Outcome: outcome, Attempt: attempt,
	})
	if err != nil {
		return nil
	}
	return &p
}

// verifyPayload encodes a result for run_events without HTML escaping, so
// the 16 KiB tail stays well inside the 64 KiB payload cap.
func verifyPayload(res VerifyResult) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(res)
	return bytes.TrimSpace(b.Bytes())
}

// recoverGates handles items that were verifying when hostbud stopped: an
// attempt that started but has no result isn't re-run (it may have had
// effects); the item needs attention. One without an open attempt starts
// one, once.
func (d *Dispatcher) recoverGates(ctx context.Context) {
	var items []store.QueueItem
	for _, machine := range d.machines(ctx) {
		mine, err := d.store.ItemsWithStatus(ctx, machine, store.ItemVerifying)
		if err != nil {
			d.log.Error("queue recovery: verifying items", "err", err)
			return
		}
		items = append(items, mine...)
	}
	for _, item := range items {
		run, err := d.store.LatestRunForItem(ctx, item.ID)
		if err != nil {
			continue
		}
		open := openAttempt(d.store.VerifyEvents(ctx, run.ID))
		if open == 0 {
			d.startVerify(ctx, run, item.ID, item.QueueID, []string{store.ItemVerifying})
			continue
		}
		res := VerifyResult{Attempt: open, Outcome: VerifyInterrupted, Detail: "hostbud restarted during verify — Re-run verify"}
		d.log.Info("verify interrupted by a restart", "item", item.ID, "attempt", open)
		d.verifyDone(ctx, run, item, item.QueueID, res)
	}
}

// openAttempt returns the attempt that started without a result, or 0.
func openAttempt(events []store.RunEvent, err error) int {
	if err != nil {
		return 0
	}
	started, finished := 0, map[int]bool{}
	for _, e := range events {
		var p struct {
			Attempt int `json:"attempt"`
		}
		if json.Unmarshal(e.Payload, &p) != nil {
			continue
		}
		switch e.Kind {
		case store.KindVerifyStarted:
			started = max(started, p.Attempt)
		case store.KindVerifyResult:
			finished[p.Attempt] = true
		}
	}
	if started == 0 || finished[started] {
		return 0
	}
	return started
}

// Actor is the account behind an owner action (V2-M4 approvals).
type Actor struct {
	ID    string
	Email string
}

// ResolveApproval applies Approve (done, then the queue advances) or
// Reject (needs attention, queue paused) to an item awaiting approval, on
// the dispatcher goroutine. store.ErrConflict: another action got there
// first.
func (d *Dispatcher) ResolveApproval(ctx context.Context, itemID string, approve bool, actor Actor) error {
	done := make(chan error, 1)
	d.enqueue(func(ctx context.Context) { done <- d.resolveApproval(ctx, itemID, approve, actor) })
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Dispatcher) resolveApproval(ctx context.Context, itemID string, approve bool, actor Actor) error {
	item, err := d.store.QueueItem(ctx, itemID)
	if err != nil {
		return err
	}
	if item.Status != store.ItemAwaitingApproval {
		return store.ErrConflict
	}
	run, err := d.store.LatestRunForItem(ctx, itemID)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"account": actor.ID})
	if approve {
		notice := d.notice(ctx, notify.KindDone, run, item, item.QueueID)
		if _, err := d.store.ResolveApproval(ctx, itemID, run.ID, store.ItemDone, store.KindApproved, payload, d.outbox(notice)); err != nil {
			return err
		}
		d.log.Info("item approved", "item", itemID)
		d.setRunDetail(ctx, run, item.QueueID, "approved by "+actor.Email)
		d.gatesPassed(ctx, item.QueueID, notice)
		return nil
	}
	if _, err := d.store.ResolveApproval(ctx, itemID, run.ID, store.ItemNeedsAttention, store.KindRejected, payload, nil); err != nil {
		return err
	}
	d.log.Info("item rejected", "item", itemID)
	d.setRunDetail(ctx, run, item.QueueID, "rejected by "+actor.Email)
	// The owner acted: no notification; the queue pauses like any
	// needs-attention item.
	d.gateAttention(ctx, item.QueueID, nil)
	return nil
}

// Reverify starts a new verify attempt for a needs-attention item whose
// latest run achieved, on the dispatcher goroutine (the owner's Re-run
// verify). store.ErrConflict: the item left needs_attention first.
func (d *Dispatcher) Reverify(ctx context.Context, itemID string) error {
	done := make(chan error, 1)
	d.enqueue(func(ctx context.Context) {
		item, err := d.store.QueueItem(ctx, itemID)
		if err != nil {
			done <- err
			return
		}
		run, err := d.store.LatestRunForItem(ctx, itemID)
		switch {
		case err != nil:
			done <- err
		case item.Status != store.ItemNeedsAttention || run.Status != store.RunAchieved || item.VerifyCommand == "":
			done <- store.ErrConflict
		case !d.startVerify(ctx, run, itemID, item.QueueID, []string{store.ItemNeedsAttention}):
			done <- store.ErrConflict
		default:
			d.log.Info("verify re-run by the owner", "item", itemID)
			done <- nil
		}
	})
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
