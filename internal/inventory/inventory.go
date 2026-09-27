// Package inventory tracks machines and their tmux sessions: a capability
// probe, then a poller that diffs `tmux list-sessions` against its cache and
// publishes events only on change (docs/ARCHITECTURE.md §5.1). v1 tracks the
// host only. The Poller interface leaves room for a tmux control-mode
// implementation later.
package inventory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/sshx"
	"hostbud/internal/tmux"
)

// Status is a machine's health as shown in the UI.
type Status string

const (
	StatusUnknown     Status = "unknown" // before the first probe
	StatusOK          Status = "ok"
	StatusUnreachable Status = "unreachable"
	StatusTmuxMissing Status = "tmux_missing"
)

// Capabilities come from the probe.
type Capabilities struct {
	OS          string `json:"os"`
	Home        string `json:"home"`
	TmuxVersion string `json:"tmuxVersion"`
	TmuxMissing bool   `json:"tmuxMissing"`
}

// Machine is the published state of one machine (machine.status payload).
type Machine struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status Status `json:"status"`
	// Error and Hint are actionable, safe to show and log (no stderr).
	Error string `json:"error,omitempty"`
	Hint  string `json:"hint,omitempty"`
	Capabilities
	LastSeen *time.Time `json:"lastSeen,omitempty"`
}

// SessionsChanged is the sessions.changed payload: the full current list.
type SessionsChanged struct {
	Sessions []tmux.Session `json:"sessions"`
}

// Executor runs remote commands (sshx.Client in production).
type Executor interface {
	Exec(ctx context.Context, machine string, args ...string) ([]byte, error)
}

// CapabilityStore persists probe results on the machine row. Optional.
type CapabilityStore interface {
	SaveCapabilities(ctx context.Context, machineID string, c Capabilities, seen time.Time) error
}

// Poller is what the rest of hostbud needs from a session tracker.
type Poller interface {
	Run(ctx context.Context)
	Refresh(ctx context.Context) error
	Snapshot() (Machine, []tmux.Session)
}

// Options configure an Inventory.
type Options struct {
	MachineID  string
	Label      string
	Interval   time.Duration
	MaxBackoff time.Duration // default: 8×Interval, at most 30s (never below Interval)
	Store      CapabilityStore
	Log        *slog.Logger
	// After replaces time.After (tests).
	After func(time.Duration) <-chan time.Time
}

// Inventory polls one machine. Safe for concurrent use.
type Inventory struct {
	exec    Executor
	bus     *events.Bus
	opt     Options
	refresh chan chan struct{}

	mu        sync.Mutex
	machine   Machine
	sessions  []tmux.Session
	listed    bool // sessions reflect at least one successful list
	needProbe bool
}

var _ Poller = (*Inventory)(nil)

// New returns an Inventory; call Run to start polling.
func New(exec Executor, bus *events.Bus, opt Options) *Inventory {
	if opt.Interval <= 0 {
		opt.Interval = 3 * time.Second
	}
	if opt.MaxBackoff == 0 {
		opt.MaxBackoff = min(8*opt.Interval, 30*time.Second)
	}
	opt.MaxBackoff = max(opt.MaxBackoff, opt.Interval)
	if opt.Log == nil {
		opt.Log = slog.New(slog.DiscardHandler)
	}
	if opt.After == nil {
		opt.After = time.After
	}
	return &Inventory{
		exec: exec, bus: bus, opt: opt,
		refresh:   make(chan chan struct{}),
		machine:   Machine{ID: opt.MachineID, Label: opt.Label, Status: StatusUnknown},
		needProbe: true,
	}
}

// Snapshot returns the current machine state and a copy of its sessions
// (never nil).
func (inv *Inventory) Snapshot() (Machine, []tmux.Session) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	return inv.machine, append([]tmux.Session{}, inv.sessions...)
}

