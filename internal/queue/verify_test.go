package queue

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"hostbud/internal/agents"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
)

// fakeExec scripts the two sshx calls of a verify attempt.
type fakeExec struct {
	calls     [][]string
	preflight error
	output    []byte
	run       error
	block     bool // the main call waits for ctx
}

func (f *fakeExec) ExecTo(ctx context.Context, _ string, w io.Writer, args ...string) error {
	f.calls = append(f.calls, args)
	if len(f.calls) == 1 {
		return f.preflight
	}
	_, _ = w.Write(f.output)
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.run
}

func remote(code int) error { return &sshx.Error{Kind: sshx.KindRemote, ExitCode: code} }

func TestVerifyArgvKeepsShellSyntaxLiteral(t *testing.T) {
	for cmd, want := range map[string][]string{
		`make test`:                   {"make", "test"},
		`touch 'x; touch pwned'`:      {"touch", "x; touch pwned"},
		`echo a && rm -rf / | cat`:    {"echo", "a", "&&", "rm", "-rf", "/", "|", "cat"},
		"echo $(id) `id` $HOME":       {"echo", "$(id)", "`id`", "$HOME"},
		`sh -c 'make test && make x'`: {"sh", "-c", "make test && make x"},
		`printf "%s\n" "é ü 日本"`:      {"printf", `%s\n`, "é ü 日本"},
		`-v --flag=1`:                 {"-v", "--flag=1"},
	} {
		got, err := VerifyArgv(cmd)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("VerifyArgv(%q) = %q, %v; want %q", cmd, got, err, want)
		}
	}
	for _, bad := range []string{`make 'x`, `"x`, `x\`, ""} {
		if _, err := VerifyArgv(bad); err == nil {
			t.Errorf("VerifyArgv(%q) accepted", bad)
		}
	}
	// Every word reaches the host quoted: the remote shell sees one literal
	// argument per word.
	for _, w := range []string{"x; touch pwned", "$(id)", "`id`", "a b", "-rf"} {
		if q := sshx.Quote(w); !strings.HasPrefix(q, "'") {
			t.Errorf("Quote(%q) = %s", w, q)
		}
	}
}

func TestVerifierRunsInTheProjectDirectoryFromArgv(t *testing.T) {
	f := &fakeExec{output: []byte("ok\n")}
	v := NewVerifier(f, 10*time.Minute)
	res, ok := v.Run(context.Background(), "host", "/home/dev/app", `touch 'x; touch pwned'`)
	if !ok || !res.Passed() || *res.ExitCode != 0 || res.Output != "ok\n" {
		t.Fatalf("result %+v, %v", res, ok)
	}
	if len(f.calls) != 2 {
		t.Fatalf("calls %q", f.calls)
	}
	pre := f.calls[0]
	if !slices.Equal(pre, []string{"sh", "-c", preflightScript, "sh", "/home/dev/app"}) {
		t.Fatalf("preflight %q", pre)
	}
	main := f.calls[1]
	want := []string{"sh", "-c", verifyScript, "sh", "/home/dev/app", "10s", "600s", "touch", "x; touch pwned"}
	if !slices.Equal(main, want) {
		t.Fatalf("main call %q, want %q", main, want)
	}
	for _, call := range f.calls {
		if slices.ContainsFunc(call, func(a string) bool { return strings.Contains(a, "tmux") || strings.Contains(a, "send-keys") }) {
			t.Fatalf("verify went through tmux: %q", call)
		}
	}
}

func TestVerifierOutcomes(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		f       *fakeExec
		timeout time.Duration
		outcome string
		exit    int // -1: none
		detail  string
	}{
		{"exit 0", &fakeExec{}, time.Minute, VerifyPassed, 0, ""},
		{"non-zero", &fakeExec{run: remote(2)}, time.Minute, VerifyFailed, 2, "verify failed (exit 2)"},
		{"not found", &fakeExec{run: remote(127)}, time.Minute, VerifyFailed, 127, "verify failed (exit 127) — the command wasn't found or can't run on the host"},
		{"remote timeout", &fakeExec{run: remote(124)}, 10 * time.Minute, VerifyTimedOut, 124, "verify timed out after 10m"},
		{"remote kill", &fakeExec{run: remote(137)}, 90 * time.Minute, VerifyTimedOut, 137, "verify timed out after 1h30m"},
		{"missing dir", &fakeExec{preflight: remote(3)}, time.Minute, VerifyMissingDir, -1, "project directory is missing on the host — restore it, then Re-run verify"},
		{"no timeout", &fakeExec{preflight: remote(4)}, time.Minute, VerifyNoTimeout, -1, "`timeout` not found on the host — install coreutils, then Re-run verify"},
		{"ssh down", &fakeExec{preflight: &sshx.Error{Kind: sshx.KindUnreachable, ExitCode: 255, Message: "can't reach sshd on the host", Hint: "Check sshd."}}, time.Minute, VerifySSHFailed, -1, "verify didn't run: can't reach sshd on the host — Check sshd."},
		{"ssh drop", &fakeExec{run: &sshx.Error{Kind: sshx.KindSSH, ExitCode: 255, Message: "ssh to the host failed"}}, time.Minute, VerifySSHFailed, -1, "verify didn't run: ssh to the host failed"},
	}
	for _, c := range cases {
		res, ok := NewVerifier(c.f, c.timeout).Run(ctx, "host", "/home/dev/app", "make test")
		exit := -1
		if res.ExitCode != nil {
			exit = *res.ExitCode
		}
		if !ok || res.Outcome != c.outcome || exit != c.exit || res.Detail != c.detail {
			t.Errorf("%s: %+v (exit %d), ok %v", c.name, res, exit, ok)
		}
	}
}

func TestVerifierLocalDeadlineIsATimeout(t *testing.T) {
	f := &fakeExec{block: true, output: []byte("partial")}
	v := NewVerifier(f, 5*time.Second)
	v.timeout, v.grace = 50*time.Millisecond, 50*time.Millisecond // fast; the detail names the configured value
	start := time.Now()
	res, ok := v.Run(context.Background(), "host", "/d", "sleep 999")
	if !ok || res.Outcome != VerifyTimedOut || res.Detail != "verify timed out after 50ms" || res.Output != "partial" {
		t.Fatalf("result %+v", res)
	}
	if took := time.Since(start); took < v.timeout+v.grace || took > 2*time.Second {
		t.Fatalf("local deadline after %s", took)
	}
}

func TestVerifierShutdownLeavesTheAttemptOpen(t *testing.T) {
	f := &fakeExec{block: true}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if _, ok := NewVerifier(f, time.Minute).Run(ctx, "host", "/d", "sleep 9"); ok {
		t.Fatal("a cancelled verify reported a result")
	}
}

func TestTailKeepsTheLastBytes(t *testing.T) {
	tail := NewTail(8)
	_, _ = tail.Write([]byte("abc"))
	if string(tail.Bytes()) != "abc" || tail.Truncated() {
		t.Fatalf("short: %q", tail.Bytes())
	}
	_, _ = tail.Write([]byte("defgh"))
	if string(tail.Bytes()) != "abcdefgh" || tail.Truncated() {
		t.Fatalf("exact: %q", tail.Bytes())
	}
	_, _ = tail.Write([]byte("ij"))
	if string(tail.Bytes()) != "cdefghij" || !tail.Truncated() {
		t.Fatalf("wrapped: %q", tail.Bytes())
	}
	_, _ = tail.Write([]byte("0123456789XYZ"))
	if string(tail.Bytes()) != "6789XYZ"[0:0]+"56789XYZ" {
		t.Fatalf("big write: %q", tail.Bytes())
	}

	// 50 MiB of random binary in odd-sized chunks: at most 16 KiB kept, the
	// right bytes, valid text after sanitizing.
	big := NewTail(VerifyTail)
	var last []byte
	chunk := make([]byte, 1<<20+7)
	for written := 0; written < 50<<20; written += len(chunk) {
		_, _ = rand.Read(chunk)
		_, _ = big.Write(chunk)
		last = chunk
	}
	if got := big.Bytes(); len(got) != VerifyTail || !bytes.Equal(got, last[len(last)-VerifyTail:]) || !big.Truncated() {
		t.Fatalf("50 MiB tail: %d bytes", len(got))
	}
	s := big.String()
	if !utf8.ValidString(s) || len(s) > VerifyTail {
		t.Fatalf("sanitized tail: valid %v, %d bytes", utf8.ValidString(s), len(s))
	}
	for _, r := range s {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			t.Fatalf("control rune %U survived", r)
		}
	}
}

func TestSanitizeOutput(t *testing.T) {
	in := "\x1b[31mFAIL\x1b[0m: x\r\n\ttab\x1b]0;title\x07 bell\x07 nul\x00 \xff\xfe é \x9b end\x1bc"
	want := "FAIL: x\n\ttab bell nul \uFFFD é \uFFFD end"
	if got := SanitizeOutput([]byte(in)); got != want {
		t.Fatalf("SanitizeOutput = %q, want %q", got, want)
	}
}

func TestVerifyPayloadFitsTheEventCap(t *testing.T) {
	res := VerifyResult{Attempt: 1, Outcome: VerifyFailed, Output: strings.Repeat("<\"\\ ", VerifyTail/7)}
	if b := verifyPayload(res); len(b) > store.MaxRunEventPayload {
		t.Fatalf("payload %d bytes", len(b))
	}
}

// Info logs carry the item, attempt, exit code and duration only; never the
// command or its output.
func TestVerifyLogHygiene(t *testing.T) {
	var logs bytes.Buffer
	e := newDispEnv(t)
	e.d.log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	v := newFakeVerifier()
	code := 1
	v.script(VerifyResult{Outcome: VerifyFailed, ExitCode: &code, Detail: "verify failed (exit 1)", Output: "OUTPUT-SECRET-42"})
	e.d.SetVerifier(v)
	if _, err := e.svc.AddItem(e.ctx(), e.queue.ID, "claude", "", "/goal m1", store.ItemGates{VerifyCommand: "check-COMMAND-SECRET-7"}); err != nil {
		t.Fatal(err)
	}
	e.startQueue()
	r := e.run(1)
	e.hook(r, EventSessionStart, "s")
	e.claude.set("s", agents.Achieved)
	e.hook(r, EventTurnEnd, "s")
	e.until("needs attention", func() bool { return e.itemStatus(1) == store.ItemNeedsAttention })
	out := logs.String()
	if strings.Contains(out, "COMMAND-SECRET") || strings.Contains(out, "OUTPUT-SECRET") {
		t.Fatalf("info logs leak the command or output:\n%s", out)
	}
	if !strings.Contains(out, "verify finished") || !strings.Contains(out, "attempt=1") || !strings.Contains(out, "exit=1") {
		t.Fatalf("info logs miss the verify summary:\n%s", out)
	}
}
