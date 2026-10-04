package machines

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
)

const realEd = "AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"

type fakeSSH struct {
	mu      sync.Mutex
	targets []sshx.Target
	closed  []string
	setErr  error
}

func (f *fakeSSH) Exec(context.Context, string, ...string) ([]byte, error) {
	return nil, &sshx.Error{Kind: sshx.KindUnreachable, Message: "can't reach sshd on the server"}
}
func (f *fakeSSH) OpenSFTP(context.Context, string) (io.ReadWriteCloser, error) {
	return nil, errors.New("no sftp")
}
func (f *fakeSSH) SetTargets(ts []sshx.Target) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	for _, t := range ts {
		if err := sshx.ValidateTarget(t); err != nil {
			return err
		}
	}
	f.targets = append([]sshx.Target(nil), ts...)
	return nil
}
func (f *fakeSSH) Targets() []sshx.Target {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sshx.Target(nil), f.targets...)
}
func (f *fakeSSH) CloseMachine(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, id)
	return nil
}
func (f *fakeSSH) Scan(context.Context, string, int) ([]sshx.HostKey, error) {
	k, err := sshx.ParseHostKey("ssh-ed25519", realEd)
	return []sshx.HostKey{k}, err
}

type fakeStore struct {
	mu       sync.Mutex
	rows     []store.Machine
	projects map[string]bool // machine ids with projects
}

