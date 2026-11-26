package nudge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/session"
	"hostbud/internal/store"
)

const (
	defaultPollInterval = 15 * time.Second
	maxStartLateness    = 30 * time.Second
)

// Repository is the durable singleton schedule and occurrence claim store.
type Repository interface {
	WindowNudge(context.Context) (store.WindowNudge, error)
	PutWindowNudge(context.Context, store.WindowNudge) (store.WindowNudge, error)
	ClaimWindowNudgeOccurrence(context.Context, time.Time, time.Time) (store.WindowNudgeOccurrence, bool, error)
	SkipMissedWindowNudge(context.Context, time.Time, time.Time) (bool, error)
	FinishWindowNudgeOccurrence(context.Context, time.Time, string, string) error
	RecoverWindowNudgeOccurrences(context.Context) error
	TryWindowNudgeLock(context.Context) (func(), bool, error)
}

// SessionManager is the one hostbud session service used for replacement.
type SessionManager interface {
	Create(context.Context, session.Spec) (string, error)
	Kill(context.Context, string, string) error
	SendCommand(context.Context, string, string, string) error
}

// TargetValidator checks that a selected machine is active and that the
// absolute directory exists on it. The callback must not perform writes.
type TargetValidator func(context.Context, string, string) error

// Config constructs the recurring nudge service.
type Config struct {
	Store          Repository
	Sessions       SessionManager
	ValidateTarget TargetValidator
	Bus            *events.Bus
	Log            *slog.Logger
	Now            func() time.Time
	PollInterval   time.Duration
}

// Error is safe for display in the configuration dialog.
type Error struct {
	Status  int
	Message string
	Hint    string
}

func (e *Error) Error() string    { return e.Message }
func (e *Error) HTTPStatus() int  { return e.Status }
func (e *Error) UserHint() string { return e.Hint }

// Service owns one recurring fixed-time nudge schedule.
type Service struct {
	store        Repository
	sessions     SessionManager
	validate     TargetValidator
	bus          *events.Bus
	log          *slog.Logger
	now          func() time.Time
	pollInterval time.Duration
	wake         chan struct{}
}

// New returns a disabled-until-configured schedule service.
func New(cfg Config) *Service {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	return &Service{store: cfg.Store, sessions: cfg.Sessions, validate: cfg.ValidateTarget, bus: cfg.Bus, log: cfg.Log,
		now: cfg.Now, pollInterval: cfg.PollInterval, wake: make(chan struct{}, 1)}
}

// Settings returns the singleton schedule for the API.
func (s *Service) Settings(ctx context.Context) (store.WindowNudge, error) {
	return s.store.WindowNudge(ctx)
}

// PutSettings validates and saves the one schedule. Enabling it only arms its
// next strictly future occurrence; it never invokes the provider immediately.
func (s *Service) PutSettings(ctx context.Context, input store.WindowNudge, skipFirst bool) (store.WindowNudge, error) {
	provider, err := NormalizeProvider(input.Provider)
	if err != nil {
		return store.WindowNudge{}, &Error{Status: http.StatusBadRequest, Message: "Choose Claude or Codex."}
	}
	input.Provider = provider
	input.Directory = path.Clean(input.Directory)
	if input.MachineID == "" || input.Directory == "." || !path.IsAbs(input.Directory) || len(input.Directory) > 4096 {
		return store.WindowNudge{}, &Error{Status: http.StatusBadRequest, Message: "Choose a machine and an absolute directory."}
	}
	preset := Preset(input.Schedule)
	if !ValidPreset(preset) {
		return store.WindowNudge{}, &Error{Status: http.StatusBadRequest, Message: "Choose one of the available daily schedules."}
	}
	if input.Enabled && s.validate == nil {
		return store.WindowNudge{}, &Error{Status: http.StatusServiceUnavailable, Message: "Machine validation is unavailable.", Hint: "Reload hostbud and try again."}
	}
	if input.Enabled {
		if err := s.validate(ctx, input.MachineID, input.Directory); err != nil {
			return store.WindowNudge{}, &Error{Status: http.StatusBadRequest, Message: "The selected machine or directory is unavailable.", Hint: "Choose an active machine and an existing directory."}
		}
	}
	input.NextRunAt = nil
	if input.Enabled {
		next, err := NextAfter(preset, s.now())
		if err != nil {
			return store.WindowNudge{}, &Error{Status: http.StatusBadRequest, Message: "The selected schedule is invalid."}
		}
		if skipFirst {
			next, err = NextAfter(preset, next)
			if err != nil {
				return store.WindowNudge{}, &Error{Status: http.StatusBadRequest, Message: "The selected schedule is invalid."}
			}
		}
		input.NextRunAt = &next
	}
	settings, err := s.store.PutWindowNudge(ctx, input)
	if err != nil {
		return store.WindowNudge{}, err
	}
	s.publish(ctx, settings)
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return settings, nil
}

// Run elects one database-backed scheduler, skips overdue slots on startup,
// and waits for the next due time or a configuration change.
func (s *Service) Run(ctx context.Context) {
	for ctx.Err() == nil {
		release, acquired, err := s.store.TryWindowNudgeLock(ctx)
		if err != nil {
			s.log.Warn("window nudge scheduler lock unavailable")
			if !s.wait(ctx, s.pollInterval) {
				return
			}
			continue
		}
		if !acquired {
			if !s.wait(ctx, s.pollInterval) {
				return
			}
			continue
		}
		s.runLeader(ctx)
		release()
	}
}

