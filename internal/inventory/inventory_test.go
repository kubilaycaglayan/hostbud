package inventory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/sshx"
	"hostbud/internal/tmux"
)

// fakeExec answers the probe and list-sessions from mutable state.
type fakeExec struct {
	mu        sync.Mutex
	probeOut  string
	probeErr  error
	listOut   string
	listErr   error
	listCalls int
	paneOut   string
	paneErr   error
	paneCalls int
}

func (f *fakeExec) Exec(_ context.Context, _ string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Contains(strings.Join(args, " "), "list-panes") {
		f.paneCalls++
		return []byte(f.paneOut), f.paneErr
	}
	if args[0] == "sh" {
		return []byte(f.probeOut), f.probeErr
	}
	f.listCalls++
	return []byte(f.listOut), f.listErr
}

func (f *fakeExec) set(fn func(*fakeExec)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

const probeOK = "os=Linux\nhome=/home/dev\ntmux=/usr/bin/tmux\nversion=tmux 3.4\n"
const probeNoTmux = "os=Linux\nhome=/home/dev\ntmux=\nversion=\n"

func line(name string, attached, windows int, activity int64) string {
	return fmt.Sprintf("$1:%s:%d:%d:1700000000:%d:/home/dev\n", name, attached, windows, activity)
}

type memStore struct {
	mu    sync.Mutex
	saved []Capabilities
}

func (m *memStore) SaveCapabilities(_ context.Context, _ string, c Capabilities, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved = append(m.saved, c)
	return nil
}

type harness struct {
	t      *testing.T
	exec   *fakeExec
	inv    *Inventory
	events <-chan events.Event
	delays chan time.Duration
	store  *memStore
}

// start runs an Inventory whose timer never fires on its own: the test
// steps it with Refresh and reads the requested delays.
func start(t *testing.T, f *fakeExec) *harness {
	t.Helper()
	bus := events.NewBus()
	ch, cancelSub := bus.Subscribe(64)
	h := &harness{t: t, exec: f, events: ch, delays: make(chan time.Duration, 64), store: &memStore{}}
	h.inv = New(f, bus, Options{
		MachineID: "host", Label: "Host machine", Interval: time.Second, Store: h.store,
		After: func(d time.Duration) <-chan time.Time { h.delays <- d; return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.inv.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; cancelSub() })
	h.delay() // the initial poll ran
	return h
}

func (h *harness) step() time.Duration {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.inv.Refresh(ctx); err != nil {
		h.t.Fatal(err)
	}
	return h.delay()
}

func (h *harness) delay() time.Duration {
	h.t.Helper()
	select {
	case d := <-h.delays:
		return d
	case <-time.After(2 * time.Second):
		h.t.Fatal("poller never waited")
		return 0
	}
}

// drain returns the events published so far.
func (h *harness) drain() []events.Event {
	var out []events.Event
	for {
		select {
		case e := <-h.events:
			out = append(out, e)
		default:
			return out
		}
	}
}

func names(e events.Event) []string {
	var n []string
	for _, s := range e.Payload.(SessionsChanged).Sessions {
		n = append(n, s.Name)
	}
	return n
}

func TestFirstPollPublishesStatusAndSessions(t *testing.T) {
	h := start(t, &fakeExec{probeOut: probeOK, listOut: line("b", 0, 1, 1) + line("a", 1, 2, 1)})
	evs := h.drain()
	if len(evs) != 2 || evs[0].Type != events.MachineStatus || evs[1].Type != events.SessionsChanged {
		t.Fatalf("events: %+v", evs)
	}
	m := evs[0].Payload.(Machine)
	if m.Status != StatusOK || m.OS != "Linux" || m.Home != "/home/dev" || m.TmuxVersion != "3.4" || m.TmuxMissing {
		t.Fatalf("machine: %+v", m)
	}
	if got := names(evs[1]); strings.Join(got, ",") != "a,b" {
		t.Fatalf("sessions not sorted: %v", got)
	}
	if len(h.store.saved) != 1 || h.store.saved[0].Home != "/home/dev" {
		t.Fatalf("capabilities not stored: %+v", h.store.saved)
	}
	snapM, snapS := h.inv.Snapshot()
	if snapM.Status != StatusOK || len(snapS) != 2 || snapM.LastSeen == nil {
		t.Fatalf("snapshot: %+v %+v", snapM, snapS)
	}
}

func TestPublishesOnlyOnChange(t *testing.T) {
	f := &fakeExec{probeOut: probeOK, listOut: line("a", 0, 1, 1)}
	h := start(t, f)
	h.drain()

	h.step()
	if evs := h.drain(); len(evs) != 0 {
		t.Fatalf("unchanged poll published %+v", evs)
	}

	f.set(func(f *fakeExec) { f.listOut = line("a", 0, 1, 59) }) // activity only, same minute
	h.step()
	if evs := h.drain(); len(evs) != 0 {
		t.Fatalf("same-minute activity change published %+v", evs)
	}

	for _, change := range []string{
		line("a", 1, 1, 1),                      // attached
		line("a", 1, 3, 1),                      // windows
		line("a", 1, 3, 1) + line("b", 0, 1, 1), // add
		line("b", 0, 1, 1),                      // remove
	} {
		f.set(func(f *fakeExec) { f.listOut = change })
		h.step()
		evs := h.drain()
		if len(evs) != 1 || evs[0].Type != events.SessionsChanged {
			t.Fatalf("change %q: events %+v", change, evs)
		}
	}
	if f.listCalls < 7 {
		t.Fatalf("list calls = %d", f.listCalls)
	}
}

func TestPaneAgentChangesPublishCollapsedSessionMetadata(t *testing.T) {
	f := &fakeExec{probeOut: probeOK, listOut: line("a", 0, 1, 1)}
	h := start(t, f)
	h.drain()

	f.set(func(f *fakeExec) { f.paneOut = "P\ta\t%1\tcoy\tworking\tcodex,\n" })
	h.step()
	evs := h.drain()
	if len(evs) != 1 || evs[0].Type != events.SessionsChanged {
		t.Fatalf("agent appearance events = %+v", evs)
	}
	sessions := evs[0].Payload.(SessionsChanged).Sessions
	if len(sessions) != 1 || !slices.Equal(sessions[0].Agents, []string{"codex"}) || sessions[0].Status != tmux.AgentWorking {
		t.Fatalf("session agent metadata = %+v", sessions)
	}

	f.set(func(f *fakeExec) { f.paneOut = "P\ta\t%1\tbash\tworking\t\n" })
	h.step()
	evs = h.drain()
	if len(evs) != 1 || len(evs[0].Payload.(SessionsChanged).Sessions[0].Agents) != 0 || evs[0].Payload.(SessionsChanged).Sessions[0].Status != tmux.AgentEnded {
		t.Fatalf("agent exit events = %+v", evs)
	}

	f.set(func(f *fakeExec) { f.paneOut = "P\ta\t%1\tcoy\tblocked\tcodex,\n" })
	h.step()
	evs = h.drain()
	if len(evs) != 1 || evs[0].Payload.(SessionsChanged).Sessions[0].Status != tmux.AgentBlocked {
		t.Fatalf("blocked status event = %+v", evs)
	}
	f.set(func(f *fakeExec) { f.paneErr = errors.New("supplementary query failed") })
	h.step()
	_, got := h.inv.Snapshot()
	if !slices.Equal(got[0].Agents, []string{"codex"}) || got[0].Status != tmux.AgentBlocked {
		t.Fatalf("failed supplementary query cleared the last known agent: %+v", got)
	}
}

func TestNoServerIsEmptyList(t *testing.T) {
	h := start(t, &fakeExec{probeOut: probeOK, listErr: &sshx.Error{
		Kind: sshx.KindRemote, ExitCode: 1, Stderr: "no server running on /tmp/tmux-1000/default",
	}})
	evs := h.drain()
	if len(evs) != 2 || evs[0].Payload.(Machine).Status != StatusOK {
		t.Fatalf("events: %+v", evs)
	}
	if s := evs[1].Payload.(SessionsChanged).Sessions; s == nil || len(s) != 0 {
		t.Fatalf("want empty non-nil list, got %#v", s)
	}
}

func TestBackoffAndRecovery(t *testing.T) {
	refused := &sshx.Error{Kind: sshx.KindUnreachable, Message: "can't reach sshd on the host (connection refused)", Hint: "Check that sshd is running"}
	f := &fakeExec{probeOut: probeOK, listOut: line("a", 0, 1, 1)}
	h := start(t, f)
	h.drain()

	f.set(func(f *fakeExec) { f.listErr = refused; f.probeErr = refused })
	var delays []time.Duration
	for range 6 {
		delays = append(delays, h.step())
	}
	want := []time.Duration{1, 2, 4, 8, 8, 8}
	for i := range want {
		if delays[i] != want[i]*time.Second {
			t.Fatalf("delays = %v, want %v (seconds)", delays, want)
		}
	}
	evs := h.drain()
	if len(evs) != 1 {
		t.Fatalf("want one unreachable event, got %+v", evs)
	}
	m := evs[0].Payload.(Machine)
	if m.Status != StatusUnreachable || !strings.Contains(m.Error, "connection refused") || !strings.Contains(m.Hint, "sshd") {
		t.Fatalf("machine: %+v", m)
	}
	if _, sessions := h.inv.Snapshot(); len(sessions) != 1 {
		t.Fatal("last known sessions dropped while unreachable")
	}

	f.set(func(f *fakeExec) { f.listErr = nil; f.probeErr = nil })
	if d := h.step(); d != time.Second {
		t.Fatalf("delay after recovery = %v", d)
	}
	evs = h.drain()
	if len(evs) != 1 || evs[0].Payload.(Machine).Status != StatusOK || evs[0].Payload.(Machine).Error != "" {
		t.Fatalf("recovery events: %+v", evs)
	}
	if len(h.store.saved) != 2 {
		t.Fatalf("recovery didn't re-probe: %d probes stored", len(h.store.saved))
	}
}

func TestTimeoutPollerMarksTimedOutAndBacksOff(t *testing.T) {
	f := &fakeExec{probeOut: probeOK, listOut: line("a", 0, 1, 1)}
	h := start(t, f)
	h.drain()
	f.set(func(f *fakeExec) {
		f.listErr = &sshx.Error{Kind: sshx.KindTimeout, Timeout: 10 * time.Second, Message: "The host didn't answer within 10s"}
	})
	if got := h.step(); got != time.Second {
		t.Fatalf("first timeout delay = %v", got)
	}
	evs := h.drain()
	if len(evs) != 1 {
		t.Fatalf("events: %+v", evs)
	}
	m := evs[0].Payload.(Machine)
	if m.Status != StatusUnreachable || !strings.Contains(m.Error, "timed out") {
		t.Fatalf("machine after timeout: %+v", m)
	}
	if got := h.step(); got != 2*time.Second {
		t.Fatalf("backoff after timeout = %v", got)
	}
	f.set(func(f *fakeExec) { f.listErr = nil })
	if got := h.step(); got != time.Second {
		t.Fatalf("recovery delay = %v", got)
	}
	if m, _ := h.inv.Snapshot(); m.Status != StatusOK {
		t.Fatalf("machine did not recover: %+v", m)
	}
}

func TestTmuxMissingAndInstalled(t *testing.T) {
	f := &fakeExec{probeOut: probeNoTmux}
	h := start(t, f)
	evs := h.drain()
	if len(evs) != 1 {
		t.Fatalf("events: %+v", evs)
	}
	m := evs[0].Payload.(Machine)
	if m.Status != StatusTmuxMissing || !m.TmuxMissing || !strings.Contains(m.Hint, "apt install tmux") {
		t.Fatalf("machine: %+v", m)
	}
	if f.listCalls != 0 {
		t.Fatal("listed sessions without tmux")
	}
	if d := h.step(); d != time.Second {
		t.Fatalf("tmux_missing should re-probe at the interval, got %v", d)
	}
	if evs := h.drain(); len(evs) != 0 {
		t.Fatalf("unchanged tmux_missing republished: %+v", evs)
	}

	f.set(func(f *fakeExec) { f.probeOut = probeOK; f.listOut = line("a", 0, 1, 1) })
	h.step()
	evs = h.drain()
	if len(evs) != 2 || evs[0].Payload.(Machine).Status != StatusOK {
		t.Fatalf("after install: %+v", evs)
	}
}

func TestTmuxRemovedAfterProbe(t *testing.T) {
	f := &fakeExec{probeOut: probeOK, listOut: line("a", 0, 1, 1)}
	h := start(t, f)
	h.drain()
	f.set(func(f *fakeExec) {
		f.listErr = &sshx.Error{Kind: sshx.KindRemote, ExitCode: 127, Stderr: "sh: tmux: not found"}
		f.probeOut = probeNoTmux
	})
	h.step()
	evs := h.drain()
	if len(evs) != 1 || evs[0].Payload.(Machine).Status != StatusTmuxMissing {
		t.Fatalf("events: %+v", evs)
	}
}

func TestRefreshNeedsRun(t *testing.T) {
	inv := New(&fakeExec{}, events.NewBus(), Options{MachineID: "host"})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := inv.Refresh(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestDelayCap(t *testing.T) {
	inv := New(&fakeExec{}, events.NewBus(), Options{Interval: 3 * time.Second})
	if got := inv.delay(10); got != 24*time.Second {
		t.Fatalf("3s interval cap = %v, want 24s", got)
	}
	inv = New(&fakeExec{}, events.NewBus(), Options{Interval: 10 * time.Second})
	if got := inv.delay(10); got != 30*time.Second {
		t.Fatalf("10s interval cap = %v, want 30s", got)
	}
}

func TestSameSessionComparesTitleAndActivityMinute(t *testing.T) {
	base := tmux.Session{ID: "$1", Name: "a", Activity: time.Date(2026, 1, 1, 10, 4, 5, 0, time.UTC)}
	sameMinute := base
	sameMinute.Activity = base.Activity.Add(30 * time.Second)
	if !sameSession(base, sameMinute) {
		t.Error("activity within the same minute must not publish")
	}
	nextMinute := base
	nextMinute.Activity = base.Activity.Add(time.Minute)
	if sameSession(base, nextMinute) {
		t.Error("activity in a new minute must publish")
	}
	titled := base
	titled.Title = "deploy"
	if sameSession(base, titled) {
		t.Error("title change must publish")
	}
}

func TestAgentUsagePublishesChangesAndCopiesSnapshots(t *testing.T) {
	f := &fakeExec{probeOut: probeOK, listOut: line("a", 0, 1, 1)}
	h := start(t, f)
	h.drain()
	for _, counts := range []string{"12,100,200", "12,120,200", "0,120,200"} {
		f.set(func(f *fakeExec) { f.paneOut = "U\ta\tcodex\t" + counts + "\nP\ta\t%1\tcodex\tworking\tcodex,\n" })
		h.step()
		evs := h.drain()
		if len(evs) != 1 || evs[0].Type != events.SessionsChanged {
			t.Fatalf("missing usage event: %+v", evs)
		}
		_, snapshot := h.inv.Snapshot()
		snapshot[0].AgentUsage.TotalTokens = 999
		_, again := h.inv.Snapshot()
		if again[0].AgentUsage.TotalTokens == 999 {
			t.Fatal("snapshot aliases usage")
		}
		h.step()
		if evs := h.drain(); len(evs) != 0 {
			t.Fatalf("unchanged counts published: %+v", evs)
		}
	}
	f.set(func(f *fakeExec) { f.paneOut = "P\ta\t%1\tbash\t\t\n" })
	h.step()
	evs := h.drain()
	if len(evs) != 1 || evs[0].Payload.(SessionsChanged).Sessions[0].AgentUsage != nil {
		t.Fatalf("stale usage: %+v", evs)
	}
}
