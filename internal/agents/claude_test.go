package agents

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"hostbud/internal/store"
)

const fixtureDir = "testdata/claude/2.1.283/"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(fixtureDir + name) //nolint:gosec // fixed test fixture directory
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const (
	fixSession   = "0b6f1f58-0000-4000-8000-000000000001"
	fixDecoySess = "0b6f1f58-0000-4000-8000-000000000002"
	condDone     = "the file done.txt in the current directory contains the single word ok"
	condFailed   = "the integer 7 is an even number (do not change any files; if this can never be true, say so and stop)"
)

var beforeFixtures = time.Date(2026, 9, 27, 21, 40, 0, 0, time.UTC)

func TestClaudeBuildCommand(t *testing.T) {
	c := NewClaude(nil, nil, nil)
	item := store.QueueItem{Agent: "claude", Flags: `--dangerously-skip-permissions --model 'opus 4'`, Instruction: "/goal ship M2 'fast'"}
	const token = "run-token-canary"
	argv, err := c.BuildCommand(item, store.Run{ID: "01RUN"})
	if err != nil {
		t.Fatal(err)
	}
	if argv[0] != "claude" || argv[1] != "--dangerously-skip-permissions" || argv[2] != "--model" || argv[3] != "opus 4" || argv[4] != "--settings" || argv[6] != "/goal ship M2 'fast'" || len(argv) != 7 {
		t.Fatalf("argv %q", argv)
	}
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct{ Type, Command string }
		}
	}
	if err := json.Unmarshal([]byte(argv[5]), &settings); err != nil {
		t.Fatal(err)
	}
	for event, want := range map[string]string{"SessionStart": EventSessionStart, "Stop": EventTurnEnd, "SessionEnd": EventSessionEnd} {
		h := settings.Hooks[event]
		if len(h) != 1 || len(h[0].Hooks) != 1 || h[0].Hooks[0].Type != "command" || h[0].Hooks[0].Command != HookCommand(want) {
			t.Errorf("%s hook = %+v", event, h)
		}
	}
	if len(settings.Hooks) != 3 || strings.Contains(strings.Join(argv, " "), token) {
		t.Fatalf("settings %s", argv[5])
	}
	for _, bad := range []store.QueueItem{{Flags: `--model 'x`, Instruction: "/goal x"}, {Instruction: ""}, {Instruction: "two\nlines"}} {
		if _, err := c.BuildCommand(bad, store.Run{}); err == nil {
			t.Errorf("BuildCommand(%+v) accepted", bad)
		}
	}
	plain, err := c.BuildCommand(store.QueueItem{Instruction: "ship it"}, store.Run{})
	if err != nil || plain[len(plain)-1] != "ship it" {
		t.Fatalf("plain prompt argv = %q, %v", plain, err)
	}
}

func TestClaudeParseHook(t *testing.T) {
	c := NewClaude(nil, nil, nil)
	b, err := c.ParseHook(EventSessionStart, []byte(`{"session_id":"s1","transcript_path":"/home/dev/.claude/projects/p/s1.jsonl","source":"startup","hook_event_name":"SessionStart"}`))
	if err != nil || b.SessionID != "s1" || b.TranscriptPath != "/home/dev/.claude/projects/p/s1.jsonl" || b.Source != "startup" {
		t.Fatalf("ParseHook = %+v, %v", b, err)
	}
	b, err = c.ParseHook(EventSessionEnd, []byte(`{"session_id":"s1","reason":"clear"}`))
	if err != nil || b.Reason != "clear" {
		t.Fatalf("SessionEnd reason: %+v, %v", b, err)
	}
	for _, bad := range []string{`{}`, `{"session_id":""}`, `{"session_id":5}`, `nope`} {
		if _, err := c.ParseHook(EventTurnEnd, []byte(bad)); err == nil {
			t.Errorf("ParseHook(%s) accepted", bad)
		}
	}
}

