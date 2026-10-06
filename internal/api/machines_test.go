package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/machines"
	"hostbud/internal/sshx"
)

// fakeRegistry is a host plus servers added through it.
type fakeRegistry struct {
	mu      sync.Mutex
	list    []*fakeMachine
	fs      map[string]*fakeFileBrowser
	added   []machines.Server
	updated machines.Server
	err     error
	scanErr error
}

func (f *fakeRegistry) Snapshotters() []Snapshotter {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Snapshotter, len(f.list))
	for i, m := range f.list {
		out[i] = m
	}
	return out
}
func (f *fakeRegistry) FileSystemFor(id string) (FileBrowser, bool) {
	fs, ok := f.fs[id]
	return fs, ok
}
func (f *fakeRegistry) Scan(context.Context, string, int) ([]sshx.HostKey, error) {
	return []sshx.HostKey{{Type: "ssh-ed25519", Key: "AAAA", Fingerprint: "SHA256:abc"}}, f.scanErr
}
func (f *fakeRegistry) Add(_ context.Context, s machines.Server) (inventory.Machine, error) {
	if f.err != nil {
		return inventory.Machine{}, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, s)
	m := inventory.Machine{ID: "s-0011223344", Label: s.Label, Source: "custom", Status: inventory.StatusUnknown}
	f.list = append(f.list, &fakeMachine{m: m})
	f.fs[m.ID] = &fakeFileBrowser{}
	return m, nil
}
func (f *fakeRegistry) Rename(_ context.Context, id, label string) (inventory.Machine, error) {
	if f.err != nil {
		return inventory.Machine{}, f.err
	}
	return inventory.Machine{ID: id, Label: label}, nil
}
func (f *fakeRegistry) Update(_ context.Context, id string, s machines.Server) (inventory.Machine, error) {
	f.updated = s
	return inventory.Machine{ID: id, Label: s.Label, Source: "custom"}, f.err
}
func (f *fakeRegistry) Remove(_ context.Context, id string) error { return f.err }

var registryProjects *fakeProjects

func registryEnv(t *testing.T) (*fakeRegistry, http.Handler) {
	t.Helper()
	reg := &fakeRegistry{list: []*fakeMachine{host()}, fs: map[string]*fakeFileBrowser{"host": {}}}
	registryProjects = &fakeProjects{}
	h := New(Config{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Dist: fstest.MapFS{},
		Origins: AllowedOrigins("hostbud.example.com", 9055), Bus: events.NewBus(),
		Registry: reg, Sessions: &fakeService{}, Auth: &fakeAuth{}, Projects: registryProjects,
	})
	return reg, h
}

func call(t *testing.T, h http.Handler, method, path, body string) (int, string) {
	t.Helper()
	e := &env{h: h}
	rec := e.do(t, method, path, body, nil)
	return rec.Code, rec.Body.String()
}