// Refresh polls now (e.g. right after a mutation) and returns once the
// result has been published. It needs Run to be running.
func (inv *Inventory) Refresh(ctx context.Context) error {
	done := make(chan struct{})
	select {
	case inv.refresh <- done:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Run probes and polls until ctx is done, backing off exponentially while
// the machine is unreachable and recovering on its own.
func (inv *Inventory) Run(ctx context.Context) {
	failures := 0
	var waiters []chan struct{}
	defer func() {
		for _, w := range waiters {
			close(w)
		}
	}()
	for {
		if inv.poll(ctx) {
			failures = 0
		} else {
			failures++
		}
		for _, w := range waiters {
			close(w)
		}
		waiters = nil
		if ctx.Err() != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-inv.opt.After(inv.delay(failures)):
		case w := <-inv.refresh:
			waiters = append(waiters, w)
		}
	}
}

// delay is Interval while healthy, then Interval·2^(n-1) capped at MaxBackoff.
func (inv *Inventory) delay(failures int) time.Duration {
	d := inv.opt.Interval
	for i := 1; i < failures && d < inv.opt.MaxBackoff; i++ {
		d *= 2
	}
	return min(d, inv.opt.MaxBackoff)
}

// probeScript is a constant: no user input reaches it.
const probeScript = `printf 'os=%s\n' "$(uname -s)"; printf 'home=%s\n' "$HOME"; ` +
	`printf 'tmux=%s\n' "$(command -v tmux)"; printf 'version=%s\n' "$(tmux -V 2>/dev/null)"`

func (inv *Inventory) probe(ctx context.Context) (Capabilities, error) {
	out, err := inv.exec.Exec(ctx, inv.opt.MachineID, "sh", "-c", probeScript)
	if err != nil {
		return Capabilities{}, err
	}
	var c Capabilities
	var tmuxPath string
	for line := range strings.SplitSeq(string(out), "\n") {
		k, v, _ := strings.Cut(line, "=")
		switch k {
		case "os":
			c.OS = v
		case "home":
			c.Home = v
		case "tmux":
			tmuxPath = v
		case "version":
			if tv, err := tmux.ParseVersion(v); err == nil {
				c.TmuxVersion = tv.Raw
			}
		}
	}
	c.TmuxMissing = tmuxPath == ""
	return c, nil
}

// poll runs one probe (when needed) and list. It reports false on failures
// that should back off.
func (inv *Inventory) poll(ctx context.Context) bool {
	inv.mu.Lock()
	needProbe := inv.needProbe
	inv.mu.Unlock()

	if needProbe {
		c, err := inv.probe(ctx)
		if err != nil {
			inv.fail(err)
			return false
		}
		inv.saveCapabilities(ctx, c)
		if c.TmuxMissing {
			inv.tmuxMissing(c)
			return true // keep re-probing at the normal interval
		}
		inv.mu.Lock()
		inv.needProbe = false
		inv.machine.Capabilities = c
		inv.mu.Unlock()
	}

	out, err := inv.exec.Exec(ctx, inv.opt.MachineID, tmux.ListSessions()...)
	var sessions []tmux.Session
	var e *sshx.Error
	switch {
	case err == nil:
		if sessions, err = tmux.ParseSessions(string(out)); err != nil {
			inv.opt.Log.Warn("can't parse tmux list-sessions output", "machine", inv.opt.MachineID, "err", err)
			inv.fail(err)
			return false
		}
	case errors.As(err, &e) && e.Kind == sshx.KindRemote && tmux.NoServer(e.Stderr):
		// No tmux server: an empty list, not an error.
	case errors.As(err, &e) && e.Kind == sshx.KindRemote && e.ExitCode == 127:
		// "command not found": tmux was removed since the probe.
		inv.mu.Lock()
		c := inv.machine.Capabilities
		inv.mu.Unlock()
		c.TmuxMissing, c.TmuxVersion = true, ""
		inv.tmuxMissing(c)
		return true
	default:
		inv.fail(err)
		return false
	}

	// Status first, so a UI clears its banner before the list updates.
	now := time.Now().UTC()
	inv.setMachine(func(m *Machine) {
		m.Status, m.Error, m.Hint = StatusOK, "", ""
		m.LastSeen = &now
	})
	inv.setSessions(sessions)
	return true
}

// tmuxMissing marks the machine tmux_missing; the next poll re-probes.
func (inv *Inventory) tmuxMissing(c Capabilities) {
	inv.mu.Lock()
	inv.needProbe = true
	inv.mu.Unlock()
	inv.setMachine(func(m *Machine) {
		m.Capabilities = c
		m.Status = StatusTmuxMissing
		m.Error = "tmux not found on the host"
		m.Hint = "Install it with `sudo apt install tmux` (macOS: `brew install tmux`); hostbud picks it up automatically."
	})
}

func (inv *Inventory) saveCapabilities(ctx context.Context, c Capabilities) {
	if inv.opt.Store == nil {
		return
	}
	if err := inv.opt.Store.SaveCapabilities(ctx, inv.opt.MachineID, c, time.Now().UTC()); err != nil {
		inv.opt.Log.Warn("can't store machine capabilities", "machine", inv.opt.MachineID, "err", err)
	}
}

// fail marks the machine unreachable with an actionable message and makes
// the next poll re-probe.
func (inv *Inventory) fail(err error) {
	msg, hint := "can't list tmux sessions on the host", "Check `make logs` with HOSTBUD_LOG_LEVEL=debug."
	var e *sshx.Error
	if errors.As(err, &e) {
		if e.Kind != sshx.KindRemote {
			msg, hint = e.Message, e.Hint
		}
		if e.Kind == sshx.KindTimeout {
			msg = fmt.Sprintf("The host timed out after %s", e.Timeout)
		}
		inv.opt.Log.Debug("poll failed", "machine", inv.opt.MachineID, "kind", e.Kind, "stderr", e.Stderr)
	}
	inv.mu.Lock()
	inv.needProbe = true
	inv.mu.Unlock()
	inv.setMachine(func(m *Machine) {
		m.Status, m.Error, m.Hint = StatusUnreachable, msg, hint
	})
}

// setMachine applies f and publishes machine.status if anything but
// LastSeen changed.
func (inv *Inventory) setMachine(f func(*Machine)) {
	inv.mu.Lock()
	before := inv.machine
	f(&inv.machine)
	after := inv.machine
	inv.mu.Unlock()

	b, a := before, after
	b.LastSeen, a.LastSeen = nil, nil
	if b == a {
		return
	}
	if before.Status != after.Status {
		inv.opt.Log.Info("machine status", "machine", after.ID, "status", after.Status, "error", after.Error)
	}
	inv.bus.Publish(events.Event{Type: events.MachineStatus, Machine: after.ID, Payload: after})
}

// setSessions stores the list and publishes sessions.changed when it
// differs (ignoring activity timestamps, which change on every keystroke).
func (inv *Inventory) setSessions(sessions []tmux.Session) {
	if sessions == nil {
		sessions = []tmux.Session{}
	}
	slices.SortFunc(sessions, func(a, b tmux.Session) int { return strings.Compare(a.Name, b.Name) })

	inv.mu.Lock()
	changed := !inv.listed || !slices.EqualFunc(inv.sessions, sessions, sameSession)
	inv.sessions = sessions
	inv.listed = true
	inv.mu.Unlock()

	if changed {
		inv.bus.Publish(events.Event{
			Type: events.SessionsChanged, Machine: inv.opt.MachineID,
			Payload: SessionsChanged{Sessions: append([]tmux.Session{}, sessions...)},
		})
	}
}

func sameSession(a, b tmux.Session) bool {
	a.Activity, b.Activity = time.Time{}, time.Time{}
	return a == b
}