// The §5.3 matrix over real 2.1.283 transcripts (V2-M1 T1).
func TestClaudeGoalStateMatrix(t *testing.T) {
	achieved := fixture(t, "achieved.jsonl")
	lines := strings.SplitAfter(string(achieved), "\n")
	var sentinelOnly string
	for _, l := range lines {
		if strings.Contains(l, `"met":true`) {
			break
		}
		sentinelOnly += l
	}
	metLine := ""
	for _, l := range lines {
		if strings.Contains(l, `"met":true`) {
			metLine = l
		}
	}
	for _, c := range []struct {
		name      string
		data      string
		session   string
		started   time.Time
		condition string
		want      string
	}{
		{"achieved", string(achieved), fixSession, beforeFixtures, condDone, Achieved},
		{"condition with other whitespace", string(achieved), fixSession, beforeFixtures, "  the file done.txt in the current  directory contains the single word ok ", Achieved},
		{"pending: only the sentinel", sentinelOnly, fixSession, beforeFixtures, condDone, Pending},
		{"failed: impossible", string(fixture(t, "failed.jsonl")), fixSession, beforeFixtures, condFailed, Failed},
		{"decoy text only nested", string(fixture(t, "decoy.jsonl")), fixDecoySess, beforeFixtures, decoyCondition(t), Pending},
		{"decoy goal achieved for real", string(fixture(t, "decoy-achieved.jsonl")), fixDecoySess, beforeFixtures, decoyCondition(t), Achieved},
		{"decoy text doesn't match another condition", string(fixture(t, "decoy-achieved.jsonl")), fixDecoySess, beforeFixtures, "decoy", Pending},
		{"stale record from before start", string(achieved), fixSession, time.Date(2026, 9, 27, 22, 0, 0, 0, time.UTC), condDone, Pending},
		{"wrong session", string(achieved), fixDecoySess, beforeFixtures, condDone, Pending},
		{"changed condition", string(achieved), fixSession, beforeFixtures, "the file done.txt contains ok", Pending},
		{"truncated last line", sentinelOnly + strings.TrimSuffix(metLine, "\n")[:len(metLine)/2], fixSession, beforeFixtures, condDone, Pending},
		{"empty", "", fixSession, beforeFixtures, condDone, Pending},
		{"goal record without met", `{"type":"attachment","timestamp":"2026-09-27T21:43:39.780Z","sessionId":"` + fixSession + `","attachment":{"type":"goal_status","condition":"` + condDone + `"}}` + "\n", fixSession, beforeFixtures, condDone, Unknown},
		{"line that isn't JSON", "garbage\n", fixSession, beforeFixtures, condDone, Unknown},
	} {
		got := scanClaudeTranscript([]byte(c.data), 0, c.session, c.started, c.condition)
		if got.Status != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got.Status, got.Reason, c.want)
		}
	}
}

func decoyCondition(t *testing.T) string {
	t.Helper()
	for _, l := range strings.Split(string(fixture(t, "decoy.jsonl")), "\n") {
		var rec struct {
			Type       string
			Attachment struct {
				Type, Condition string
			}
		}
		if json.Unmarshal([]byte(l), &rec) == nil && rec.Type == "attachment" && rec.Attachment.Type == "goal_status" {
			return rec.Attachment.Condition
		}
	}
	t.Fatal("decoy fixture has no goal record")
	return ""
}

// fakeFiles serves one transcript and resolves paths through links.
type fakeFiles struct {
	data  map[string][]byte
	links map[string]string
	reads int
	err   error
}

func (f *fakeFiles) RealPath(_ context.Context, p string) (string, error) {
	for from, to := range f.links {
		if p == from || strings.HasPrefix(p, from+"/") {
			return to + strings.TrimPrefix(p, from), nil
		}
	}
	return p, nil
}

func (f *fakeFiles) ReadRange(_ context.Context, p string, offset int64, limit int) ([]byte, error) {
	f.reads++
	if f.err != nil {
		return nil, f.err
	}
	d, ok := f.data[p]
	if !ok {
		return nil, errors.New("no such file")
	}
	if offset > int64(len(d)) {
		return nil, nil
	}
	end := min(int64(len(d)), offset+int64(limit))
	return d[offset:end], nil
}

func claudeWith(files *fakeFiles) *Claude {
	return NewClaude(nil, func(string) Files { return files }, func(string) string { return "/home/dev" })
}

