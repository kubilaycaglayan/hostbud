package queue

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"hostbud/internal/session"
	"hostbud/internal/store"
)

// Env vars a run session gets (v2 §8).
const (
	EnvURL   = "HOSTBUD_URL"
	EnvRunID = "HOSTBUD_RUN_ID"
	EnvToken = "HOSTBUD_RUN_TOKEN" //nolint:gosec // the variable name, not a credential
)

// RunAgent is what starting a run needs from the item's agent adapter.
type RunAgent interface {
	// CheckVersion returns the client version on the machine, or an
	// actionable error (client missing or too old).
	CheckVersion(ctx context.Context, machine string) (string, error)
	// BuildCommand returns the run's argv: client, flags, hooks, prompt.
	BuildCommand(item store.QueueItem, run store.Run) ([]string, error)
}

// SessionCreator is the single session-create service (session.Service).
type SessionCreator interface {
	Create(ctx context.Context, spec session.Spec) (string, error)
}

// StarterStore is what the starter needs from the store.
type StarterStore interface {
	CreateRun(ctx context.Context, itemID string, tokenHash []byte, startedAt time.Time) (store.Run, error)
	CreateRunInSlot(ctx context.Context, itemID string, tokenHash []byte, startedAt time.Time) (store.Run, error)
	UpdateRun(ctx context.Context, id string, u store.RunUpdate) (store.Run, error)
	TransitionRun(ctx context.Context, id string, from []string, to, detail string, endedAt *time.Time) (store.Run, error)
	AppendRunEvent(ctx context.Context, runID, source, kind string, payload []byte) (store.RunEvent, error)
}

// Starter creates a run and its tmux session (v2 §5.1).
type Starter struct {
	store    StarterStore
	sessions SessionCreator
	hookURL  string
	log      *slog.Logger
	now      func() time.Time
	// inSlot reports whether runs are created behind the machine's cap
	// (V2-M2, the parallel-queues switch; set by the dispatcher).
	inSlot func() bool
}

// NewStarter returns a Starter. hookURL is HOSTBUD_URL inside run sessions.
func NewStarter(st StarterStore, sessions SessionCreator, hookURL string, log *slog.Logger) *Starter {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Starter{store: st, sessions: sessions, hookURL: hookURL, log: log, now: time.Now}
}

// RunSessionName is a run's session name: <project>-q<position> for the
// project's first queue (queueName ""; V2-M1's name), else
// <project>-<queue>-q<position> (V2-M2), each part sanitized like a
// directory name and the whole capped at 60 bytes. Collisions get the v1
// suffixes (-1, -2, …) from the session service, which retries (bounded)
// when tmux reports a duplicate at creation.
func RunSessionName(projectName, queueName string, position int) string {
	suffix := "-q" + strconv.Itoa(position)
	base := session.SanitizeName(projectName)
	if queueName != "" {
		base += "-" + session.SanitizeName(queueName)
	}
	if len(base)+len(suffix) > 60 {
		base = strings.TrimRight(base[:60-len(suffix)], "-")
	}
	return base + suffix
}

// Start creates a run for item in project's directory: a new token, then
// started_at (before the session exists, so anything the session writes
// counts as after start), the client version check, the command, and the
// session through the single session-create service. A failure at any
// step leaves the run failed with an actionable detail; nothing retries.
// The error is only for store failures.
//
// queueName is "" for the project's first queue, else the queue's name for
// the session name (RunSessionName).
func (s *Starter) Start(ctx context.Context, source string, project store.Project, queueName string, item store.QueueItem, agent RunAgent) (store.Run, error) {
	token, hash, err := NewToken()
	if err != nil {
		return store.Run{}, err
	}
	create := s.store.CreateRun
	if s.inSlot != nil && s.inSlot() {
		create = s.store.CreateRunInSlot // store.ErrNoSlot when the cap is reached
	}
	run, err := create(ctx, item.ID, hash, s.now())
	if err != nil {
		return store.Run{}, err
	}
	s.log.Info("run starting", "run", run.ID)
	if _, err := s.store.AppendRunEvent(ctx, run.ID, source, store.RunStarting, detailPayload("")); err != nil {
		return run, err
	}
	if agent == nil {
		return s.fail(ctx, source, run, "unknown agent "+strconv.Quote(item.Agent)+" — edit the item and pick claude or codex")
	}
	version, err := agent.CheckVersion(ctx, project.MachineID)
	if err != nil {
		return s.fail(ctx, source, run, detailOf(err))
	}
	if run, err = s.store.UpdateRun(ctx, run.ID, store.RunUpdate{ClientVersion: &version}); err != nil {
		return run, err
	}
	argv, err := agent.BuildCommand(item, run)
	if err != nil {
		return s.fail(ctx, source, run, detailOf(err))
	}
	name, err := s.sessions.Create(ctx, session.Spec{
		Machine:   project.MachineID,
		Name:      RunSessionName(project.Name, queueName, item.Position),
		Path:      project.Path,
		Env:       map[string]string{EnvURL: s.hookURL, EnvRunID: run.ID, EnvToken: token},
		StartArgv: argv,
	})
	if err != nil {
		return s.fail(ctx, source, run, detailOf(err))
	}
	s.log.Debug("run session created", "run", run.ID, "session", name)
	return s.store.UpdateRun(ctx, run.ID, store.RunUpdate{SessionName: &name})
}

func (s *Starter) fail(ctx context.Context, source string, run store.Run, detail string) (store.Run, error) {
	s.log.Info("run failed to start", "run", run.ID)
	ended := s.now()
	failed, err := s.store.TransitionRun(ctx, run.ID, []string{store.RunStarting}, store.RunFailed, detail, &ended)
	if err != nil {
		return run, err
	}
	_, err = s.store.AppendRunEvent(ctx, run.ID, source, "failed", detailPayload(detail))
	return failed, err
}

// detailOf turns an error into a run detail: the message plus the
// actionable hint of a session error.
func detailOf(err error) string {
	var se *session.Error
	if errors.As(err, &se) && se.Hint != "" {
		return se.Message + " — " + se.Hint
	}
	return err.Error()
}

func detailPayload(detail string) []byte {
	b, _ := json.Marshal(map[string]string{"detail": detail})
	return b
}
