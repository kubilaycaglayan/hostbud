//go:build integration

package inventory_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/session"
	"hostbud/internal/sshx"
	"hostbud/internal/testenv"
	"hostbud/internal/tmux"
)

const interval = 500 * time.Millisecond

func run(t *testing.T, host string) (*inventory.Inventory, <-chan events.Event, *sshx.Client) {
	t.Helper()
	c := testenv.Connected(t, host)
	bus := events.NewBus()
	ch, unsub := bus.Subscribe(64)
	inv := inventory.New(c, bus, inventory.Options{MachineID: sshx.HostMachineID, Label: "test", Interval: interval})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { inv.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; unsub() })
	return inv, ch, c
}

// await returns the first event matching ok within d.
func await(t *testing.T, ch <-chan events.Event, d time.Duration, what string, ok func(events.Event) bool) events.Event {
	t.Helper()
	timeout := time.After(d)
	for {
		select {
		case e := <-ch:
			if ok(e) {
				return e
			}
		case <-timeout:
			t.Fatalf("no %s event within %v", what, d)
		}
	}
}

func hasSession(name string) func(events.Event) bool {
	return func(e events.Event) bool {
		if e.Type != events.SessionsChanged {
			return false
		}
		return slices.ContainsFunc(e.Payload.(inventory.SessionsChanged).Sessions,
			func(s tmux.Session) bool { return s.Name == name })
	}
}

func TestIntegrationPollerSeesCreateAndKill(t *testing.T) {
	_, ch, c := run(t, testenv.SSHD)
	testenv.Sh(t, c, "tmux kill-server 2>/dev/null; true")
	t.Cleanup(func() { testenv.Sh(t, c, "tmux kill-server 2>/dev/null; true") })

	await(t, ch, 5*time.Second, "machine ok", func(e events.Event) bool {
		m, ok := e.Payload.(inventory.Machine)
		return ok && m.Status == inventory.StatusOK && m.Home == "/home/dev" && m.TmuxVersion != ""
	})

	// A "real terminal" creates a session out of band.
	testenv.Sh(t, c, "tmux new-session -d -s inv-it")
	start := time.Now()
	await(t, ch, 2*interval, "sessions.changed with inv-it", hasSession("inv-it"))
	t.Logf("create seen after %v", time.Since(start))

	testenv.Sh(t, c, "tmux kill-session -t =inv-it")
	await(t, ch, 2*interval, "sessions.changed without inv-it", func(e events.Event) bool {
		return e.Type == events.SessionsChanged && !hasSession("inv-it")(e)
	})
}

func TestIntegrationTmuxServerKilledListsEmptyAndCanRecreate(t *testing.T) {
	inv, _, c := run(t, testenv.SSHD)
	if err := inv.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	testenv.Sh(t, c, "tmux new-session -d -s server-kill-it")
	if err := inv.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, sessions := inv.Snapshot(); !slices.ContainsFunc(sessions, func(s tmux.Session) bool { return s.Name == "server-kill-it" }) {
		t.Fatalf("initial session not listed: %+v", sessions)
	}
	_, _ = c.Exec(context.Background(), sshx.HostMachineID, "tmux", "kill-server")
	if err := inv.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, sessions := inv.Snapshot(); len(sessions) != 0 {
		t.Fatalf("sessions after tmux server exit = %+v", sessions)
	}
	machine, _ := inv.Snapshot()
	if machine.Status != inventory.StatusOK {
		t.Fatalf("no-server status = %+v; want ok", machine)
	}
	service := session.New(c, map[string]session.Tracker{sshx.HostMachineID: inv}, nil)
	name, err := service.Create(context.Background(), session.Spec{Machine: sshx.HostMachineID, Name: "server-recreated", Path: "/home/dev"})
	if err != nil || name != "server-recreated" {
		t.Fatalf("create after kill-server = %q, %v", name, err)
	}
	if sessions := testenv.Sh(t, c, "tmux list-sessions -F '#{session_name}'"); !strings.Contains(sessions, name) {
		t.Fatalf("session after server restart = %q", sessions)
	}
}

