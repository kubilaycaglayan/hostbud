package queue

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"hostbud/internal/agents"
	"hostbud/internal/sshx"
)

// V2-M4 verify runner: an item's verify command runs on the host, in the
// project directory, as its own sshx call from argv — never through tmux or
// the run's session.

// VerifyTail caps the output kept from a verify attempt (its tail).
const VerifyTail = 16 << 10

// verifyKillAfter is the remote `timeout -k` grace before SIGKILL; the local
// deadline allows it plus a margin, so the remote side normally ends first.
const (
	verifyKillAfter = 10 * time.Second
	verifyMargin    = 5 * time.Second
	preflightLimit  = 30 * time.Second
)

// Verify outcomes (the verify_result event's outcome).
const (
	VerifyPassed      = "passed"
	VerifyFailed      = "failed"
	VerifyTimedOut    = "timeout"
	VerifyMissingDir  = "missing_directory"
	VerifyNoTimeout   = "timeout_missing"
	VerifySSHFailed   = "ssh_failed"
	VerifyInterrupted = "interrupted"
)

// Remote scripts. The first checks the directory and coreutils' timeout
// (its own exit codes); the second runs the command's argv, each word
// passed as an argument, never parsed by a shell.
const (
	preflightScript = `test -d "$1" || exit 3; command -v timeout >/dev/null 2>&1 || exit 4`
	verifyScript    = `exec 2>&1; cd -- "$1" || exit; k=$2 t=$3; shift 3; exec timeout -k "$k" "$t" "$@"`
)

// VerifyExec runs a remote command and streams its stdout (sshx.Client).
type VerifyExec interface {
	ExecTo(ctx context.Context, machine string, w io.Writer, args ...string) error
}

// VerifyResult is one attempt's outcome (the verify_result payload).
type VerifyResult struct {
	Attempt    int    `json:"attempt"`
	Outcome    string `json:"outcome"`
	ExitCode   *int   `json:"exitCode,omitempty"` // nil: the command didn't run to an exit
	DurationMs int64  `json:"durationMs"`
	Truncated  bool   `json:"truncated"`
	Output     string `json:"output"`
	Detail     string `json:"detail,omitempty"`
}

// Passed reports whether the gate is met.
func (r VerifyResult) Passed() bool { return r.Outcome == VerifyPassed }

// Verifier runs verify commands.
type Verifier struct {
	ssh     VerifyExec
	timeout time.Duration
	grace   time.Duration // local deadline beyond timeout (kill-after + margin)
	now     func() time.Time
}

// NewVerifier returns a Verifier bounded by timeout (HOSTBUD_VERIFY_TIMEOUT).
func NewVerifier(ssh VerifyExec, timeout time.Duration) *Verifier {
	return &Verifier{ssh: ssh, timeout: timeout, grace: verifyKillAfter + verifyMargin, now: time.Now}
}

// Timeout is the verify timeout.
func (v *Verifier) Timeout() time.Duration { return v.timeout }

// VerifyArgv splits a verify command into argv like flags (quotes, no
// expansion). Shell operators stay literal words.
func VerifyArgv(command string) ([]string, error) {
	argv, err := agents.SplitFlags(command)
	if err == nil && len(argv) == 0 {
		err = errors.New("the verify command is empty")
	}
	return argv, err
}

