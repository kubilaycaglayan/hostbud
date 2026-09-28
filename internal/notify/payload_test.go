package notify

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestBuildTheThreeEvents(t *testing.T) {
	base := Event{RunID: "01RUN", QueueID: "queue_a", ItemID: "item_b", Project: "app", Position: 2}
	cases := []struct {
		kind, outcome          string
		key, title, body, want string
	}{
		{KindDone, "", "run:01RUN:done", "app: item 2 done", "Item 2 reached its goal.", "done"},
		{KindAttention, "failed", "run:01RUN:attention", "app: item 2 needs attention", "Item 2 failed. The queue is paused.", "failed"},
		{KindAttention, "stale", "run:01RUN:attention", "app: item 2 needs attention", "Item 2 has gone quiet. The queue is paused.", "stale"},
		{KindAttention, "weird", "run:01RUN:attention", "app: item 2 needs attention", "Item 2 needs your attention. The queue is paused.", "needs_attention"},
		{KindFinished, "", "queue:queue_a:finished:01RUN", "app: queue finished", "Every item has run (last: item 2).", "finished"},
	}
	for _, c := range cases {
		e := base
		e.Kind, e.Outcome = c.kind, c.outcome
		p, err := Build(e)
		if err != nil {
			t.Fatal(err)
		}
		if p.Key != c.key || p.Title != c.title || p.Body != c.body || p.Outcome != c.want || p.V != 1 || p.URL != "/queues/queue_a?item=item_b" {
			t.Errorf("%s/%s: %+v", c.kind, c.outcome, p)
		}
	}
	if _, err := Build(Event{Kind: "started"}); err == nil {
		t.Fatal("unknown kind accepted")
	}
}

func TestLLMFlagNoticeUsesAllowlistAndStableKey(t *testing.T) {
	e := Event{Kind: KindAttention, RunID: "run-a", QueueID: "q", ItemID: "i", Project: "app", Position: 3, Outcome: "completed", Key: "run:run-a:llm:completed"}
	p, err := Build(e)
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != e.Key || p.Outcome != "completed" || !strings.Contains(p.Body, "queue has not advanced") || strings.Contains(p.Body, "reason") {
		t.Fatalf("unexpected payload: %+v", p)
	}
	b, err := p.JSON()
	if err != nil || strings.Contains(string(b), "pane") || strings.Contains(string(b), "instruction") {
		t.Fatalf("payload=%s err=%v", b, err)
	}
}

// The payload carries exactly the allowlisted fields, each ≤ 1 KiB in all,
// and control characters in a project name can't break the text.
func TestPayloadAllowlistAndCap(t *testing.T) {
	p, err := Build(Event{Kind: KindDone, RunID: "r", QueueID: "q", ItemID: "i", Project: strings.Repeat("ü", 5000) + "\n\x1b[31m", Position: 1})
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.JSON()
	if err != nil || len(b) > MaxPayload {
		t.Fatalf("JSON = %d bytes, %v", len(b), err)
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if want := []string{"body", "key", "kind", "outcome", "position", "project", "title", "url", "v"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("payload fields = %v, want %v", keys, want)
	}
	if strings.ContainsAny(p.Title+p.Project, "\n\x1b") {
		t.Fatalf("control characters kept: %q", p.Title)
	}
	if p2, _ := Build(Event{Kind: KindDone, Project: "  "}); p2.Project != "hostbud" {
		t.Fatalf("empty project = %q", p2.Project)
	}
}

func TestBuildTestNotification(t *testing.T) {
	p, err := Build(Event{Kind: KindTest, RunID: "abc"})
	if err != nil || p.Key != "test:abc" || p.URL != "/" || p.Title != "hostbud: test notification" || p.Outcome != "test" {
		t.Fatalf("test payload %+v, %v", p, err)
	}
}

// V2-M4: gate notices are needs-attention notices with their own keys.
func TestBuildGateNotices(t *testing.T) {
	approval, err := Build(Event{Kind: KindAttention, RunID: "R1", QueueID: "q", ItemID: "i", Project: "app", Position: 2, Outcome: OutcomeAwaitingApproval})
	if err != nil || approval.Key != "run:R1:approval" || approval.Outcome != OutcomeAwaitingApproval ||
		approval.Body != "Item 2 is waiting for your approval. The queue waits." {
		t.Fatalf("approval: %+v, %v", approval, err)
	}
	verify, err := Build(Event{Kind: KindAttention, RunID: "R1", QueueID: "q", ItemID: "i", Project: "app", Position: 2, Outcome: OutcomeVerifyFailed, Attempt: 3})
	if err != nil || verify.Key != "run:R1:verify:3" || verify.Outcome != OutcomeVerifyFailed ||
		verify.Body != "Item 2 failed its verify command. The queue is paused." {
		t.Fatalf("verify: %+v, %v", verify, err)
	}
	plain, _ := Build(Event{Kind: KindAttention, RunID: "R1", Outcome: "failed"})
	if plain.Key != "run:R1:attention" {
		t.Fatalf("run attention key changed: %q", plain.Key)
	}
}
