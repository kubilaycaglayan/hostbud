package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func queueFixture(t *testing.T) (*Store, Project) {
	t.Helper()
	ctx := context.Background()
	s := openTemp(t, t.TempDir())
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProject(ctx, HostMachineID, "/home/dev/app", "app")
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

func tokenHash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func itemIDs(items []QueueItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func positions(items []QueueItem) []int {
	var out []int
	for _, it := range items {
		out = append(out, it.Position)
	}
	return out
}

func TestNewULID(t *testing.T) {
	at := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	a, err := NewULID(at)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewULID(at.Add(time.Millisecond))
	if len(a) != 26 || strings.Trim(a, crockford) != "" || a[0] > '7' {
		t.Fatalf("ULID %q is not 26 Crockford characters", a)
	}
	if a >= b {
		t.Fatalf("ULIDs don't sort by time: %q >= %q", a, b)
	}
	// The time prefix (10 characters) encodes the millisecond timestamp.
	c, _ := NewULID(at)
	if a[:10] != c[:10] || a == c {
		t.Fatalf("same-millisecond ULIDs %q, %q", a, c)
	}
}

func TestQueueSchemaChecksRejectBadValues(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, err := s.CreateQueue(ctx, p.ID, "Milestones")
	if err != nil {
		t.Fatal(err)
	}
	it, err := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal ship M1")
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateRun(ctx, it.ID, tokenHash("a"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`UPDATE queues SET status = 'stopped' WHERE id = '` + q.ID + `'`,
		`UPDATE queue_items SET status = 'waiting' WHERE id = '` + it.ID + `'`,
		`UPDATE queue_items SET agent = 'gemini' WHERE id = '` + it.ID + `'`,
		`UPDATE runs SET status = 'paused' WHERE id = '` + run.ID + `'`,
		`UPDATE runs SET token_hash = '\x00' WHERE id = '` + run.ID + `'`,
		`INSERT INTO run_events (run_id, machine_id, source, kind) VALUES ('` + run.ID + `', 'host', 'browser', 'x')`,
		`INSERT INTO run_events (run_id, machine_id, source, kind, payload_json) VALUES ('` + run.ID + `', 'host', 'hook', 'x', repeat('a', 65537))`,
		`INSERT INTO queue_items (id, queue_id, machine_id, position, agent, instruction) VALUES ('dup', '` + q.ID + `', 'host', 1, 'codex', 'x')`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err == nil {
			t.Errorf("accepted: %s", stmt)
		}
	}
	if _, err := s.TransitionQueue(ctx, q.ID, []string{QueueIdle}, "stopped"); err == nil {
		t.Error("TransitionQueue accepted an unknown status")
	}
	if _, err := s.TransitionRun(ctx, run.ID, []string{RunStarting}, "paused", "", nil); err == nil {
		t.Error("TransitionRun accepted an unknown status")
	}
	if _, err := s.AppendRunEvent(ctx, run.ID, "browser", "x", nil); err == nil {
		t.Error("AppendRunEvent accepted an unknown source")
	}
	if _, err := s.AddQueueItem(ctx, q.ID, "gemini", "", "/goal x"); err == nil {
		t.Error("AddQueueItem accepted an unknown agent")
	}
	if _, err := s.CreateRun(ctx, it.ID, []byte("short"), time.Now()); err == nil {
		t.Error("CreateRun accepted a token hash that isn't SHA-256")
	}
}

func TestQueueSchemaHasMachineIDsAndIndexes(t *testing.T) {
	ctx := context.Background()
	s, _ := queueFixture(t)
	for _, table := range []string{"queues", "queue_items", "runs", "run_events"} {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = 'machine_id'`, table).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s.machine_id: %d, %v", table, n, err)
		}
	}
	for _, index := range []string{"runs_item", "runs_status", "run_events_run_created", "queues_project_name"} {
		var ok bool
		if err := s.db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, index).Scan(&ok); err != nil || !ok {
			t.Errorf("index %s missing: %v", index, err)
		}
	}
	// The v1 sketch's tasks table is superseded; machine_capacity arrived
	// with V2-M2 (0006).
	var ok bool
	if err := s.db.QueryRowContext(ctx, `SELECT to_regclass('tasks') IS NOT NULL`).Scan(&ok); err != nil || ok {
		t.Errorf("superseded table tasks exists")
	}
	if err := s.db.QueryRowContext(ctx, `SELECT to_regclass('machine_capacity') IS NOT NULL`).Scan(&ok); err != nil || !ok {
		t.Errorf("machine_capacity missing: %v", err)
	}
}

func TestQueueItemsReorderAndDeleteKeepPositionsDense(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, _ := s.CreateQueue(ctx, p.ID, "Milestones")
	var ids []string
	for _, instr := range []string{"/goal m1", "/goal m2", "/goal m3", "/goal m4"} {
		it, err := s.AddQueueItem(ctx, q.ID, "claude", "--model 'opus 4'", instr)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, it.ID)
	}
	items, _ := s.QueueItems(ctx, q.ID)
	if !slices.Equal(positions(items), []int{1, 2, 3, 4}) || items[0].Flags != "--model 'opus 4'" {
		t.Fatalf("appended items: %+v", items)
	}

	// Item 1 is done: a reorder must list exactly the queued items 2–4.
	if _, err := s.TransitionQueueItem(ctx, ids[0], []string{ItemQueued}, ItemDone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReorderQueueItems(ctx, q.ID, []string{ids[3], ids[2], ids[1], ids[0]}); !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("reorder with a done item: %v", err)
	}
	if _, err := s.ReorderQueueItems(ctx, q.ID, []string{ids[3], ids[3], ids[1]}); !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("reorder with a duplicate: %v", err)
	}
	items, err := s.ReorderQueueItems(ctx, q.ID, []string{ids[3], ids[1], ids[2]})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(itemIDs(items), []string{ids[0], ids[3], ids[1], ids[2]}) || !slices.Equal(positions(items), []int{1, 2, 3, 4}) {
		t.Fatalf("after reorder: %v %v", itemIDs(items), positions(items))
	}

	// Deleting renumbers without gaps; only queued items can be deleted.
	if err := s.DeleteQueueItem(ctx, ids[0]); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete done item: %v", err)
	}
	if err := s.DeleteQueueItem(ctx, ids[3]); err != nil {
		t.Fatal(err)
	}
	items, _ = s.QueueItems(ctx, q.ID)
	if !slices.Equal(itemIDs(items), []string{ids[0], ids[1], ids[2]}) || !slices.Equal(positions(items), []int{1, 2, 3}) {
		t.Fatalf("after delete: %v %v", itemIDs(items), positions(items))
	}
	next, err := s.AddQueueItem(ctx, q.ID, "codex", "", "/goal m5")
	if err != nil || next.Position != 4 {
		t.Fatalf("append after delete: %+v, %v", next, err)
	}
	first, err := s.FirstQueuedItem(ctx, q.ID)
	if err != nil || first.ID != ids[1] {
		t.Fatalf("first queued: %+v, %v", first, err)
	}
}

func TestQueueItemEditsOnlyWhileQueued(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, _ := s.CreateQueue(ctx, p.ID, "Milestones")
	it, _ := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal m1")
	instr, agent := "/goal m1 and docs", "codex"
	edited, err := s.UpdateQueueItem(ctx, it.ID, QueueItemUpdate{Instruction: &instr, Agent: &agent})
	if err != nil || edited.Instruction != instr || edited.Agent != "codex" || edited.Flags != "" {
		t.Fatalf("edit queued: %+v, %v", edited, err)
	}
	bad := "gemini"
	if _, err := s.UpdateQueueItem(ctx, it.ID, QueueItemUpdate{Agent: &bad}); err == nil {
		t.Fatal("edit accepted an unknown agent")
	}
	for _, status := range []string{ItemRunning, ItemDone, ItemNeedsAttention, ItemSkipped} {
		if _, err := s.db.ExecContext(ctx, `UPDATE queue_items SET status = $2 WHERE id = $1`, it.ID, status); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpdateQueueItem(ctx, it.ID, QueueItemUpdate{Instruction: &instr}); !errors.Is(err, ErrConflict) {
			t.Errorf("edit %s item: %v", status, err)
		}
	}
}

func TestGuardedTransitionsRefuseWrongSourceState(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, _ := s.CreateQueue(ctx, p.ID, "Milestones")
	it, _ := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal m1")
	if _, err := s.TransitionQueue(ctx, q.ID, []string{QueuePaused}, QueueRunning); !errors.Is(err, ErrConflict) {
		t.Fatalf("resume an idle queue: %v", err)
	}
	if got, err := s.TransitionQueue(ctx, q.ID, []string{QueueIdle, QueueFinished}, QueueRunning); err != nil || got.Status != QueueRunning {
		t.Fatalf("start: %+v, %v", got, err)
	}
	if _, err := s.TransitionQueue(ctx, "queue_missing", []string{QueueIdle}, QueueRunning); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing queue: %v", err)
	}
	if _, err := s.TransitionQueueItem(ctx, it.ID, []string{ItemNeedsAttention}, ItemSkipped); !errors.Is(err, ErrConflict) {
		t.Fatalf("skip a queued item: %v", err)
	}
	run, _ := s.CreateRun(ctx, it.ID, tokenHash("t1"), time.Now())
	if run.Status != RunStarting || run.MachineID != HostMachineID || len(run.ID) != 26 {
		t.Fatalf("new run: %+v", run)
	}
	if _, err := s.TransitionRun(ctx, run.ID, []string{RunRunning}, RunAchieved, "", nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("achieve a starting run: %v", err)
	}
	ended := time.Now()
	got, err := s.TransitionRun(ctx, run.ID, []string{RunStarting}, RunFailed, "claude not found on the host", &ended)
	if err != nil || got.Status != RunFailed || got.Detail != "claude not found on the host" || got.EndedAt == nil {
		t.Fatalf("fail: %+v, %v", got, err)
	}
	if _, err := s.TransitionRun(ctx, run.ID, []string{RunStarting, RunRunning}, RunAchieved, "", nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("achieve an ended run: %v", err)
	}
}

// Two goroutines race one guarded transition: exactly one wins.
func TestGuardedTransitionRaceHasOneWinner(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, _ := s.CreateQueue(ctx, p.ID, "Milestones")
	it, _ := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal m1")
	for round := range 10 {
		run, err := s.CreateRun(ctx, it.ID, tokenHash("race"+string(rune('a'+round))), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		results := make([]error, 2)
		for i := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, results[i] = s.TransitionRun(ctx, run.ID, []string{RunStarting, RunRunning}, RunAchieved, "", nil)
			}()
		}
		wg.Wait()
		wins := 0
		for _, err := range results {
			switch {
			case err == nil:
				wins++
			case !errors.Is(err, ErrConflict):
				t.Fatalf("round %d: %v", round, err)
			}
		}
		if wins != 1 {
			t.Fatalf("round %d: %d winners, want 1 (%v)", round, wins, results)
		}
	}
}

func TestRunLookupsUpdatesAndEvents(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, _ := s.CreateQueue(ctx, p.ID, "Milestones")
	it, _ := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal m1")
	old, _ := s.CreateRun(ctx, it.ID, tokenHash("old"), time.Now().Add(-time.Minute))
	if _, err := s.TransitionRun(ctx, old.ID, []string{RunStarting}, RunCancelled, "retried", nil); err != nil {
		t.Fatal(err)
	}
	run, _ := s.CreateRun(ctx, it.ID, tokenHash("new"), time.Now())
	if got, err := s.RunByTokenHash(ctx, tokenHash("new")); err != nil || got.ID != run.ID {
		t.Fatalf("by token: %+v, %v", got, err)
	}
	if _, err := s.RunByTokenHash(ctx, tokenHash("nope")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown token: %v", err)
	}
	if got, err := s.ActiveRunForItem(ctx, it.ID); err != nil || got.ID != run.ID {
		t.Fatalf("active for item: %+v, %v", got, err)
	}
	if active, err := s.ActiveRuns(ctx); err != nil || len(active) != 1 || active[0].ID != run.ID {
		t.Fatalf("active runs: %+v, %v", active, err)
	}
	latest, err := s.LatestRuns(ctx, q.ID)
	if err != nil || latest[it.ID].ID != run.ID {
		t.Fatalf("latest: %+v, %v", latest, err)
	}

	name, sid, path, version, detail := "app-q1", "sess-1", "/home/dev/.claude/projects/x/sess-1.jsonl", "2.1.283", "waiting"
	offset, at := int64(1234), time.Now().Truncate(time.Microsecond)
	got, err := s.UpdateRun(ctx, run.ID, RunUpdate{SessionName: &name, AgentSessionID: &sid, TranscriptPath: &path, TranscriptOffset: &offset, ClientVersion: &version, Detail: &detail, LastSignalAt: &at})
	if err != nil || got.SessionName != name || got.AgentSessionID != sid || got.TranscriptPath != path || got.TranscriptOffset != offset ||
		got.ClientVersion != version || got.Detail != detail || got.LastSignalAt == nil || !got.LastSignalAt.Equal(at) {
		t.Fatalf("update: %+v, %v", got, err)
	}

	for i, kind := range []string{"session_start", "turn_end"} {
		if _, err := s.AppendRunEvent(ctx, run.ID, SourceHook, kind, []byte(`{"n":`+string(rune('0'+i))+`}`)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AppendRunEvent(ctx, run.ID, SourceHook, "turn_end", []byte(`{"a":"`+strings.Repeat("x", MaxRunEventPayload)+`"}`)); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("oversized payload: %v", err)
	}
	if _, err := s.AppendRunEvent(ctx, run.ID, SourceHook, "turn_end", []byte(`not json`)); err == nil {
		t.Fatal("non-JSON payload accepted")
	}
	events, err := s.RunEvents(ctx, run.ID, 10)
	if err != nil || len(events) != 2 || events[0].Kind != "turn_end" || string(events[0].Payload) != `{"n":1}` {
		t.Fatalf("events newest first: %+v, %v", events, err)
	}
	if events, _ := s.RunEvents(ctx, run.ID, 1); len(events) != 1 {
		t.Fatalf("limit: %d events", len(events))
	}
}

func TestLLMClaimsBudgetAndGuardedFlagOnlyResult(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, _ := s.CreateQueue(ctx, p.ID, "llm")
	item, _ := s.AddQueueItem(ctx, q.ID, "claude", "", "goal")
	run, _ := s.CreateRun(ctx, item.ID, tokenHash("llm"), time.Now().Add(-time.Hour))
	if _, err := s.TransitionRun(ctx, run.ID, []string{RunStarting}, RunRunning, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransitionQueueItem(ctx, item.ID, []string{ItemQueued}, ItemRunning); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimLLM(ctx, run.ID, nil, 2, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	claimed, err = s.ClaimLLM(ctx, run.ID, nil, 2, time.Minute)
	if err != nil || claimed {
		t.Fatalf("duplicate claim=%v err=%v", claimed, err)
	}
	result := []byte(`{"label":"completed","reason":"looks done"}`)
	inserted, got, queueID, err := s.FinishLLM(ctx, run.ID, nil, result)
	if err != nil || !inserted || got.ID != run.ID || queueID != q.ID {
		t.Fatalf("finish=%v run=%+v queue=%q err=%v", inserted, got, queueID, err)
	}
	unchanged, _ := s.QueueItem(ctx, item.ID)
	unchangedRun, _ := s.Run(ctx, run.ID)
	unchangedQueue, _ := s.Queue(ctx, q.ID)
	if unchanged.Status != ItemRunning || unchangedRun.Status != RunRunning || unchangedQueue.Status != QueueIdle {
		t.Fatalf("LLM changed state: item=%s run=%s queue=%s", unchanged.Status, unchangedRun.Status, unchangedQueue.Status)
	}
}

func TestLLMStaleRunRespectsQuietAfterACompletedClaim(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, _ := s.CreateQueue(ctx, p.ID, "llm-stale-quiet")
	item, _ := s.AddQueueItem(ctx, q.ID, "claude", "", "goal")
	run, _ := s.CreateRun(ctx, item.ID, tokenHash("llm-stale-quiet"), time.Now().Add(-time.Hour))
	_, _ = s.TransitionRun(ctx, run.ID, []string{RunStarting}, RunRunning, "", nil)
	_, _ = s.TransitionQueueItem(ctx, item.ID, []string{ItemQueued}, ItemRunning)
	if claimed, err := s.ClaimLLM(ctx, run.ID, nil, 2, time.Hour); err != nil || !claimed {
		t.Fatalf("initial running claim=%v err=%v", claimed, err)
	}
	if _, err := s.TransitionRun(ctx, run.ID, []string{RunRunning}, RunStale, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransitionQueueItem(ctx, item.ID, []string{ItemRunning}, ItemNeedsAttention); err != nil {
		t.Fatal(err)
	}
	if inserted, _, _, err := s.FinishLLM(ctx, run.ID, nil, []byte(`{"label":"waiting_input","reason":"waiting"}`)); err != nil || !inserted {
		t.Fatalf("finish=%v err=%v", inserted, err)
	}
	if claimed, err := s.ClaimLLM(ctx, run.ID, nil, 2, time.Hour); err != nil || claimed {
		t.Fatalf("stale run was reclaimed before quiet interval: claim=%v err=%v", claimed, err)
	}
}

func TestLLMEligibleRunsIncludesOnlyLatestRunningAndStalePairs(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, err := s.CreateQueue(ctx, p.ID, "llm-eligible")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for i, pair := range []struct{ runStatus, itemStatus string }{
		{RunRunning, ItemRunning},
		{RunStale, ItemNeedsAttention},
		{RunStarting, ItemRunning},
		{RunAchieved, ItemDone},
		{RunRunning, ItemVerifying},
		{RunRunning, ItemAwaitingApproval},
		{RunStale, ItemRunning},
		{RunRunning, ItemNeedsAttention},
	} {
		item, err := s.AddQueueItem(ctx, q.ID, "claude", "", fmt.Sprintf("eligible %d", i))
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now().Add(time.Duration(i) * time.Second)
		run, err := s.CreateRun(ctx, item.ID, tokenHash(fmt.Sprintf("eligible-%d", i)), started)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE runs SET status=$2 WHERE id=$1`, run.ID, pair.runStatus); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE queue_items SET status=$2 WHERE id=$1`, item.ID, pair.itemStatus); err != nil {
			t.Fatal(err)
		}
		want[run.ID] = pair.runStatus == RunRunning && pair.itemStatus == ItemRunning || pair.runStatus == RunStale && pair.itemStatus == ItemNeedsAttention
	}
	// The original running attempt is excluded once a newer retry exists.
	item, err := s.AddQueueItem(ctx, q.ID, "claude", "", "latest attempt")
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.CreateRun(ctx, item.ID, tokenHash("eligible-old"), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE runs SET status=$2 WHERE id=$1`, old.ID, RunRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE queue_items SET status=$2 WHERE id=$1`, item.ID, ItemRunning); err != nil {
		t.Fatal(err)
	}
	want[old.ID] = false
	newer, err := s.CreateRun(ctx, item.ID, tokenHash("eligible-new"), time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	want[newer.ID] = false

	runs, err := s.LLMEligibleRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, run := range runs {
		got[run.ID] = true
	}
	for id, eligible := range want {
		if got[id] != eligible {
			t.Errorf("run %s eligible=%v want=%v", id, got[id], eligible)
		}
	}
}

func TestRecoveredLLMClaimDoesNotRunAgain(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, _ := s.CreateQueue(ctx, p.ID, "llm-recover")
	item, _ := s.AddQueueItem(ctx, q.ID, "claude", "", "goal")
	run, _ := s.CreateRun(ctx, item.ID, tokenHash("llm-recover"), time.Now().Add(-time.Hour))
	_, _ = s.TransitionRun(ctx, run.ID, []string{RunStarting}, RunRunning, "", nil)
	_, _ = s.TransitionQueueItem(ctx, item.ID, []string{ItemQueued}, ItemRunning)
	if claimed, err := s.ClaimLLM(ctx, run.ID, nil, 2, time.Minute); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE run_events SET created_at=now()-interval '71 seconds' WHERE run_id=$1 AND source='llm' AND kind='llm_started'`, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverLLMClaims(ctx); err != nil {
		t.Fatal(err)
	}
	events, err := s.RunEvents(ctx, run.ID, 10)
	if err != nil || len(events) != 2 || events[0].Kind != KindLLMResult || !strings.Contains(string(events[0].Payload), "hostbud restarted during classification") || !strings.Contains(string(events[0].Payload), `"recovered":true`) {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if claimed, err := s.ClaimLLM(ctx, run.ID, nil, 2, time.Second); err != nil || claimed {
		t.Fatalf("recovered claim was repeated: claim=%v err=%v", claimed, err)
	}
}

func TestLLMResultAfterNewSignalIsDiscardedWithoutQueueMutation(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, _ := s.CreateQueue(ctx, p.ID, "llm-race")
	it, _ := s.AddQueueItem(ctx, q.ID, "claude", "", "goal")
	run, _ := s.CreateRun(ctx, it.ID, tokenHash("llm-race"), time.Now().Add(-time.Hour))
	_, _ = s.TransitionRun(ctx, run.ID, []string{RunStarting}, RunRunning, "", nil)
	_, _ = s.TransitionQueueItem(ctx, it.ID, []string{ItemQueued}, ItemRunning)
	claimed, err := s.ClaimLLM(ctx, run.ID, nil, 2, time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	signal := time.Now().UTC()
	if _, err = s.UpdateRun(ctx, run.ID, RunUpdate{LastSignalAt: &signal}); err != nil {
		t.Fatal(err)
	}
	inserted, _, _, err := s.FinishLLM(ctx, run.ID, nil, []byte(`{"label":"completed","reason":"looks done"}`))
	if err != nil || inserted {
		t.Fatalf("finish=%v err=%v", inserted, err)
	}
	events, err := s.RunEvents(ctx, run.ID, 10)
	if err != nil || len(events) != 2 || events[0].Kind != KindLLMDiscarded || events[1].Kind != KindLLMStarted {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	item, _ := s.QueueItem(ctx, it.ID)
	runAfter, _ := s.Run(ctx, run.ID)
	queue, _ := s.Queue(ctx, q.ID)
	if item.Status != ItemRunning || runAfter.Status != RunRunning || queue.Status != QueueIdle {
		t.Fatalf("state changed item=%s run=%s queue=%s", item.Status, runAfter.Status, queue.Status)
	}
}

func TestLLMResultsNeverChangeQueueStateForAnyLabel(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, err := s.CreateQueue(ctx, p.ID, "llm-labels")
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"running", "waiting_input", "blocked", "completed", "failed", "unknown"} {
		t.Run(label, func(t *testing.T) {
			item, err := s.AddQueueItem(ctx, q.ID, "claude", "", "label "+label)
			if err != nil {
				t.Fatal(err)
			}
			run, err := s.CreateRun(ctx, item.ID, tokenHash("label-"+label), time.Now().Add(-time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.TransitionRun(ctx, run.ID, []string{RunStarting}, RunRunning, "", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := s.TransitionQueueItem(ctx, item.ID, []string{ItemQueued}, ItemRunning); err != nil {
				t.Fatal(err)
			}
			if claimed, err := s.ClaimLLM(ctx, run.ID, nil, 2, time.Minute); err != nil || !claimed {
				t.Fatalf("claim=%v err=%v", claimed, err)
			}
			beforeItem, _ := s.QueueItem(ctx, item.ID)
			beforeRun, _ := s.Run(ctx, run.ID)
			beforeQueue, _ := s.Queue(ctx, q.ID)
			result := []byte(fmt.Sprintf(`{"label":%q,"reason":"advisory"}`, label))
			inserted, _, _, err := s.FinishLLM(ctx, run.ID, nil, result)
			if err != nil || !inserted {
				t.Fatalf("finish=%v err=%v", inserted, err)
			}
			afterItem, _ := s.QueueItem(ctx, item.ID)
			afterRun, _ := s.Run(ctx, run.ID)
			afterQueue, _ := s.Queue(ctx, q.ID)
			if !reflect.DeepEqual(beforeItem, afterItem) || !reflect.DeepEqual(beforeRun, afterRun) || !reflect.DeepEqual(beforeQueue, afterQueue) {
				t.Fatalf("label %s changed item/run/queue: item %+v → %+v; run %+v → %+v; queue %+v → %+v", label, beforeItem, afterItem, beforeRun, afterRun, beforeQueue, afterQueue)
			}
		})
	}
}

func TestLLMResultsAreDiscardedAfterOwnerAndRunRaces(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	q, err := s.CreateQueue(ctx, p.ID, "llm-races")
	if err != nil {
		t.Fatal(err)
	}
	actions := []struct {
		name  string
		apply func(*testing.T, QueueItem, Run)
	}{
		{name: "achieved", apply: func(t *testing.T, _ QueueItem, run Run) {
			_, err := s.TransitionRun(ctx, run.ID, []string{RunRunning}, RunAchieved, "", nil)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "exit", apply: func(t *testing.T, _ QueueItem, run Run) {
			_, err := s.TransitionRun(ctx, run.ID, []string{RunRunning}, RunExited, "", nil)
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "stale then achieved", apply: func(t *testing.T, item QueueItem, run Run) {
			if _, err := s.TransitionRun(ctx, run.ID, []string{RunRunning}, RunStale, "", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := s.TransitionQueueItem(ctx, item.ID, []string{ItemRunning}, ItemNeedsAttention); err != nil {
				t.Fatal(err)
			}
			if _, err := s.TransitionRun(ctx, run.ID, []string{RunStale}, RunAchieved, "", nil); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "mark done", apply: func(t *testing.T, item QueueItem, run Run) {
			if _, err := s.TransitionRun(ctx, run.ID, []string{RunRunning}, RunStale, "", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := s.TransitionQueueItem(ctx, item.ID, []string{ItemRunning}, ItemNeedsAttention); err != nil {
				t.Fatal(err)
			}
			if _, err := s.TransitionQueueItem(ctx, item.ID, []string{ItemNeedsAttention}, ItemDone); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "skip", apply: func(t *testing.T, item QueueItem, run Run) {
			if _, err := s.TransitionRun(ctx, run.ID, []string{RunRunning}, RunStale, "", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := s.TransitionQueueItem(ctx, item.ID, []string{ItemRunning}, ItemNeedsAttention); err != nil {
				t.Fatal(err)
			}
			if _, err := s.TransitionQueueItem(ctx, item.ID, []string{ItemNeedsAttention}, ItemSkipped); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "retry", apply: func(t *testing.T, item QueueItem, run Run) {
			if _, err := s.TransitionRun(ctx, run.ID, []string{RunRunning}, RunFailed, "retry", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := s.TransitionQueueItem(ctx, item.ID, []string{ItemRunning}, ItemQueued); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateRun(ctx, item.ID, tokenHash("retry"+time.Now().String()), time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "new signal", apply: func(t *testing.T, _ QueueItem, run Run) {
			now := time.Now().UTC()
			if _, err := s.UpdateRun(ctx, run.ID, RunUpdate{LastSignalAt: &now}); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, action := range actions {
		t.Run(action.name, func(t *testing.T) {
			for i := range 50 {
				item, err := s.AddQueueItem(ctx, q.ID, "claude", "", fmt.Sprintf("%s %d", action.name, i))
				if err != nil {
					t.Fatal(err)
				}
				run, err := s.CreateRun(ctx, item.ID, tokenHash(fmt.Sprintf("%s-%d", action.name, i)), time.Now().Add(-time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.TransitionRun(ctx, run.ID, []string{RunStarting}, RunRunning, "", nil); err != nil {
					t.Fatal(err)
				}
				if _, err := s.TransitionQueueItem(ctx, item.ID, []string{ItemQueued}, ItemRunning); err != nil {
					t.Fatal(err)
				}
				if claimed, err := s.ClaimLLM(ctx, run.ID, nil, 2, time.Minute); err != nil || !claimed {
					t.Fatalf("claim=%v err=%v", claimed, err)
				}
				action.apply(t, item, run)
				beforeItem, _ := s.QueueItem(ctx, item.ID)
				beforeRun, _ := s.Run(ctx, run.ID)
				beforeQueue, _ := s.Queue(ctx, q.ID)
				inserted, _, _, err := s.FinishLLM(ctx, run.ID, nil, []byte(`{"label":"completed","reason":"raced result"}`))
				if err != nil || inserted {
					t.Fatalf("race %d finish=%v err=%v", i, inserted, err)
				}
				afterItem, _ := s.QueueItem(ctx, item.ID)
				afterRun, _ := s.Run(ctx, run.ID)
				afterQueue, _ := s.Queue(ctx, q.ID)
				if !reflect.DeepEqual(beforeItem, afterItem) || !reflect.DeepEqual(beforeRun, afterRun) || !reflect.DeepEqual(beforeQueue, afterQueue) {
					t.Fatalf("race %d mutated state: item %+v → %+v; run %+v → %+v; queue %+v → %+v", i, beforeItem, afterItem, beforeRun, afterRun, beforeQueue, afterQueue)
				}
			}
		})
	}
}

// Deleting a queue removes only its own rows, and never while a run is
// active. A project can't be deleted while a queue uses it.
func TestDeleteQueueRemovesOnlyItsRows(t *testing.T) {
	ctx := context.Background()
	s, p := queueFixture(t)
	other, _ := s.CreateProject(ctx, HostMachineID, "/home/dev/other", "other")
	seed := func(project Project, token string) (Queue, Run) {
		q, _ := s.CreateQueue(ctx, project.ID, "q")
		it, _ := s.AddQueueItem(ctx, q.ID, "claude", "", "/goal x")
		r, err := s.CreateRun(ctx, it.ID, tokenHash(token), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.AppendRunEvent(ctx, r.ID, SourceHook, "session_start", nil); err != nil {
			t.Fatal(err)
		}
		return q, r
	}
	a, runA := seed(p, "a")
	b, runB := seed(other, "b")

	if err := s.DeleteProject(ctx, p.ID); !errors.Is(err, ErrProjectHasQueue) {
		t.Fatalf("delete project with a queue: %v", err)
	}
	if err := s.DeleteQueue(ctx, a.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete queue with an active run: %v", err)
	}
	if _, err := s.TransitionRun(ctx, runA.ID, ActiveRunStatuses, RunExited, "", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteQueue(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	count := func(query string, args ...any) int {
		var n int
		if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM queues`); n != 1 {
		t.Fatalf("queues left: %d", n)
	}
	if count(`SELECT count(*) FROM queue_items WHERE queue_id = $1`, b.ID) != 1 || count(`SELECT count(*) FROM runs WHERE id = $1`, runB.ID) != 1 ||
		count(`SELECT count(*) FROM run_events WHERE run_id = $1`, runB.ID) != 1 {
		t.Fatal("the other queue's rows changed")
	}
	if count(`SELECT count(*) FROM runs`) != 1 || count(`SELECT count(*) FROM run_events`) != 1 || count(`SELECT count(*) FROM queue_items`) != 1 {
		t.Fatal("the deleted queue left rows behind")
	}
	if err := s.DeleteQueue(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	if err := s.DeleteProject(ctx, p.ID); err != nil {
		t.Fatalf("delete project after its queue: %v", err)
	}
}
