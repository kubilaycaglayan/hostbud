package queue

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"hostbud/internal/store"
)

type fakeHookStore struct {
	mu     sync.Mutex
	runs   map[string]store.Run
	events []store.RunEvent
}

func (f *fakeHookStore) Run(_ context.Context, id string) (store.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.runs[id]
	if !ok {
		return r, store.ErrNotFound
	}
	return r, nil
}

func (f *fakeHookStore) AppendRunEvent(_ context.Context, runID, source, kind string, payload []byte) (store.RunEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(payload) > store.MaxRunEventPayload {
		return store.RunEvent{}, store.ErrPayloadTooLarge
	}
	e := store.RunEvent{RunID: runID, Source: source, Kind: kind, Payload: append([]byte(nil), payload...)}
	f.events = append(f.events, e)
	return e, nil
}

type recordingNotifier struct {
	mu      sync.Mutex
	signals []Signal
}

func (r *recordingNotifier) Notify(s Signal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signals = append(r.signals, s)
}

const runA, runB = "01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW"

func hookFixture(t *testing.T) (*Hooks, *fakeHookStore, *recordingNotifier, string, string) {
	t.Helper()
	tokenA, hashA, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	tokenB, hashB, _ := NewToken()
	st := &fakeHookStore{runs: map[string]store.Run{
		runA: {ID: runA, TokenHash: hashA, Status: store.RunStarting},
		runB: {ID: runB, TokenHash: hashB, Status: store.RunRunning},
	}}
	n := &recordingNotifier{}
	return NewHooks(st, n, nil), st, n, tokenA, tokenB
}

func status(err error) int {
	var he *HookError
	if errors.As(err, &he) {
		return he.Status
	}
	if err == nil {
		return http.StatusNoContent
	}
	return -1
}

func TestNewTokenIsRandomHashedAndComparedSafely(t *testing.T) {
	a, hashA, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _, _ := NewToken()
	if len(a) != 43 || a == b || strings.ContainsAny(a, "+/=") {
		t.Fatalf("tokens %q %q: want 43 base64url characters, distinct", a, b)
	}
	if len(hashA) != 32 || !bytes.Equal(hashA, HashToken(a)) || bytes.Contains(hashA, []byte(a)) {
		t.Fatalf("hash %x is not the SHA-256 of the token", hashA)
	}
	if !TokenMatches(a, hashA) || TokenMatches(b, hashA) || TokenMatches("", hashA) || TokenMatches(a, hashA[:31]) {
		t.Fatal("TokenMatches accepts the wrong token or a short hash")
	}
}

func TestHookResponsesInOrder(t *testing.T) {
	h, st, n, tokenA, tokenB := hookFixture(t)
	ctx := context.Background()
	bearer := "Bearer " + tokenA
	body := `{"session_id":"s1","transcript_path":"/home/dev/.claude/projects/x/s1.jsonl"}`
	big := `{"a":"` + strings.Repeat("x", MaxHookBody) + `"}`
	cases := []struct {
		name, run, event, auth, body string
		want                         int
	}{
		{"unknown event", runA, "notify", bearer, body, http.StatusNotFound},
		{"unknown event beats a bad token", runA, "notify", "Bearer nope", big, http.StatusNotFound},
		{"unknown run", "01ARZ3NDEKTSV4RRFFQ69G5FAX", "turn_end", bearer, body, http.StatusNotFound},
		{"no token", runA, "turn_end", "", body, http.StatusUnauthorized},
		{"wrong scheme", runA, "turn_end", "Basic " + tokenA, body, http.StatusUnauthorized},
		{"another run's token", runA, "turn_end", "Bearer " + tokenB, body, http.StatusUnauthorized},
		{"bad token beats a big body", runA, "turn_end", "Bearer x", big, http.StatusUnauthorized},
		{"too large", runA, "turn_end", bearer, big, http.StatusRequestEntityTooLarge},
		{"not JSON", runA, "turn_end", bearer, "session_id=s1", http.StatusBadRequest},
		{"empty body", runA, "turn_end", bearer, "", http.StatusBadRequest},
		{"accepted", runA, "session_start", bearer, body, http.StatusNoContent},
	}
	for _, c := range cases {
		if got := status(h.Receive(ctx, c.run, c.event, c.auth, strings.NewReader(c.body))); got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}
	if len(st.events) != 1 || st.events[0].Kind != "session_start" || st.events[0].Source != store.SourceHook || string(st.events[0].Payload) != body {
		t.Fatalf("recorded events: %+v", st.events)
	}
	if len(n.signals) != 1 || n.signals[0].RunID != runA || n.signals[0].Event != "session_start" || string(n.signals[0].Body) != body {
		t.Fatalf("signals: %+v", n.signals)
	}
	// Exactly 64 KiB is still accepted.
	exact := `{"a":"` + strings.Repeat("x", MaxHookBody-8) + `"}`
	if got := status(h.Receive(ctx, runA, "turn_end", bearer, strings.NewReader(exact))); got != http.StatusNoContent {
		t.Fatalf("64 KiB body: %d", got)
	}
}

