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
