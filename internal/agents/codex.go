package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"hostbud/internal/sshx"
	"hostbud/internal/store"
)

// CodexMinVersion is the Codex version the V2-M1 spike verified (§12).
const CodexMinVersion = "0.157.1"

// StreamHost runs commands on a machine, also as a two-way stream
// (sshx.Client).
type StreamHost interface {
	Host
	Stream(ctx context.Context, machine string, args ...string) (io.ReadWriteCloser, error)
}

// Codex is the Codex adapter: hooks through -c hooks.<Event>, the plain
// condition as the prompt, the goal set and read through Codex's own app
// server (`codex app-server proxy`, JSON-RPC thread/goal/set and get).
type Codex struct {
	host    StreamHost
	timeout time.Duration
}

// NewCodex returns the Codex adapter; timeout bounds each app-server call.
func NewCodex(host StreamHost, timeout time.Duration) *Codex {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Codex{host: host, timeout: timeout}
}

func (*Codex) Kind() string       { return "codex" }
func (*Codex) MinVersion() string { return CodexMinVersion }

func (c *Codex) CheckVersion(ctx context.Context, machine string) (string, error) {
	return checkVersion(ctx, c.host, machine, "codex", "Codex", CodexMinVersion, "codex update")
}

// codexHooks maps Codex hook events to the run protocol's events.
var codexHooks = [][2]string{{"SessionStart", EventSessionStart}, {"Stop", EventTurnEnd}, {"SessionEnd", EventSessionEnd}}

// CodexHookOverrides are the per-run -c overrides: one hostbud command hook
// per event, as TOML inline tables (they merge with the user's hooks).
// Never notify: that would replace the user's own notifier.
func CodexHookOverrides() []string {
	var out []string
	for _, h := range codexHooks {
		command, _ := json.Marshal(HookCommand(h[1])) // a JSON string is a TOML basic string
		out = append(out, "-c", fmt.Sprintf(`hooks.%s=[{hooks=[{type="command",command=%s}]}]`, h[0], command))
	}
	return out
}

// BuildCommand: codex <flags> -c 'hooks.…' … '<condition>'. The prompt is the
// plain condition: Codex doesn't run a slash command from its prompt (§12
// S2); Arm sets the goal once the thread exists.
func (c *Codex) BuildCommand(item store.QueueItem, _ store.Run) ([]string, error) {
	flags, err := SplitFlags(item.Flags)
	if err != nil {
		return nil, err
	}
	condition, err := Condition(item.Instruction)
	if err != nil {
		return nil, err
	}
	argv := append([]string{"codex"}, flags...)
	argv = append(argv, CodexHookOverrides()...)
	return append(argv, condition), nil
}

// ParseHook reads session_id (the Codex thread id) and transcript_path.
func (c *Codex) ParseHook(_ string, body []byte) (Binding, error) {
	return parseHookBody(body)
}

type codexGoal struct {
	ThreadID  string `json:"threadId"`
	Objective string `json:"objective"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// Arm sets the run's goal on the bound thread (thread/goal/set).
func (c *Codex) Arm(ctx context.Context, machine string, b Binding, _ store.Run, condition string) error {
	var res struct {
		Goal *codexGoal `json:"goal"`
	}
	err := c.rpc(ctx, machine, func(conn *rpcConn) error {
		return conn.call("thread/goal/set", map[string]any{"threadId": b.SessionID, "objective": condition}, &res)
	})
	if err != nil {
		return fmt.Errorf("could not set the Codex goal: %w", err)
	}
	if res.Goal == nil || res.Goal.ThreadID != b.SessionID || !SameCondition(res.Goal.Objective, condition) {
		return errors.New("could not set the Codex goal: the app server didn't confirm it")
	}
	return nil
}

// ReadGoalState reads the bound thread's goal (thread/goal/get, read-only)
// and maps it (v2 §5.3): complete, set during this run, with the queued
// objective ⇒ achieved; blocked ⇒ failed; active, paused, usage_limited,
// budget_limited ⇒ pending; no goal, another objective, a goal from before
// the run or an unknown status ⇒ unknown.
func (c *Codex) ReadGoalState(ctx context.Context, machine string, b Binding, run store.Run, condition string) (GoalState, error) {
	var res struct {
		Goal *codexGoal `json:"goal"`
	}
	err := c.rpc(ctx, machine, func(conn *rpcConn) error {
		return conn.call("thread/goal/get", map[string]any{"threadId": b.SessionID}, &res)
	})
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		return GoalState{Status: Unknown, Reason: "Codex app server: " + rpcErr.Message, Offset: run.TranscriptOffset}, nil
	}
	if err != nil {
		return GoalState{Offset: run.TranscriptOffset}, err
	}
	return codexGoalState(res.Goal, run, condition), nil
}

func codexGoalState(g *codexGoal, run store.Run, condition string) GoalState {
	state := GoalState{Offset: run.TranscriptOffset}
	if g == nil {
		state.Status, state.Reason = Unknown, "the Codex thread has no goal"
		return state
	}
	state.Condition, state.At = g.Objective, time.Unix(g.UpdatedAt, 0).UTC()
	switch {
	case !SameCondition(g.Objective, condition):
		state.Status, state.Reason = Unknown, "the Codex goal's objective isn't the queued condition"
	case g.CreatedAt < run.StartedAt.Unix():
		state.Status, state.Reason = Unknown, "the Codex goal is older than this run"
	case g.Status == "complete":
		state.Status = Achieved
	case g.Status == "blocked":
		state.Status, state.Reason = Failed, "Codex marked the goal blocked"
	case g.Status == "active", g.Status == "paused", g.Status == "usage_limited", g.Status == "budget_limited":
		state.Status = Pending
	default:
		state.Status, state.Reason = Unknown, fmt.Sprintf("unrecognised Codex goal status %q", g.Status)
	}
	return state
}

// rpc runs fn on a JSON-RPC connection to Codex's app server through
// `codex app-server proxy` in the user's login shell, bounded by timeout.
func (c *Codex) rpc(ctx context.Context, machine string, fn func(*rpcConn) error) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	stream, err := c.host.Stream(ctx, machine, LoginShell("codex", "app-server", "proxy")...)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = stream.Close() // unblocks a read at the deadline
		case <-done:
		}
	}()
	conn, err := dialRPC(stream)
	if err == nil {
		err = conn.call("initialize", map[string]any{"clientInfo": map[string]string{"name": "hostbud", "version": "v2"}}, nil)
	}
	if err == nil {
		err = conn.notify("initialized")
	}
	if err == nil {
		err = fn(conn)
	}
	closeErr := stream.Close()
	if err == nil {
		return nil
	}
	switch {
	case sshx.ExitCode(closeErr) == 127:
		return errors.New("codex not found on the host — install Codex first")
	case ctx.Err() != nil:
		return fmt.Errorf("the Codex app server didn't answer within %s — is Codex still running in the session?", c.timeout)
	}
	return err
}