// Run runs command in dir on machine. The result carries the outcome, exit
// code, duration and the sanitized output tail; it never fails otherwise.
// A cancelled ctx (shutdown) returns ok=false: the attempt stays open.
func (v *Verifier) Run(ctx context.Context, machine, dir, command string) (res VerifyResult, ok bool) {
	start := v.now()
	defer func() { res.DurationMs = v.now().Sub(start).Milliseconds() }()
	argv, err := VerifyArgv(command)
	if err != nil {
		return VerifyResult{Outcome: VerifyFailed, Detail: "verify didn't run: " + err.Error() + " — edit the verify command"}, true
	}

	pctx, cancel := context.WithTimeout(ctx, preflightLimit)
	err = v.ssh.ExecTo(pctx, machine, io.Discard, "sh", "-c", preflightScript, "sh", dir)
	cancel()
	switch code := sshx.ExitCode(err); {
	case ctx.Err() != nil:
		return VerifyResult{}, false
	case err == nil:
	case code == 3:
		return VerifyResult{Outcome: VerifyMissingDir, Detail: "project directory is missing on the host — restore it, then Re-run verify"}, true
	case code == 4:
		return VerifyResult{Outcome: VerifyNoTimeout, Detail: "`timeout` not found on the host — install coreutils, then Re-run verify"}, true
	default:
		return VerifyResult{Outcome: VerifySSHFailed, Detail: sshDetail(err)}, true
	}

	secs := int((v.timeout + time.Second - 1) / time.Second)
	out := NewTail(VerifyTail)
	rctx, cancel := context.WithTimeout(ctx, v.timeout+v.grace)
	defer cancel()
	args := append([]string{"sh", "-c", verifyScript, "sh", dir, fmt.Sprintf("%ds", int(verifyKillAfter/time.Second)), fmt.Sprintf("%ds", secs)}, argv...)
	err = v.ssh.ExecTo(rctx, machine, out, args...)
	res = VerifyResult{Output: out.String(), Truncated: out.Truncated()}
	code := sshx.ExitCode(err)
	switch {
	case ctx.Err() != nil:
		return VerifyResult{}, false
	case err == nil:
		zero := 0
		res.Outcome, res.ExitCode = VerifyPassed, &zero
	case errors.Is(err, context.DeadlineExceeded), code == 124, code == 137:
		res.Outcome, res.Detail = VerifyTimedOut, "verify timed out after "+formatTimeout(v.timeout)
		if code >= 0 {
			res.ExitCode = &code
		}
	case code >= 0:
		res.Outcome, res.ExitCode = VerifyFailed, &code
		res.Detail = fmt.Sprintf("verify failed (exit %d)", code)
		if code == 127 || code == 126 {
			res.Detail += " — the command wasn't found or can't run on the host"
		}
	default:
		res.Outcome, res.Detail = VerifySSHFailed, sshDetail(err)
	}
	return res, true
}

func sshDetail(err error) string {
	var se *sshx.Error
	if errors.As(err, &se) && se.Message != "" {
		msg := se.Message
		if se.Hint != "" {
			msg += " — " + se.Hint
		}
		return "verify didn't run: " + msg
	}
	return "verify didn't run: " + err.Error()
}

// formatTimeout prints a duration the way it is configured (10m, 1h30m, 5s).
func formatTimeout(d time.Duration) string {
	s := d.String()
	s = strings.TrimSuffix(s, "0s")
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	if s == "" {
		return d.String()
	}
	return s
}

// Tail is an io.Writer that keeps only the last n bytes written (a ring
// buffer: the whole output is never held).
type Tail struct {
	buf   []byte
	pos   int
	full  bool
	total int64
}

// NewTail returns a Tail of n bytes.
func NewTail(n int) *Tail { return &Tail{buf: make([]byte, n)} }

func (t *Tail) Write(p []byte) (int, error) {
	n := len(p)
	t.total += int64(n)
	if len(p) >= len(t.buf) {
		copy(t.buf, p[len(p)-len(t.buf):])
		t.pos, t.full = 0, true
		return n, nil
	}
	for len(p) > 0 {
		c := copy(t.buf[t.pos:], p)
		p = p[c:]
		t.pos += c
		if t.pos == len(t.buf) {
			t.pos, t.full = 0, true
		}
	}
	return n, nil
}

// Truncated reports whether more than the tail was written.
func (t *Tail) Truncated() bool { return t.total > int64(len(t.buf)) }

// Bytes returns the kept tail, oldest byte first.
func (t *Tail) Bytes() []byte {
	if !t.full {
		return append([]byte(nil), t.buf[:t.pos]...)
	}
	return append(append([]byte(nil), t.buf[t.pos:]...), t.buf[:t.pos]...)
}

// String returns the kept tail sanitized for display (SanitizeOutput),
// still at most the tail's size (replacement characters can grow it).
func (t *Tail) String() string {
	s := SanitizeOutput(t.Bytes())
	if over := len(s) - len(t.buf); over > 0 {
		cut := over
		for cut < len(s) && !utf8.RuneStart(s[cut]) {
			cut++
		}
		s = s[cut:]
	}
	return s
}

var ansiEscape = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)?|[ -/]*[0-~])`)

// SanitizeOutput makes command output safe to store and show as text:
// invalid UTF-8 is replaced, ANSI escape sequences are removed, and every
// other control character except newline and tab is dropped.
func SanitizeOutput(b []byte) string {
	s := strings.ToValidUTF8(string(b), "�")
	s = ansiEscape.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		}
		return r
	}, s)
}
