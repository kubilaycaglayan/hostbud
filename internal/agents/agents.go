// Package agents holds the v2 agent client adapters: how to start a client
// with per-run hooks and a goal, and how to read its structured goal state
// (docs/roadmap-v2/ARCHITECTURE.md §7).
package agents

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"hostbud/internal/sshx"
	"hostbud/internal/store"
)

// Goal states (v2 §5.2).
const (
	Achieved = "achieved"
	Pending  = "pending"
	Failed   = "failed"
	Unknown  = "unknown"
)

// Hook events hostbud's run protocol uses (v2 §8).
const (
	EventSessionStart = "session_start"
	EventTurnEnd      = "turn_end"
	EventSessionEnd   = "session_end"
)

// Binding is what a run's hooks tell about the agent session: its id (the
// Claude session or Codex thread) and transcript. Source and Reason carry
// the client's SessionStart source and SessionEnd reason (a /clear).
type Binding struct {
	SessionID      string
	TranscriptPath string
	Source         string
	Reason         string
}

// GoalState is the client's own verdict on the run's goal. Offset is how far
// the transcript has been read (stored on the run for the next read).
type GoalState struct {
	Status    string
	Condition string
	At        time.Time
	Reason    string
	Offset    int64
}

// Host runs commands on a machine (sshx.Client).
type Host interface {
	Exec(ctx context.Context, machine string, args ...string) ([]byte, error)
	ExecInput(ctx context.Context, machine string, input []byte, args ...string) ([]byte, error)
}

// Files reads files on a machine over SFTP, read-only (fsbrowse.Service).
type Files interface {
	RealPath(ctx context.Context, path string) (string, error)
	ReadRange(ctx context.Context, path string, offset int64, limit int) ([]byte, error)
}

// Adapter is one agent client (v2 §7).
type Adapter interface {
	Kind() string
	MinVersion() string
	// CheckVersion returns the client version on the machine, or an
	// actionable error when it is missing or older than MinVersion.
	CheckVersion(ctx context.Context, machine string) (string, error)
	// BuildCommand returns the argv: client, user flags, hook injection,
	// initial prompt. The token is only referenced as $HOSTBUD_RUN_TOKEN.
	BuildCommand(item store.QueueItem, run store.Run) ([]string, error)
	// ParseHook extracts the binding from a forwarded hook body.
	ParseHook(event string, body []byte) (Binding, error)
	// Arm makes the client track the goal after binding (Codex); a no-op
	// when the goal already came with the command (Claude).
	Arm(ctx context.Context, machine string, b Binding, run store.Run, condition string) error
	// ReadGoalState reads the bound session's goal verdict, read-only.
	ReadGoalState(ctx context.Context, machine string, b Binding, run store.Run, condition string) (GoalState, error)
}

// Registry holds the adapters by kind.
type Registry struct{ adapters map[string]Adapter }

// NewRegistry returns a registry of the given adapters.
func NewRegistry(adapters ...Adapter) *Registry {
	r := &Registry{adapters: map[string]Adapter{}}
	for _, a := range adapters {
		r.adapters[a.Kind()] = a
	}
	return r
}

// Get returns the adapter for kind, or nil.
func (r *Registry) Get(kind string) Adapter { return r.adapters[kind] }