func TestIntegrationPinnedHostKeyErrorAndRecovery(t *testing.T) {
	testenv.Agent(t, true)
	c := testenv.Client(t, testenv.SSHD, testenv.Options{HostKeysDir: testenv.WrongHostKeys(t)})
	bus := events.NewBus()
	ch, unsubscribe := bus.Subscribe(32)
	defer unsubscribe()
	inv := inventory.New(c, bus, inventory.Options{MachineID: sshx.HostMachineID, Interval: interval})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { inv.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	failure := await(t, ch, 5*time.Second, "host-key mismatch status", func(e events.Event) bool {
		m, ok := e.Payload.(inventory.Machine)
		return e.Type == events.MachineStatus && ok && m.Status == inventory.StatusUnreachable
	}).Payload.(inventory.Machine)
	if failure.Error != "The host's SSH key changed." || !strings.Contains(failure.Hint, "If you expected this") {
		t.Fatalf("host-key mismatch status = %+v", failure)
	}
	if entries, err := os.ReadDir(filepath.Join(filepath.Dir(c.ConfigPath()), "cm")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("host-key mismatch created ControlMaster sockets: %v", entries)
	}

	var pins strings.Builder
	entries, err := os.ReadDir(filepath.Join(testenv.KeysDir(), "hostpub"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		key, err := os.ReadFile(filepath.Join(testenv.KeysDir(), "hostpub", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(string(key))
		if len(fields) < 2 {
			t.Fatalf("invalid test host key %s", entry.Name())
		}
		fmt.Fprintf(&pins, "%s %s %s\n", sshx.HostAlias, fields[0], fields[1])
	}
	knownHosts := filepath.Join(filepath.Dir(c.ConfigPath()), "known_hosts")
	if err := os.WriteFile(knownHosts, []byte(pins.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := inv.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	await(t, ch, 5*time.Second, "host after re-pinning", func(e events.Event) bool {
		m, ok := e.Payload.(inventory.Machine)
		return e.Type == events.MachineStatus && ok && m.Status == inventory.StatusOK
	})
}

func TestIntegrationUnreachableSSHDMarksMachine(t *testing.T) {
	testenv.Agent(t, true)
	c := testenv.Client(t, testenv.SSHD, testenv.Options{Port: 2222})
	bus := events.NewBus()
	ch, unsubscribe := bus.Subscribe(32)
	defer unsubscribe()
	inv := inventory.New(c, bus, inventory.Options{MachineID: sshx.HostMachineID, Interval: interval})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { inv.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	failure := await(t, ch, 5*time.Second, "unreachable sshd status", func(e events.Event) bool {
		m, ok := e.Payload.(inventory.Machine)
		return e.Type == events.MachineStatus && ok && m.Status == inventory.StatusUnreachable
	}).Payload.(inventory.Machine)
	if !strings.Contains(failure.Error, "connection refused") || !strings.Contains(failure.Hint, "sshd") {
		t.Fatalf("unreachable sshd status = %+v", failure)
	}
}

func TestIntegrationProbeTmuxMissing(t *testing.T) {
	inv, ch, _ := run(t, testenv.SSHDNoTmux)
	e := await(t, ch, 5*time.Second, "tmux_missing", func(e events.Event) bool {
		m, ok := e.Payload.(inventory.Machine)
		return ok && m.Status == inventory.StatusTmuxMissing
	})
	m := e.Payload.(inventory.Machine)
	if !m.TmuxMissing || m.OS != "Linux" || m.Hint == "" {
		t.Fatalf("machine: %+v", m)
	}
	if _, sessions := inv.Snapshot(); len(sessions) != 0 {
		t.Fatalf("sessions without tmux: %+v", sessions)
	}
}

func TestIntegrationPollerTimeoutAndRecovery(t *testing.T) {
	testenv.Agent(t, true)
	c := testenv.Client(t, testenv.SSHD, testenv.Options{Timeout: 500 * time.Millisecond})
	bus := events.NewBus()
	ch, unsub := bus.Subscribe(64)
	defer unsub()
	inv := inventory.New(c, bus, inventory.Options{MachineID: sshx.HostMachineID, Label: "test", Interval: interval})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { inv.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	await(t, ch, 5*time.Second, "machine ok", func(e events.Event) bool {
		m, ok := e.Payload.(inventory.Machine)
		return ok && m.Status == inventory.StatusOK
	})
	testenv.Sh(t, c, "mkdir -p /home/dev/.hostbud-stall && printf '%d\\n' "+strconv.FormatInt(time.Now().Add(60*time.Second).Unix(), 10)+" >/home/dev/.hostbud-stall/tmux")
	t.Cleanup(func() { testenv.Sh(t, c, "rm -f /home/dev/.hostbud-stall/tmux") })
	e := await(t, ch, 5*time.Second, "timed out status", func(e events.Event) bool {
		m, ok := e.Payload.(inventory.Machine)
		return ok && m.Status == inventory.StatusUnreachable
	})
	if !strings.Contains(e.Payload.(inventory.Machine).Error, "timed out") {
		t.Fatalf("timeout machine: %+v", e.Payload)
	}
	testenv.Sh(t, c, "rm -f /home/dev/.hostbud-stall/tmux")
	await(t, ch, 10*time.Second, "machine recovery", func(e events.Event) bool {
		m, ok := e.Payload.(inventory.Machine)
		return ok && m.Status == inventory.StatusOK
	})
}