func (s *Service) runLeader(ctx context.Context) {
	if err := s.store.RecoverWindowNudgeOccurrences(ctx); err != nil {
		s.log.Warn("window nudge recovery failed")
	}
	if current, err := s.store.WindowNudge(ctx); err == nil {
		s.skipOverdue(ctx, current)
	}
	for ctx.Err() == nil {
		settings, err := s.store.WindowNudge(ctx)
		if err != nil {
			s.log.Warn("window nudge settings unavailable")
			if !s.wait(ctx, s.pollInterval) {
				return
			}
			continue
		}
		if !settings.Enabled || settings.NextRunAt == nil {
			if !s.wait(ctx, s.pollInterval) {
				return
			}
			continue
		}
		due := *settings.NextRunAt
		wait := due.Sub(s.now())
		if wait > 0 {
			// Re-read durable settings periodically so configuration changes made
			// by another app instance are observed before an old due time fires.
			if wait > s.pollInterval {
				wait = s.pollInterval
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				stopTimer(timer)
				return
			case <-s.wake:
				stopTimer(timer)
				continue
			case <-timer.C:
			}
		}
		now := s.now()
		if late := now.Sub(due); late > maxStartLateness {
			s.skipOverdue(ctx, settings)
			continue
		}
		next, err := NextAfter(Preset(settings.Schedule), due)
		if err != nil {
			s.log.Warn("window nudge schedule became invalid")
			if !s.wait(ctx, s.pollInterval) {
				return
			}
			continue
		}
		occurrence, claimed, err := s.store.ClaimWindowNudgeOccurrence(ctx, due, next)
		if err != nil {
			s.log.Warn("window nudge occurrence claim failed")
			if !s.wait(ctx, s.pollInterval) {
				return
			}
			continue
		}
		if !claimed {
			continue
		}
		s.execute(ctx, occurrence)
		if settings, err := s.store.WindowNudge(ctx); err == nil {
			s.publish(ctx, settings)
		}
	}
}

func (s *Service) skipOverdue(ctx context.Context, settings store.WindowNudge) {
	if !settings.Enabled || settings.NextRunAt == nil || settings.NextRunAt.After(s.now()) {
		return
	}
	next, err := NextAfter(Preset(settings.Schedule), s.now())
	if err != nil {
		return
	}
	changed, err := s.store.SkipMissedWindowNudge(ctx, *settings.NextRunAt, next)
	if err != nil {
		s.log.Warn("missed window nudge could not be skipped")
		return
	}
	if changed {
		if current, err := s.store.WindowNudge(ctx); err == nil {
			s.publish(ctx, current)
		}
	}
}

func (s *Service) execute(ctx context.Context, occurrence store.WindowNudgeOccurrence) {
	outcome, detail := store.WindowNudgeSucceeded, "Nudge sent to the selected provider."
	err := s.replaceSession(ctx, occurrence)
	if err != nil {
		outcome, detail = store.WindowNudgeFailed, safeFailure(err)
		s.log.Warn("window nudge occurrence failed", "provider", occurrence.Provider)
	}
	if err := s.store.FinishWindowNudgeOccurrence(ctx, occurrence.ScheduledAt, outcome, detail); err != nil {
		s.log.Warn("window nudge occurrence result could not be saved")
	}
}

func (s *Service) replaceSession(ctx context.Context, occurrence store.WindowNudgeOccurrence) error {
	if s.sessions == nil {
		return errors.New("session service is unavailable")
	}
	if err := s.sessions.Kill(ctx, occurrence.MachineID, SessionName); err != nil {
		var sessionErr *session.Error
		if !errors.As(err, &sessionErr) || sessionErr.Code != session.CodeNotFound {
			return err
		}
	}
	name, err := s.sessions.Create(ctx, session.Spec{
		Machine: occurrence.MachineID, Name: SessionName, Path: occurrence.Directory,
		StartArgv: []string{occurrence.Provider}, ExactName: true,
	})
	if err != nil {
		return err
	}
	if name != SessionName {
		return fmt.Errorf("session service returned an unexpected name")
	}
	return s.sessions.SendCommand(ctx, occurrence.MachineID, name, promptForSession(name))
}

func promptForSession(name string) string { return "hi, please kill this tmux session " + name }

func safeFailure(err error) string {
	var sessionErr *session.Error
	if errors.As(err, &sessionErr) {
		switch sessionErr.Code {
		case session.CodeUnavailable, session.CodeTimeout, session.CodeUnknownMachine:
			return "The selected machine was unavailable at the scheduled time."
		case session.CodePathNotFound:
			return "The selected directory no longer exists."
		case session.CodeTmuxMissing, session.CodeTmuxVersion:
			return "tmux is missing or too old on the selected machine."
		case session.CodeDuplicate:
			return "Another session claimed the fixed nudge session name."
		}
	}
	return "The scheduled session or provider command failed."
}

func (s *Service) publish(ctx context.Context, settings store.WindowNudge) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(events.Event{Type: events.WindowNudgeChanged, Machine: settings.MachineID, Payload: settings})
}

func (s *Service) wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer stopTimer(timer)
	select {
	case <-ctx.Done():
		return false
	case <-s.wake:
		return true
	case <-timer.C:
		return true
	}
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
