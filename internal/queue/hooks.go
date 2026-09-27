package queue

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"hostbud/internal/store"
)

// Hook events a run session reports (v2 §8). Clients map their own names:
// SessionStart, Stop and SessionEnd.
const (
	EventSessionStart = "session_start"
	EventTurnEnd      = "turn_end"
	EventSessionEnd   = "session_end"
)

// HookEvents lists the accepted events.
var HookEvents = []string{EventSessionStart, EventTurnEnd, EventSessionEnd}

// MaxHookBody is the hook body cap (the run event payload cap).
const MaxHookBody = store.MaxRunEventPayload

// endedRunStatuses revoke a run's token: a hook for them answers 410. A
// stale run keeps its token, so a late achieved record is still seen.
var endedRunStatuses = []string{store.RunAchieved, store.RunFailed, store.RunExited, store.RunCancelled}

// Signal is one accepted hook, handed to the dispatcher.
type Signal struct {
	RunID string
	Event string
	Body  []byte
	At    time.Time
}

// Notifier receives accepted hooks (the dispatcher, T9).
type Notifier interface {
	Notify(Signal)
}

type nopNotifier struct{}

func (nopNotifier) Notify(Signal) {}

// HookStore is what the hook receiver needs from the store.
type HookStore interface {
	Run(ctx context.Context, id string) (store.Run, error)
	AppendRunEvent(ctx context.Context, runID, source, kind string, payload []byte) (store.RunEvent, error)
}

// HookError is a refused hook: the HTTP status and a short message.
type HookError struct {
	Status  int
	Message string
}

func (e *HookError) Error() string { return e.Message }

// Hooks receives run hooks (POST /api/hooks/{run}/{event}). A hook only
// writes a run_events row and notifies the dispatcher; it never runs a
// command. The bearer token is its only credential (v2 §8, §9).
type Hooks struct {
	store   HookStore
	notify  Notifier
	limiter *limiter
	log     *slog.Logger
	now     func() time.Time
}

// NewHooks returns a hook receiver. A nil notifier drops signals (before the
// dispatcher exists).
func NewHooks(st HookStore, notify Notifier, log *slog.Logger) *Hooks {
	if notify == nil {
		notify = nopNotifier{}
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Hooks{store: st, notify: notify, limiter: newLimiter(HookRatePerMinute, HookBurst), log: log, now: time.Now}
}

// SetNotifier connects the dispatcher.
func (h *Hooks) SetNotifier(n Notifier) { h.notify = n }

// Receive checks and records one hook. The checks run in a fixed order:
// unknown event or run → 404, bad token → 401, ended run → 410, over the
// rate limit → 429, body over 64 KiB → 413, body not JSON → 400.
func (h *Hooks) Receive(ctx context.Context, runID, event, authorization string, body io.Reader) error {
	if !slices.Contains(HookEvents, event) || runID == "" || len(runID) > 64 {
		return &HookError{http.StatusNotFound, "unknown run or event"}
	}
	run, err := h.store.Run(ctx, runID)
	if errors.Is(err, store.ErrNotFound) {
		return &HookError{http.StatusNotFound, "unknown run or event"}
	} else if err != nil {
		return err
	}
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || !TokenMatches(strings.TrimSpace(token), run.TokenHash) {
		h.log.Info("run hook refused", "run", run.ID, "reason", "bad_token")
		return &HookError{http.StatusUnauthorized, "invalid run token"}
	}
	if slices.Contains(endedRunStatuses, run.Status) {
		return &HookError{http.StatusGone, "the run has ended"}
	}
	now := h.now()
	if !h.limiter.allow(run.ID, now) {
		h.log.Info("run hook refused", "run", run.ID, "reason", "rate_limited")
		return &HookError{http.StatusTooManyRequests, "too many hook calls for this run"}
	}
	data, err := io.ReadAll(io.LimitReader(body, MaxHookBody+1))
	if err != nil {
		return &HookError{http.StatusBadRequest, "could not read the hook body"}
	}
	if len(data) > MaxHookBody {
		return &HookError{http.StatusRequestEntityTooLarge, "hook body is larger than 64 KiB"}
	}
	if !json.Valid(data) {
		return &HookError{http.StatusBadRequest, "hook body must be JSON"}
	}
	if _, err := h.store.AppendRunEvent(ctx, run.ID, store.SourceHook, event, data); err != nil {
		if errors.Is(err, store.ErrPayloadTooLarge) {
			return &HookError{http.StatusRequestEntityTooLarge, "hook body is larger than 64 KiB"}
		}
		return err
	}
	h.log.Debug("run hook", "run", run.ID, "event", event)
	h.notify.Notify(Signal{RunID: run.ID, Event: event, Body: data, At: now})
	return nil
}