func TestServerRoutes(t *testing.T) {
	reg, h := registryEnv(t)
	if code, body := call(t, h, http.MethodPost, "/api/machines/scan", `{"host":"server-a","port":22}`); code != http.StatusOK || !strings.Contains(body, `"fingerprint":"SHA256:abc"`) {
		t.Fatalf("scan = %d %s", code, body)
	}
	reg.scanErr = &sshx.Error{Kind: sshx.KindUnreachable, Message: "the server sent no SSH host keys", Hint: "Check the host name"}
	if code, body := call(t, h, http.MethodPost, "/api/machines/scan", `{"host":"server-a"}`); code != http.StatusBadGateway || !strings.Contains(body, "Check the host name") {
		t.Fatalf("scan failure = %d %s", code, body)
	}
	reg.scanErr = &sshx.Error{Kind: sshx.KindTimeout, Message: "late"}
	if code, _ := call(t, h, http.MethodPost, "/api/machines/scan", `{"host":"server-a"}`); code != http.StatusGatewayTimeout {
		t.Fatalf("scan timeout = %d", code)
	}

	code, body := call(t, h, http.MethodPost, "/api/machines", `{"label":"Build","host":"server-a","port":2222,"user":"dev","hostKeys":[{"type":"ssh-ed25519","key":"AAAA"}]}`)
	if code != http.StatusCreated || !strings.Contains(body, `"source":"custom"`) {
		t.Fatalf("add = %d %s", code, body)
	}
	if a := reg.added[0]; a.Label != "Build" || a.Host != "server-a" || a.Port != 2222 || a.User != "dev" || len(a.Keys) != 1 || a.Keys[0].Key != "AAAA" {
		t.Fatalf("added = %+v", a)
	}
	// The added server is listed and its routes resolve.
	if code, body := call(t, h, http.MethodGet, "/api/machines", ""); code != http.StatusOK || !strings.Contains(body, `"id":"s-0011223344"`) {
		t.Fatalf("list = %d %s", code, body)
	}
	if code, _ := call(t, h, http.MethodGet, "/api/machines/s-0011223344/fs/home", ""); code != http.StatusOK {
		t.Fatalf("server fs = %d", code)
	}
	if code, _ := call(t, h, http.MethodGet, "/api/machines/s-0011223344/sessions", ""); code != http.StatusOK {
		t.Fatalf("server sessions = %d", code)
	}
	// Every machine's projects in one list.
	if code, body := call(t, h, http.MethodGet, "/api/projects?machine=*", ""); code != http.StatusOK || body != "{\"projects\":[]}\n" || len(registryProjects.calls) != 2 {
		t.Fatalf("all projects = %d %q calls=%v", code, body, registryProjects.calls)
	}
	if code, _ := call(t, h, http.MethodGet, "/api/machines/s-missing/fs/home", ""); code != http.StatusNotFound {
		t.Fatalf("unknown fs = %d", code)
	}
	if code, body := call(t, h, http.MethodPatch, "/api/machines/s-0011223344", `{"label":"CI"}`); code != http.StatusOK || !strings.Contains(body, `"label":"CI"`) {
		t.Fatalf("rename = %d %s", code, body)
	}
	if code, body := call(t, h, http.MethodPatch, "/api/machines/s-0011223344", `{"label":"CI","host":"server-b","port":2222,"user":"ops","hostKeys":[{"type":"ssh-ed25519","key":"BBBB"}]}`); code != http.StatusOK || !strings.Contains(body, `"label":"CI"`) {
		t.Fatalf("update server = %d %s", code, body)
	}
	if reg.updated.Host != "server-b" || reg.updated.Port != 2222 || reg.updated.User != "ops" || len(reg.updated.Keys) != 1 || reg.updated.Keys[0].Key != "BBBB" {
		t.Fatalf("updated server = %+v", reg.updated)
	}
	if code, _ := call(t, h, http.MethodDelete, "/api/machines/s-0011223344", ""); code != http.StatusNoContent {
		t.Fatalf("remove = %d", code)
	}

	for _, tc := range []struct {
		err    error
		status int
	}{
		{&machines.Error{Code: machines.CodeInvalid, Message: "invalid host"}, http.StatusBadRequest},
		{&machines.Error{Code: machines.CodeNotFound, Message: "no such server"}, http.StatusNotFound},
		{&machines.Error{Code: machines.CodeInUse, Message: "projects still use this server", Hint: "Remove the server's projects"}, http.StatusConflict},
		{&machines.Error{Code: machines.CodeConflict, Message: "dup"}, http.StatusConflict},
	} {
		reg.err = tc.err
		if code, body := call(t, h, http.MethodDelete, "/api/machines/s-0011223344", ""); code != tc.status || !strings.Contains(body, tc.err.Error()) {
			t.Errorf("%v: %d %s", tc.err, code, body)
		}
	}
	reg.err = context.DeadlineExceeded
	if code, body := call(t, h, http.MethodPost, "/api/machines", `{"label":"x"}`); code != http.StatusInternalServerError || strings.Contains(body, "deadline") {
		t.Fatalf("internal = %d %s", code, body)
	}
	if code, _ := call(t, h, http.MethodPost, "/api/machines", `{"label":"x","password":"nope"}`); code != http.StatusBadRequest {
		t.Fatalf("unknown field = %d", code)
	}
}

func TestServerRoutesWithoutRegistry(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(t, http.MethodPost, "/api/machines", `{"label":"x"}`, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("no registry = %d", rec.Code)
	}
}