func (f *fakeStore) Machines(context.Context) ([]store.Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Machine(nil), f.rows...), nil
}
func (f *fakeStore) CreateMachine(_ context.Context, m store.NewMachine) (store.Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := store.Machine{ID: m.ID, Source: "custom", SSHAlias: m.SSHAlias, Label: m.Label, HostName: m.HostName, Port: m.Port, SSHUser: m.SSHUser, HostKeys: m.HostKeys}
	f.rows = append(f.rows, row)
	return row, nil
}
func (f *fakeStore) RenameMachine(_ context.Context, id, label string) (store.Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].ID == id && f.rows[i].Source == "custom" {
			f.rows[i].Label = label
			return f.rows[i], nil
		}
	}
	return store.Machine{}, store.ErrNotFound
}
func (f *fakeStore) DeleteMachine(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.projects[id] {
		return store.ErrInUse
	}
	for i := range f.rows {
		if f.rows[i].ID == id && f.rows[i].Source == "custom" {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

func newRegistry(t *testing.T, rows ...store.Machine) (*Registry, *fakeSSH, *fakeStore, <-chan events.Event) {
	t.Helper()
	ssh := &fakeSSH{}
	st := &fakeStore{rows: append([]store.Machine{{ID: store.HostMachineID, Source: "host", Label: "Host"}}, rows...), projects: map[string]bool{}}
	bus := events.NewBus()
	ch, cancel := bus.Subscribe(64)
	t.Cleanup(cancel)
	r := New(ssh, st, bus, Options{HostLabel: "Host", PollInterval: time.Hour})
	if err := r.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r, ssh, st, ch
}

func ids(r *Registry) []string {
	var out []string
	for _, inv := range r.Inventories() {
		m, _ := inv.Snapshot()
		out = append(out, m.ID)
	}
	return out
}

func code(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func key() []sshx.HostKey { return []sshx.HostKey{{Type: "ssh-ed25519", Key: realEd}} }

func TestLoadRegistersHostAndValidServers(t *testing.T) {
	good := store.Machine{ID: "s-0011223344", Source: "custom", SSHAlias: "hostbud-custom-s-0011223344", Label: "Build",
		HostName: "server-a.example.com", Port: 22, SSHUser: "dev", HostKeys: "ssh-ed25519 " + realEd + "\n"}
	bad := good
	bad.ID, bad.SSHAlias, bad.HostName = "s-5566778899", "hostbud-custom-s-5566778899", "-oProxyCommand=x"
	r, ssh, _, _ := newRegistry(t, good, bad)
	if got := strings.Join(ids(r), ","); got != "host,s-0011223344" {
		t.Fatalf("ids = %s", got)
	}
	if ts := ssh.Targets(); len(ts) != 1 || ts[0].ID != good.ID || len(ts[0].Keys) != 1 {
		t.Fatalf("targets = %+v", ts)
	}
	inv, _ := r.Inventory(good.ID)
	if m, _ := inv.Snapshot(); m.Source != "custom" || m.Address != "dev@server-a.example.com:22" || m.Label != "Build" {
		t.Fatalf("server = %+v", m)
	}
	if host, _ := r.Inventory(store.HostMachineID); host == nil {
		t.Fatal("no host")
	}
	if _, ok := r.FileSystem(good.ID); !ok {
		t.Fatal("no file browser for the server")
	}
}

func TestAddValidatesAndStartsTracking(t *testing.T) {
	r, ssh, st, ch := newRegistry(t)
	ctx := context.Background()
	base := Server{Label: "Build box", Host: "server-a.example.com", Port: 2222, User: "dev", Keys: key()}
	for name, mutate := range map[string]func(*Server){
		"empty label":  func(s *Server) { s.Label = " " },
		"long label":   func(s *Server) { s.Label = strings.Repeat("x", MaxLabel+1) },
		"control char": func(s *Server) { s.Label = "a\nb" },
		"bad host":     func(s *Server) { s.Host = "-oProxyCommand=x" },
		"bad user":     func(s *Server) { s.User = "a b" },
		"bad port":     func(s *Server) { s.Port = 70000 },
		"no keys":      func(s *Server) { s.Keys = nil },
		"bad key":      func(s *Server) { s.Keys = []sshx.HostKey{{Type: "ssh-rsa", Key: realEd}} },
	} {
		s := base
		mutate(&s)
		if _, err := r.Add(ctx, s); code(err) != CodeInvalid {
			t.Errorf("%s: %v", name, err)
		}
	}
	dup := base
	dup.Label = "host"
	if _, err := r.Add(ctx, dup); code(err) != CodeConflict {
		t.Fatalf("duplicate nickname: %v", err)
	}
	if len(st.rows) != 1 {
		t.Fatalf("rejected adds stored rows: %+v", st.rows)
	}

	r.Start(t.Context())
	m, err := r.Add(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(m.ID, "s-") || m.Label != "Build box" || m.Source != "custom" || m.Address != "dev@server-a.example.com:2222" {
		t.Fatalf("added = %+v", m)
	}
	if ts := ssh.Targets(); len(ts) != 1 || ts[0].Alias != sshx.TargetAliasPrefix+m.ID || ts[0].Port != 2222 {
		t.Fatalf("targets = %+v", ts)
	}
	if row := st.rows[1]; row.HostKeys != "ssh-ed25519 "+realEd+"\n" || row.SSHAlias != sshx.TargetAliasPrefix+m.ID {
		t.Fatalf("row = %+v", row)
	}
	for e := range ch {
		if e.Type == events.MachineStatus && e.Machine == m.ID {
			break
		}
	}
	// The started poller reports the (fake) server unreachable.
	deadline := time.After(5 * time.Second)
	for {
		inv, _ := r.Inventory(m.ID)
		if s, _ := inv.Snapshot(); s.Status == inventory.StatusUnreachable && strings.Contains(s.Error, "server") {
			break
		}
		select {
		case <-deadline:
			t.Fatal("added server never polled")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestRenameAndRemove(t *testing.T) {
	r, ssh, st, ch := newRegistry(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	m, err := r.Add(context.Background(), Server{Label: "One", Host: "10.0.0.5", User: "dev", Keys: key()})
	if err != nil {
		t.Fatal(err)
	}
	if m.Address != "dev@10.0.0.5:22" {
		t.Fatalf("default port: %+v", m)
	}
	if _, err := r.Rename(ctx, store.HostMachineID, "x"); code(err) != CodeNotFound {
		t.Fatalf("host rename: %v", err)
	}
	if _, err := r.Rename(ctx, m.ID, "HOST"); code(err) != CodeConflict {
		t.Fatalf("dup rename: %v", err)
	}
	renamed, err := r.Rename(ctx, m.ID, " Two ")
	if err != nil || renamed.Label != "Two" || st.rows[1].Label != "Two" {
		t.Fatalf("rename = %+v, %v", renamed, err)
	}

	st.projects[m.ID] = true
	if err := r.Remove(ctx, m.ID); code(err) != CodeInUse {
		t.Fatalf("in use: %v", err)
	}
	if _, ok := r.Inventory(m.ID); !ok {
		t.Fatal("refused removal stopped tracking")
	}
	delete(st.projects, m.ID)
	for len(ch) > 0 {
		<-ch
	}
	if err := r.Remove(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Inventory(m.ID); ok || len(ssh.Targets()) != 0 || len(ssh.closed) != 1 || ssh.closed[0] != m.ID {
		t.Fatalf("after remove: tracked=%v targets=%+v closed=%v", ok, ssh.Targets(), ssh.closed)
	}
	var removed bool
	for len(ch) > 0 {
		if e := <-ch; e.Type == events.MachineRemoved && e.Machine == m.ID {
			removed = true
		}
	}
	if !removed {
		t.Fatal("no machine.removed event")
	}
	if err := r.Remove(ctx, store.HostMachineID); code(err) != CodeNotFound {
		t.Fatalf("host remove: %v", err)
	}
	cancel()
	r.Wait()
}

func TestAddRollsBackWhenConfigFails(t *testing.T) {
	r, ssh, st, _ := newRegistry(t)
	ssh.setErr = errors.New("disk full")
	if _, err := r.Add(context.Background(), Server{Label: "One", Host: "server-a", User: "dev", Keys: key()}); err == nil {
		t.Fatal("add succeeded")
	}
	if len(st.rows) != 1 || len(ids(r)) != 1 {
		t.Fatalf("not rolled back: rows=%+v ids=%v", st.rows, ids(r))
	}
}
