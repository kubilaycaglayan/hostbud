package session

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"hostbud/internal/inventory"
	"hostbud/internal/sshx"
	"hostbud/internal/tmux"
)

// fakeExec records commands; handler decides each result.
type fakeExec struct {
	mu      sync.Mutex
	calls   [][]string
	handler func(args []string) error
}

func (f *fakeExec) Exec(_ context.Context, _ string, args ...string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, args)
	f.mu.Unlock()
	if f.handler != nil {
		return nil, f.handler(args)
	}
	return nil, nil
}

func (f *fakeExec) tmuxCalls() [][]string {
	var out [][]string
	for _, c := range f.calls {
		if c[0] == "tmux" {
			out = append(out, c)
		}
	}
	return out
}

type fakeTracker struct {
	machine   inventory.Machine
	sessions  []tmux.Session
	refreshes int
	onRefresh func()
}

func (t *fakeTracker) Snapshot() (inventory.Machine, []tmux.Session) { return t.machine, t.sessions }
func (t *fakeTracker) Refresh(context.Context) error {
	t.refreshes++
	if t.onRefresh != nil {
		t.onRefresh()
	}
	return nil
}

func okHost(sessions ...string) *fakeTracker {
	t := &fakeTracker{machine: inventory.Machine{
		ID: "host", Status: inventory.StatusOK,
		Capabilities: inventory.Capabilities{Home: "/home/dev", TmuxVersion: "3.4"},
	}}
	for _, s := range sessions {
		t.sessions = append(t.sessions, tmux.Session{Name: s})
	}
	return t
}

func newSvc(f *fakeExec, t *fakeTracker) *Service {
	return New(f, map[string]Tracker{"host": t}, nil)
}

type lifecycleRecorder struct{ calls []string }

func (l *lifecycleRecorder) RenameSessionLink(_ context.Context, machine, oldName, newName string) error {
	l.calls = append(l.calls, "rename "+machine+" "+oldName+" "+newName)
	return nil
}
func (l *lifecycleRecorder) EndSessionLink(_ context.Context, machine, name string) error {
	l.calls = append(l.calls, "end "+machine+" "+name)
	return nil
}

func TestLifecycleHooksRunBeforeInventoryRefresh(t *testing.T) {
	tracker := okHost("old", "new")
	hooks := &lifecycleRecorder{}
	tracker.onRefresh = func() {
		if len(hooks.calls) == 0 {
			t.Fatal("inventory refreshed before linked session metadata")
		}
	}
	svc := New(&fakeExec{}, map[string]Tracker{"host": tracker}, nil, hooks)
	if err := svc.Rename(context.Background(), "host", "old", "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Kill(context.Background(), "host", "new"); err != nil {
		t.Fatal(err)
	}
	want := []string{"rename host old renamed", "end host new"}
	if !slices.Equal(hooks.calls, want) {
		t.Fatalf("lifecycle hooks = %v; want %v", hooks.calls, want)
	}
}

func remote(code int, stderr string) error {
	return &sshx.Error{Kind: sshx.KindRemote, ExitCode: code, Stderr: stderr}
}

