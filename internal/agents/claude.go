package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"hostbud/internal/store"
)

// ClaudeMinVersion is the Claude Code version the V2-M1 spike verified (§12).
const ClaudeMinVersion = "2.1.283"

// maxTranscriptRead bounds one incremental transcript read.
const maxTranscriptRead = 4 << 20

// Claude is the Claude Code adapter: hooks through --settings, the goal as
// the initial "/goal …" prompt, goal verdicts from the session transcript.
type Claude struct {
	host  Host
	files func(machine string) Files
	home  func(machine string) string
}

// NewClaude returns the Claude Code adapter. files returns the SFTP reader
// for a machine, home the remote user's home directory.
func NewClaude(host Host, files func(string) Files, home func(string) string) *Claude {
	return &Claude{host: host, files: files, home: home}
}

func (*Claude) Kind() string       { return "claude" }
func (*Claude) MinVersion() string { return ClaudeMinVersion }

func (c *Claude) CheckVersion(ctx context.Context, machine string) (string, error) {
	return checkVersion(ctx, c.host, machine, "claude", "Claude Code", ClaudeMinVersion, "claude update")
}

// claudeHooks maps Claude hook events to the run protocol's events.
var claudeHooks = [][2]string{{"SessionStart", EventSessionStart}, {"Stop", EventTurnEnd}, {"SessionEnd", EventSessionEnd}}

// ClaudeSettings is the per-run --settings JSON: one hostbud hook per event.
func ClaudeSettings() string {
	type hook struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type matcher struct {
		Hooks []hook `json:"hooks"`
	}
	hooks := map[string][]matcher{}
	for _, h := range claudeHooks {
		hooks[h[0]] = []matcher{{Hooks: []hook{{Type: "command", Command: HookCommand(h[1])}}}}
	}
	b, _ := json.Marshal(map[string]any{"hooks": hooks})
	return string(b)
}

// BuildCommand: claude <flags> --settings '<hooks>' '/goal <condition>'.
func (c *Claude) BuildCommand(item store.QueueItem, _ store.Run) ([]string, error) {
	flags, err := SplitFlags(item.Flags)
	if err != nil {
		return nil, err
	}
	if _, err := Condition(item.Instruction); err != nil {
		return nil, err
	}
	argv := append([]string{"claude"}, flags...)
	return append(argv, "--settings", ClaudeSettings(), item.Instruction), nil
}

type hookBody struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Source         string `json:"source"`
	Reason         string `json:"reason"`
}

// ParseHook reads session_id (required) and transcript_path.
func (c *Claude) ParseHook(_ string, body []byte) (Binding, error) {
	return parseHookBody(body)
}

func parseHookBody(body []byte) (Binding, error) {
	var h hookBody
	if err := json.Unmarshal(body, &h); err != nil {
		return Binding{}, fmt.Errorf("hook body: %w", err)
	}
	if strings.TrimSpace(h.SessionID) == "" {
		return Binding{}, errors.New("hook body has no session_id")
	}
	return Binding{SessionID: h.SessionID, TranscriptPath: h.TranscriptPath, Source: h.Source, Reason: h.Reason}, nil
}

// Arm is a no-op: Claude runs /goal from its initial prompt.
func (*Claude) Arm(context.Context, string, Binding, store.Run, string) error { return nil }

// ValidTranscriptPath checks an untrusted transcript path from a hook body
// (v2 §9): absolute, clean, under root (the client's data directory), and
// still under it once symlinks are resolved.
func ValidTranscriptPath(ctx context.Context, files Files, p, root string) error {
	if p == "" {
		return errors.New("the hook gave no transcript_path")
	}
	if !path.IsAbs(p) || path.Clean(p) != p || strings.ContainsAny(p, "\x00\n") {
		return fmt.Errorf("transcript path %q isn't an absolute, clean path", p)
	}
	if !strings.HasPrefix(p, root+"/") {
		return fmt.Errorf("transcript path %q is outside %s", p, root)
	}
	realRoot, err := files.RealPath(ctx, root)
	if err != nil {
		return err
	}
	real, err := files.RealPath(ctx, p)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, strings.TrimSuffix(realRoot, "/")+"/") {
		return fmt.Errorf("transcript path %q resolves outside %s", p, root)
	}
	return nil
}

// claudeRecord is the part of a transcript line hostbud looks at.
type claudeRecord struct {
	Type       string          `json:"type"`
	Timestamp  string          `json:"timestamp"`
	SessionID  string          `json:"sessionId"`
	Attachment json.RawMessage `json:"attachment"`
}

type claudeGoalStatus struct {
	Type      string `json:"type"`
	Met       *bool  `json:"met"`
	Failed    bool   `json:"failed"`
	Sentinel  bool   `json:"sentinel"`
	Condition string `json:"condition"`
	Reason    string `json:"reason"`
}

// ReadGoalState reads the bound transcript read-only from the run's offset
// and takes the newest goal record bound to this run (v2 §5.3): a top-level
// attachment of type goal_status, from the bound session, after the run
// started, with the queued condition. Only complete lines count, and every
// line is parsed as JSON: text is never searched. No record ⇒ pending.
func (c *Claude) ReadGoalState(ctx context.Context, machine string, b Binding, run store.Run, condition string) (GoalState, error) {
	files := c.files(machine)
	root := strings.TrimSuffix(c.home(machine), "/") + "/.claude/projects"
	if err := ValidTranscriptPath(ctx, files, b.TranscriptPath, root); err != nil {
		return GoalState{Status: Unknown, Reason: err.Error(), Offset: run.TranscriptOffset}, nil
	}
	data, err := files.ReadRange(ctx, b.TranscriptPath, run.TranscriptOffset, maxTranscriptRead)
	if err != nil {
		return GoalState{Offset: run.TranscriptOffset}, err
	}
	return scanClaudeTranscript(data, run.TranscriptOffset, b.SessionID, run.StartedAt, condition), nil
}

// scanClaudeTranscript applies the §5.3 rules to the complete lines of data
// (read from offset).
func scanClaudeTranscript(data []byte, offset int64, sessionID string, startedAt time.Time, condition string) GoalState {
	state := GoalState{Status: Pending, Offset: offset}
	for {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			return state // a truncated last line waits for the next read
		}
		line := data[:nl]
		data = data[nl+1:]
		state.Offset += int64(nl + 1)
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec claudeRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return GoalState{Status: Unknown, Reason: "unrecognised transcript line", Offset: state.Offset}
		}
		if rec.Type != "attachment" || len(rec.Attachment) == 0 {
			continue
		}
		var goal claudeGoalStatus
		if err := json.Unmarshal(rec.Attachment, &goal); err != nil || goal.Type != "goal_status" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
		if err != nil || goal.Met == nil {
			return GoalState{Status: Unknown, Reason: "unrecognised goal-state format", Offset: state.Offset}
		}
		if rec.SessionID != sessionID || !at.After(startedAt) || !SameCondition(goal.Condition, condition) {
			continue // another session, an earlier goal, or a changed condition
		}
		switch {
		case *goal.Met && !goal.Sentinel:
			state = GoalState{Status: Achieved, Condition: goal.Condition, At: at, Reason: goal.Reason, Offset: state.Offset}
		case !*goal.Met && goal.Failed:
			state = GoalState{Status: Failed, Condition: goal.Condition, At: at, Reason: goal.Reason, Offset: state.Offset}
		default:
			state = GoalState{Status: Pending, Condition: goal.Condition, At: at, Reason: goal.Reason, Offset: state.Offset}
		}
		if state.Status != Pending {
			return state // a final verdict: nothing after it matters
		}
	}
}