const transcript = "/home/dev/.claude/projects/-home-dev-app/" + fixSession + ".jsonl"

// Reads are incremental: an appended line is seen exactly once, and the
// offset never passes a truncated last line.
func TestClaudeReadGoalStateIsIncremental(t *testing.T) {
	achieved := fixture(t, "achieved.jsonl")
	cut := strings.LastIndex(string(achieved[:len(achieved)-1]), "\n") // before the last line
	metAt := strings.Index(string(achieved), `"met":true`)
	metStart := strings.LastIndex(string(achieved[:metAt]), "\n") + 1
	files := &fakeFiles{data: map[string][]byte{transcript: achieved[:metStart+20]}}
	c := claudeWith(files)
	run := store.Run{StartedAt: beforeFixtures}
	b := Binding{SessionID: fixSession, TranscriptPath: transcript}
	ctx := context.Background()

	s1, err := c.ReadGoalState(ctx, "host", b, run, condDone)
	if err != nil || s1.Status != Pending || s1.Offset != int64(metStart) {
		t.Fatalf("first read: %+v, %v (want pending at %d)", s1, err, metStart)
	}
	files.data[transcript] = achieved
	run.TranscriptOffset = s1.Offset
	s2, err := c.ReadGoalState(ctx, "host", b, run, condDone)
	if err != nil || s2.Status != Achieved || s2.Offset <= s1.Offset || s2.At.IsZero() {
		t.Fatalf("second read: %+v, %v", s2, err)
	}
	// Reading on from after the achieved record sees it no more.
	run.TranscriptOffset = int64(cut + 1)
	s3, _ := c.ReadGoalState(ctx, "host", b, run, condDone)
	if s3.Status != Pending || s3.Offset != int64(len(achieved)) {
		t.Fatalf("third read: %+v", s3)
	}
	// An unknown home (hostbud just restarted, host not probed yet) is an
	// error (retried), not an "outside /.claude/projects" verdict.
	noHome := NewClaude(nil, func(string) Files { return files }, func(string) string { return "" })
	if s, err := noHome.ReadGoalState(ctx, "host", b, run, condDone); err == nil || s.Status == Unknown {
		t.Fatalf("unknown home: %+v, %v (want a retryable error)", s, err)
	}
	// An SFTP failure is an error (retried on the next signal), not a verdict.
	files.err = errors.New("sftp: timeout")
	if _, err := c.ReadGoalState(ctx, "host", b, run, condDone); err == nil {
		t.Fatal("read error swallowed")
	}
}

func TestClaudeTranscriptPathValidation(t *testing.T) {
	ctx := context.Background()
	files := &fakeFiles{data: map[string][]byte{}, links: map[string]string{
		"/home/dev/.claude/projects/evil": "/etc",
	}}
	for _, c := range []struct {
		path string
		ok   bool
	}{
		{"/home/dev/.claude/projects/-home-dev-app/s.jsonl", true},
		{"", false},
		{"relative/s.jsonl", false},
		{"/home/dev/.claude/projects/../settings.json", false},
		{"/home/dev/.claude/projects//x/s.jsonl", false},
		{"/home/dev/.claude/projectsX/s.jsonl", false},
		{"/home/other/.claude/projects/x/s.jsonl", false},
		{"/home/dev/.codex/sessions/s.jsonl", false},
		{"/home/dev/.claude/projects/evil/passwd", false},
	} {
		err := ValidTranscriptPath(ctx, files, c.path, "/home/dev/.claude/projects")
		if (err == nil) != c.ok {
			t.Errorf("%q: %v, want ok=%v", c.path, err, c.ok)
		}
	}
	// A bad path is an unknown state, not a read.
	c := claudeWith(files)
	got, err := c.ReadGoalState(ctx, "host", Binding{SessionID: "s", TranscriptPath: "/etc/passwd"}, store.Run{}, condDone)
	if err != nil || got.Status != Unknown || files.reads != 0 {
		t.Fatalf("bad path: %+v, %v, %d reads", got, err, files.reads)
	}
}