func code(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestCreateDefaults(t *testing.T) {
	f, tr := &fakeExec{}, okHost()
	name, err := newSvc(f, tr).Create(context.Background(), Spec{Machine: "host"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "dev" {
		t.Fatalf("name = %q, want dev (the home dir's name)", name)
	}
	if !slices.Equal(f.calls[0], []string{"test", "-d", "/home/dev"}) {
		t.Fatalf("path check: %q", f.calls[0])
	}
	want := []string{"tmux", "new-session", "-d", "-s", "dev", "-c", "/home/dev"}
	if got := f.tmuxCalls(); len(got) != 1 || !slices.Equal(got[0], want) {
		t.Fatalf("tmux calls: %q", got)
	}
	if tr.refreshes != 1 {
		t.Fatalf("refreshes = %d", tr.refreshes)
	}
}

func TestCreateNameFromPathWithSuffixOnClash(t *testing.T) {
	cases := []struct {
		path, want string
		taken      []string
	}{
		{"~/code/my.app", "my-app", nil},
		{"/root/docs/dev", "dev", nil},
		{"/srv/api", "api-1", []string{"api"}},
		{"/srv/api/", "api-2", []string{"api", "api-1"}},
		{"/srv/api", "api-1", []string{"api", "api-2"}},
		{"~", "dev", nil},
		{"/", "root", nil},
		{"/srv/..hidden..", "hidden", nil},
	}
	for _, c := range cases {
		f := &fakeExec{}
		name, err := newSvc(f, okHost(c.taken...)).Create(context.Background(), Spec{Machine: "host", Path: c.path})
		if err != nil || name != c.want {
			t.Errorf("path %q: got %q, %v; want %q", c.path, name, err, c.want)
		}
	}
}

func TestCreateExpandsTildeAndPassesCommandAndEnv(t *testing.T) {
	f := &fakeExec{}
	_, err := newSvc(f, okHost()).Create(context.Background(), Spec{
		Machine: "host", Name: "web", Path: "~/some/dir", StartCommand: "htop", Env: map[string]string{"K": "v"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tmux", "new-session", "-d", "-s", "web", "-c", "/home/dev/some/dir", "-e", "K=v", "htop"}
	if got := f.tmuxCalls(); !slices.Equal(got[0], want) {
		t.Fatalf("got %q", got[0])
	}
}

func TestCreateAutoNameRetriesOnRace(t *testing.T) {
	n := 0
	f := &fakeExec{handler: func(args []string) error {
		if args[0] == "tmux" && n < 2 {
			n++
			return remote(1, "duplicate session: "+args[4])
		}
		return nil
	}}
	name, err := newSvc(f, okHost()).Create(context.Background(), Spec{Machine: "host", Path: "/srv/api"})
	if err != nil || name != "api-2" {
		t.Fatalf("got %q, %v", name, err)
	}
}

func TestCreateCustomNameIsKept(t *testing.T) {
	f := &fakeExec{}
	name, err := newSvc(f, okHost("dev")).Create(context.Background(), Spec{Machine: "host", Name: "my-work", Path: "~"})
	if err != nil || name != "my-work" {
		t.Fatalf("got %q, %v", name, err)
	}
}

func TestCreateRetriesOnceWhenTheServerRaced(t *testing.T) {
	tries := 0
	f := &fakeExec{handler: func(args []string) error {
		if args[0] == "tmux" {
			tries++
			if tries == 1 {
				return remote(1, "server exited unexpectedly\n")
			}
		}
		return nil
	}}
	tr := okHost()
	name, err := newSvc(f, tr).Create(context.Background(), Spec{Machine: "host", Name: "web"})
	if err != nil || name != "web" || tries != 2 || tr.refreshes != 1 {
		t.Fatalf("got %q, %v after %d tries", name, err, tries)
	}

	// A second failure is reported, not retried forever.
	always := &fakeExec{handler: func(args []string) error {
		if args[0] == "tmux" {
			return remote(1, "server exited unexpectedly\n")
		}
		return nil
	}}
	if _, err := newSvc(always, okHost()).Create(context.Background(), Spec{Machine: "host", Name: "web"}); code(err) != CodeInternal {
		t.Fatalf("persistent failure: %v", err)
	}
}

func TestCreateValidation(t *testing.T) {
	for _, bad := range []string{"a.b", "a:b", "a b", strings.Repeat("x", 65)} {
		f := &fakeExec{}
		_, err := newSvc(f, okHost()).Create(context.Background(), Spec{Machine: "host", Name: bad})
		if code(err) != CodeInvalid || len(f.calls) != 0 {
			t.Errorf("name %q: %v, %d remote calls", bad, err, len(f.calls))
		}
	}
	f := &fakeExec{}
	if _, err := newSvc(f, okHost()).Create(context.Background(), Spec{Machine: "host", Path: "relative/dir"}); code(err) != CodeInvalid {
		t.Errorf("relative path: %v", err)
	}
	if _, err := newSvc(f, okHost()).Create(context.Background(), Spec{Machine: "server-a"}); code(err) != CodeUnknownMachine {
		t.Errorf("unknown machine: %v", err)
	}
	tr := okHost()
	tr.machine.TmuxVersion = "3.1"
	_, err := newSvc(f, tr).Create(context.Background(), Spec{Machine: "host", Env: map[string]string{"K": "v"}})
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeInvalid || !strings.Contains(e.Hint, "3.2") {
		t.Errorf("env on 3.1: %v", err)
	}
}

func TestCreateErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		handler func([]string) error
		want    Code
		hint    string
	}{
		{"missing path", func(a []string) error {
			if a[0] == "test" {
				return remote(1, "")
			}
			return nil
		}, CodePathNotFound, "existing directory"},
		{"duplicate", func(a []string) error {
			if a[0] == "tmux" {
				return remote(1, "duplicate session: web")
			}
			return nil
		}, CodeDuplicate, "another name"},
		{"tmux gone", func(a []string) error {
			if a[0] == "tmux" {
				return remote(127, "sh: 1: tmux: not found")
			}
			return nil
		}, CodeTmuxMissing, "apt install tmux"},
		{"unreachable", func([]string) error {
			return &sshx.Error{Kind: sshx.KindUnreachable, Message: "can't reach sshd", Hint: "Check that sshd is running"}
		}, CodeUnavailable, "sshd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := okHost()
			_, err := newSvc(&fakeExec{handler: c.handler}, tr).Create(context.Background(), Spec{Machine: "host", Name: "web"})
			var e *Error
			if !errors.As(err, &e) || e.Code != c.want || !strings.Contains(e.Hint, c.hint) {
				t.Fatalf("got %+v", err)
			}
			if tr.refreshes != 0 {
				t.Fatal("refreshed after a failed create")
			}
		})
	}
}

func TestMachineNotReady(t *testing.T) {
	for status, want := range map[inventory.Status]Code{
		inventory.StatusTmuxMissing: CodeTmuxMissing,
		inventory.StatusUnreachable: CodeUnavailable,
		inventory.StatusUnknown:     CodeUnavailable,
	} {
		tr := okHost()
		tr.machine.Status, tr.machine.Error, tr.machine.Hint = status, "e", "h"
		f := &fakeExec{}
		svc := newSvc(f, tr)
		_, err1 := svc.Create(context.Background(), Spec{Machine: "host"})
		err2 := svc.Rename(context.Background(), "host", "a", "b")
		err3 := svc.Kill(context.Background(), "host", "a")
		for _, err := range []error{err1, err2, err3} {
			if code(err) != want {
				t.Errorf("%s: got %v", status, err)
			}
		}
		if len(f.calls) != 0 {
			t.Errorf("%s: ran remote commands", status)
		}
	}
}

func TestRename(t *testing.T) {
	f, tr := &fakeExec{}, okHost("old")
	if err := newSvc(f, tr).Rename(context.Background(), "host", "old", "new"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.calls[0], []string{"tmux", "rename-session", "-t", "=old", "new"}) || tr.refreshes != 1 {
		t.Fatalf("calls %q refreshes %d", f.calls, tr.refreshes)
	}

	if err := newSvc(&fakeExec{}, okHost()).Rename(context.Background(), "host", "old", "a.b"); code(err) != CodeInvalid {
		t.Errorf("invalid: %v", err)
	}
	dup := &fakeExec{handler: func([]string) error { return remote(1, "duplicate session: new") }}
	if err := newSvc(dup, okHost()).Rename(context.Background(), "host", "old", "new"); code(err) != CodeDuplicate {
		t.Errorf("duplicate: %v", err)
	}
	gone := &fakeExec{handler: func([]string) error { return remote(1, "can't find session: old") }}
	if err := newSvc(gone, okHost()).Rename(context.Background(), "host", "old", "new"); code(err) != CodeNotFound {
		t.Errorf("not found: %v", err)
	}
}

func TestKill(t *testing.T) {
	f, tr := &fakeExec{}, okHost("a")
	if err := newSvc(f, tr).Kill(context.Background(), "host", "a"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.calls[0], []string{"tmux", "kill-session", "-t", "=a"}) || tr.refreshes != 1 {
		t.Fatalf("calls %q refreshes %d", f.calls, tr.refreshes)
	}
	gone := &fakeExec{handler: func([]string) error { return remote(1, "can't find session: a") }}
	if err := newSvc(gone, okHost()).Kill(context.Background(), "host", "a"); code(err) != CodeNotFound {
		t.Errorf("not found: %v", err)
	}
	if err := newSvc(&fakeExec{}, okHost()).Kill(context.Background(), "host", "a b"); code(err) != CodeInvalid {
		t.Errorf("invalid: %v", err)
	}
}

func TestNextName(t *testing.T) {
	for in, want := range map[string]string{"api": "api-1", "api-1": "api-2", "a-b-9": "a-b-10"} {
		if got := nextName(in); got != want {
			t.Errorf("nextName(%q) = %q", in, got)
		}
	}
}