// Tokens are revoked when a run is finished with, but not when it is stale.
func TestHookTokenRevocationPerRunState(t *testing.T) {
	for status_, want := range map[string]int{
		store.RunStarting: 204, store.RunRunning: 204, store.RunStale: 204,
		store.RunAchieved: 410, store.RunFailed: 410, store.RunExited: 410, store.RunCancelled: 410,
	} {
		h, st, _, tokenA, tokenB := hookFixture(t)
		r := st.runs[runA]
		r.Status = status_
		st.runs[runA] = r
		if got := status(h.Receive(context.Background(), runA, "turn_end", "Bearer "+tokenA, strings.NewReader(`{}`))); got != want {
			t.Errorf("%s run: %d, want %d", status_, got, want)
		}
		// A bad token is still 401 on an ended run.
		if got := status(h.Receive(context.Background(), runA, "turn_end", "Bearer "+tokenB, strings.NewReader(`{}`))); got != http.StatusUnauthorized {
			t.Errorf("%s run with another token: %d", status_, got)
		}
	}
}

func TestHookRateLimitPerRunHasNoSideEffect(t *testing.T) {
	h, st, n, tokenA, tokenB := hookFixture(t)
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return now }
	ctx := context.Background()
	for i := range HookBurst {
		if got := status(h.Receive(ctx, runA, "turn_end", "Bearer "+tokenA, strings.NewReader(`{}`))); got != 204 {
			t.Fatalf("call %d: %d", i, got)
		}
	}
	if got := status(h.Receive(ctx, runA, "turn_end", "Bearer "+tokenA, strings.NewReader(`{}`))); got != http.StatusTooManyRequests {
		t.Fatalf("over the burst: %d", got)
	}
	if len(st.events) != HookBurst || len(n.signals) != HookBurst {
		t.Fatalf("a refused call had side effects: %d events, %d signals", len(st.events), len(n.signals))
	}
	// Other runs have their own bucket; one token refills every second.
	if got := status(h.Receive(ctx, runB, "turn_end", "Bearer "+tokenB, strings.NewReader(`{}`))); got != 204 {
		t.Fatalf("other run: %d", got)
	}
	now = now.Add(time.Second)
	if got := status(h.Receive(ctx, runA, "turn_end", "Bearer "+tokenA, strings.NewReader(`{}`))); got != 204 {
		t.Fatalf("after a second: %d", got)
	}
	if got := status(h.Receive(ctx, runA, "turn_end", "Bearer "+tokenA, strings.NewReader(`{}`))); got != http.StatusTooManyRequests {
		t.Fatalf("second call after a second: %d", got)
	}
}

type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// Tokens and hook bodies never reach the logs; 401 and 429 are logged at
// info with the run id only.
func TestHookLogsNeverHoldTokensOrBodies(t *testing.T) {
	for _, level := range []slog.Level{slog.LevelInfo, slog.LevelDebug} {
		h, _, _, tokenA, tokenB := hookFixture(t)
		var out logBuffer
		h.log = slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: level}))
		secret := `{"session_id":"sess-canary","transcript_path":"/home/dev/.claude/projects/canary/x.jsonl"}`
		_ = h.Receive(context.Background(), runA, "turn_end", "Bearer "+tokenB, strings.NewReader(secret))
		for range HookBurst + 1 {
			_ = h.Receive(context.Background(), runA, "turn_end", "Bearer "+tokenA, strings.NewReader(secret))
		}
		logs := out.buf.String()
		for _, canary := range []string{tokenA, tokenB, "sess-canary", "canary/x.jsonl"} {
			if strings.Contains(logs, canary) {
				t.Errorf("%s logs contain %q:\n%s", level, canary, logs)
			}
		}
		if !strings.Contains(logs, "reason=bad_token") || !strings.Contains(logs, "reason=rate_limited") || !strings.Contains(logs, "run="+runA) {
			t.Errorf("%s logs miss the refusals:\n%s", level, logs)
		}
	}
}