// Kinds lists the registered kinds, sorted.
func (r *Registry) Kinds() []string {
	out := make([]string, 0, len(r.adapters))
	for k := range r.adapters {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// ErrUnknownAgent is returned for an agent kind with no adapter.
var ErrUnknownAgent = errors.New("unknown agent")

// ValidateItem checks an item's agent, flags and instruction (the PoC
// requires "/goal <condition>" on one line).
func (r *Registry) ValidateItem(agent, flags, instruction string) error {
	if r.Get(agent) == nil {
		return fmt.Errorf("%w %q: pick one of %s", ErrUnknownAgent, agent, strings.Join(r.Kinds(), ", "))
	}
	if _, err := SplitFlags(flags); err != nil {
		return err
	}
	_, err := Condition(instruction)
	return err
}

// Condition returns the goal condition of a "/goal <condition>" instruction,
// as typed (it is what the client records). One line only.
func Condition(instruction string) (string, error) {
	rest, ok := strings.CutPrefix(instruction, "/goal ")
	if !ok || strings.TrimSpace(rest) == "" {
		return "", errors.New("the instruction must be /goal followed by the condition, e.g. /goal work on milestone 2 per docs/roadmap/M2-tasks.md")
	}
	if strings.ContainsAny(instruction, "\n\r") {
		return "", errors.New("the instruction must be one line")
	}
	return rest, nil
}

// SameCondition compares conditions after whitespace normalization (trimmed,
// runs of whitespace collapsed).
func SameCondition(a, b string) bool {
	return strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
}

// SplitFlags splits user flags into arguments like a POSIX shell would:
// whitespace separates words; single quotes are literal; double quotes allow
// \" \\ \$ \` escapes; a backslash outside quotes escapes the next
// character. Nothing is expanded. Unbalanced quotes are an error.
func SplitFlags(flags string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(flags); i++ {
		c := flags[i]
		switch {
		case c == ' ' || c == '\t':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		case c == '\'':
			inWord = true
			end := strings.IndexByte(flags[i+1:], '\'')
			if end < 0 {
				return nil, errors.New("flags: unbalanced single quote")
			}
			cur.WriteString(flags[i+1 : i+1+end])
			i += end + 1
		case c == '"':
			inWord = true
			closed := false
			for i++; i < len(flags); i++ {
				d := flags[i]
				if d == '"' {
					closed = true
					break
				}
				if d == '\\' && i+1 < len(flags) && strings.IndexByte("\"\\$`", flags[i+1]) >= 0 {
					i++
					d = flags[i]
				}
				cur.WriteByte(d)
			}
			if !closed {
				return nil, errors.New("flags: unbalanced double quote")
			}
		case c == '\\':
			inWord = true
			if i+1 >= len(flags) {
				return nil, errors.New("flags: trailing backslash")
			}
			i++
			cur.WriteByte(flags[i])
		case c == '\n' || c == '\r' || c == 0:
			return nil, errors.New("flags must be one line")
		default:
			inWord = true
			cur.WriteByte(c)
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out, nil
}

// HookCommand is the command every client runs for a hook event (v2 §7): it
// forwards its stdin to hostbud, always exits 0 and prints nothing. The run
// id, URL and token are only env references.
func HookCommand(event string) string {
	return `curl -fs --max-time 5 -o /dev/null -X POST -H "Authorization: Bearer $HOSTBUD_RUN_TOKEN" -H 'Content-Type: application/json' --data-binary @- "$HOSTBUD_URL/api/hooks/$HOSTBUD_RUN_ID/` + event + `" >/dev/null 2>&1 || true`
}

// LoginShell wraps argv so it runs in the user's login shell, where the
// clients are on the PATH (they aren't on the bare SSH PATH, §12).
func LoginShell(argv ...string) []string {
	return []string{"sh", "-c", `exec "${SHELL:-/bin/sh}" -lic "$1" 2>/dev/null`, "hostbud", sshx.Command(argv...)}
}

var versionRE = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// ParseVersion finds the first x.y.z in text.
func ParseVersion(text string) (string, bool) {
	m := versionRE.FindString(text)
	return m, m != ""
}

// VersionAtLeast compares two x.y.z versions numerically.
func VersionAtLeast(have, want string) bool {
	h, w := versionRE.FindStringSubmatch(have), versionRE.FindStringSubmatch(want)
	if h == nil || w == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		a, _ := strconv.Atoi(h[i])
		b, _ := strconv.Atoi(w[i])
		if a != b {
			return a > b
		}
	}
	return true
}

// checkVersion runs `<client> --version` through the login shell and checks
// it against min; name is the product name for messages.
func checkVersion(ctx context.Context, host Host, machine, client, name, min, update string) (string, error) {
	out, err := host.Exec(ctx, machine, LoginShell(client, "--version")...)
	if sshx.ExitCode(err) == 127 {
		return "", fmt.Errorf("%s not found on the host — install %s first", client, name)
	}
	if err != nil {
		return "", fmt.Errorf("could not check the %s version on the host: %w", name, err)
	}
	version, ok := ParseVersion(string(out))
	if !ok {
		return "", fmt.Errorf("could not read the %s version on the host (`%s --version`)", name, client)
	}
	if !VersionAtLeast(version, min) {
		return version, fmt.Errorf("%s %s or newer is needed on the host; found %s — update with `%s`", name, min, version, update)
	}
	return version, nil
}
