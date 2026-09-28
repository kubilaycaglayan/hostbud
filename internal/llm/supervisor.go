package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/notify"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
	"hostbud/internal/tmux"
)

type SupervisorStore interface {
	LLMEligibleRuns(context.Context) ([]store.Run, error)
	RecoverLLMClaims(context.Context) error
	SkipLLM(context.Context, string) error
	ClaimLLM(context.Context, string, *time.Time, int, time.Duration) (bool, error)
	FinishLLM(context.Context, string, *time.Time, []byte) (bool, store.Run, string, error)
	FinishLLMNotify(context.Context, string, *time.Time, []byte, *store.Notice) (bool, store.Run, string, error)
	LLMNoticeContext(context.Context, string) (string, string, int, error)
}
type Capture interface {
	ExecTo(context.Context, string, io.Writer, ...string) error
}
type Flag struct {
	Label  Label     `json:"label"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}
type Supervisor struct {
	st         SupervisorStore
	ssh        Capture
	classifier Classifier
	bus        *events.Bus
	quiet      time.Duration
	budget     int
	scrub      bool
	log        *slog.Logger
	now        func() time.Time
	notifier   interface {
		Enabled(context.Context) bool
		Wake()
	}
}

func (s *Supervisor) SetNotifier(n interface {
	Enabled(context.Context) bool
	Wake()
}) {
	s.notifier = n
}

func NewSupervisor(st SupervisorStore, ssh Capture, classifier Classifier, bus *events.Bus, quiet time.Duration, budget int, scrub bool, log *slog.Logger) *Supervisor {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Supervisor{st: st, ssh: ssh, classifier: classifier, bus: bus, quiet: quiet, budget: budget, scrub: scrub, log: log, now: time.Now}
}
func (s *Supervisor) Run(ctx context.Context) {
	if s == nil || s.classifier == nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scan(ctx)
		}
	}
}
func (s *Supervisor) scan(ctx context.Context) {
	if err := s.st.RecoverLLMClaims(ctx); err != nil {
		s.log.Warn("LLM supervisor could not recover claims", "err", err)
		return
	}
	runs, err := s.st.LLMEligibleRuns(ctx)
	if err != nil {
		s.log.Warn("LLM supervisor could not list runs", "err", err)
		return
	}
	for _, run := range runs {
		if !due(run, s.now(), s.quiet) {
			continue
		}
		s.classify(ctx, run)
	}
}
func (s *Supervisor) classify(ctx context.Context, run store.Run) {
	args, ok := captureArgs(run.SessionName)
	if !ok {
		return
	}
	claimed, err := s.st.ClaimLLM(ctx, run.ID, run.LastSignalAt, s.budget, s.quiet)
	if err != nil || !claimed {
		return
	}
	var out boundedCapture
	captureCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err = s.ssh.ExecTo(captureCtx, run.MachineID, &out, args...)
	cancel()
	if err != nil {
		var sshErr *sshx.Error
		if errors.As(err, &sshErr) && sshErr.Kind == sshx.KindRemote && (strings.Contains(strings.ToLower(sshErr.Stderr), "can't find session") || strings.Contains(strings.ToLower(sshErr.Stderr), "session not found")) {
			_ = s.st.SkipLLM(ctx, run.ID)
			return
		}
		s.finish(ctx, run, Result{Label: Unknown, Reason: "could not capture the run pane"})
		return
	}
	pane := lastLines(out.String(), 200)
	token := ""
	if !s.scrub {
		token, err = s.runToken(ctx, run)
		if err != nil {
			s.finish(ctx, run, Result{Label: Unknown, Reason: "could not safely prepare the run pane"})
			return
		}
	}
	pane = PreparePane(pane, s.scrub, token)
	classifyCtx, classifyCancel := context.WithTimeout(ctx, 60*time.Second)
	result, err := s.classifier.Classify(classifyCtx, pane)
	classifyCancel()
	if err != nil {
		result = Result{Label: Unknown, Reason: "classification failed"}
	}
	result.Reason = cleanReason(PreparePane(result.Reason, true, ""))
	s.finish(ctx, run, result)
}
func (s *Supervisor) runToken(ctx context.Context, run store.Run) (string, error) {
	args, ok := tokenArgs(run.SessionName)
	if !ok {
		return "", errors.New("invalid session name")
	}
	var out boundedCapture
	captureCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := s.ssh.ExecTo(captureCtx, run.MachineID, &out, args...)
	cancel()
	if err != nil {
		return "", errors.New("could not read run token")
	}
	token, ok := parseRunToken(out.String())
	if !ok {
		return "", errors.New("run token was unavailable")
	}
	return token, nil
}

func tokenArgs(sessionName string) ([]string, bool) {
	if err := tmux.ValidateName(sessionName); err != nil {
		return nil, false
	}
	return []string{"tmux", "show-environment", "-t", "=" + sessionName, "HOSTBUD_RUN_TOKEN"}, true
}

func parseRunToken(output string) (string, bool) {
	line := strings.TrimSuffix(strings.TrimSpace(output), "\n")
	const prefix = "HOSTBUD_RUN_TOKEN="
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	token := strings.TrimPrefix(line, prefix)
	if len(token) < 32 || strings.ContainsAny(token, "\r\n\x00") {
		return "", false
	}
	return token, true
}

func due(run store.Run, now time.Time, quiet time.Duration) bool {
	if run.Status == store.RunStale {
		return true
	}
	base := run.StartedAt
	if run.LastSignalAt != nil {
		base = *run.LastSignalAt
	}
	return !now.Before(base.Add(quiet))
}

func captureArgs(sessionName string) ([]string, bool) {
	if err := tmux.ValidateName(sessionName); err != nil {
		return nil, false
	}
	return []string{"tmux", "capture-pane", "-p", "-J", "-t", "=" + sessionName + ":", "-S", "-200"}, true
}
func (s *Supervisor) finish(ctx context.Context, run store.Run, result Result) {
	if !labels[result.Label] {
		result = Result{Label: Unknown, Reason: "provider returned an invalid classification"}
	}
	b, _ := json.Marshal(result)
	var notice *store.Notice
	var payload *notify.Payload
	if flaggable(result.Label) && s.notifier != nil && s.notifier.Enabled(ctx) {
		qid, project, position, contextErr := s.st.LLMNoticeContext(ctx, run.ID)
		if contextErr == nil {
			p, buildErr := notify.Build(notify.Event{Kind: notify.KindAttention, RunID: run.ID, QueueID: qid, ItemID: run.ItemID, Project: project, Position: position, Outcome: string(result.Label), Key: "run:" + run.ID + ":llm:" + string(result.Label)})
			if buildErr == nil {
				if body, jsonErr := p.JSON(); jsonErr == nil {
					payload = &p
					notice = &store.Notice{Kind: notify.KindAttention, Key: p.Key, Payload: body}
				}
			}
		}
	}
	inserted, updated, qid, err := s.st.FinishLLMNotify(ctx, run.ID, run.LastSignalAt, b, notice)
	if err != nil || !inserted {
		return
	}
	if notice != nil && s.notifier != nil {
		s.notifier.Wake()
	}
	var flagJSON json.RawMessage
	if result.Label != Running && result.Label != Unknown {
		f := Flag{Label: result.Label, Reason: result.Reason, At: s.now().UTC()}
		flagJSON, _ = json.Marshal(f)
	}
	if s.bus != nil {
		s.bus.Publish(events.Event{Type: events.RunChanged, Machine: updated.MachineID, Payload: FlaggedRun{RunID: updated.ID, ItemID: updated.ItemID, QueueID: qid, Status: updated.Status, Flag: flagJSON, Notification: payload}})
	}
}
func flaggable(label Label) bool { return label != Running && label != Unknown }

type FlaggedRun struct {
	RunID        string          `json:"runId"`
	ItemID       string          `json:"itemId"`
	QueueID      string          `json:"queueId"`
	Status       string          `json:"status"`
	Flag         json.RawMessage `json:"flag,omitempty"`
	Notification *notify.Payload `json:"notification,omitempty"`
}
type boundedCapture struct{ bytes.Buffer }

func (b *boundedCapture) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 256<<10 {
		keep := min(n, (256<<10)-b.Len())
		_, _ = b.Buffer.Write(p[:keep])
	}
	return n, nil
}
func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
