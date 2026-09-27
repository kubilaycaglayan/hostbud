package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"hostbud/internal/auth"
	"hostbud/internal/events"
	"hostbud/internal/fsbrowse"
	"hostbud/internal/inventory"
	"hostbud/internal/projects"
	"hostbud/internal/session"
	"hostbud/internal/store"
	"hostbud/internal/tmux"
)

const origin = "http://localhost:9055"

type placementResolverFake struct{ projectID string }

func (f placementResolverFake) List(context.Context, string) ([]store.Project, error) {
	return nil, nil
}
func (f placementResolverFake) RecentCommands(context.Context, string) ([]store.RecentCommand, error) {
	return nil, nil
}
func (f placementResolverFake) Get(context.Context, string) (store.Project, error) {
	return store.Project{}, store.ErrNotFound
}
func (f placementResolverFake) Create(context.Context, string, string, string) (store.Project, error) {
	return store.Project{}, nil
}
func (f placementResolverFake) Rename(context.Context, string, string) (store.Project, error) {
	return store.Project{}, nil
}
func (f placementResolverFake) Delete(context.Context, string) error { return nil }
func (f placementResolverFake) CreateSession(context.Context, string, session.Spec) (string, error) {
	return "", nil
}
func (f placementResolverFake) Place(_ context.Context, machine, _ string, _ string) (projects.Placement, error) {
	return projects.Placement{MachineID: machine, ProjectID: f.projectID, Matched: f.projectID != ""}, nil
}

func TestWithProjectPlacementAddsExplicitPlacement(t *testing.T) {
	s := &server{cfg: Config{Projects: placementResolverFake{projectID: "linked"}, Log: slog.New(slog.DiscardHandler)}}
	sessions := []tmux.Session{{Name: "work", Path: "/work/app"}}
	got := s.withProjectPlacement(context.Background(), "host", sessions)
	if len(got) != 1 || got[0].ProjectID != "linked" {
		t.Fatalf("placed sessions = %+v", got)
	}
	if sessions[0].ProjectID != "" {
		t.Fatal("input snapshot was mutated")
	}
}

type fakeMachine struct {
	mu       sync.Mutex
	m        inventory.Machine
	sessions []tmux.Session
}

func (f *fakeMachine) Snapshot() (inventory.Machine, []tmux.Session) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.m, append([]tmux.Session{}, f.sessions...)
}

func (f *fakeMachine) Refresh(context.Context) error { return nil }

func host(sessions ...string) *fakeMachine {
	f := &fakeMachine{m: inventory.Machine{
		ID: "host", Label: "Host machine", Status: inventory.StatusOK,
		Capabilities: inventory.Capabilities{Home: "/home/dev", TmuxVersion: "3.4"},
	}}
	for _, s := range sessions {
		f.sessions = append(f.sessions, tmux.Session{Name: s, Windows: 1})
	}
	return f
}

// fakeService records calls and returns err.
type fakeService struct {
	calls []string
	err   error
}

func (f *fakeService) Create(_ context.Context, s session.Spec) (string, error) {
	f.calls = append(f.calls, "create "+s.Machine+" "+s.Name+" "+s.Path+" "+s.StartCommand)
	if s.Name == "" {
		s.Name = "dev"
	}
	return s.Name, f.err
}

func (f *fakeService) Rename(_ context.Context, m, from, to string) error {
	f.calls = append(f.calls, "rename "+m+" "+from+" "+to)
	return f.err
}

func (f *fakeService) Kill(_ context.Context, m, name string) error {
	f.calls = append(f.calls, "kill "+m+" "+name)
	return f.err
}

func (f *fakeService) CopyMode(_ context.Context, m, name string, action tmux.CopyAction, lines int) (session.CopyModeState, error) {
	f.calls = append(f.calls, "copy "+m+" "+name+" "+string(action)+" "+strconv.Itoa(lines))
	return session.CopyModeState{InMode: true, ScrollPosition: 4, HistorySize: 10}, f.err
}

func (f *fakeService) ListWindows(_ context.Context, m, name string) (session.WindowsState, error) {
	f.calls = append(f.calls, "windows "+m+" "+name)
	return session.WindowsState{Windows: []tmux.Window{{ID: "@1", Index: 0, Name: "shell", Active: true, Panes: []tmux.Pane{{ID: "%1", Index: 0, Active: true, Command: "bash", Width: 80, Height: 24}}}}}, f.err
}

func (f *fakeService) SelectWindow(_ context.Context, m, name, window, pane string) (session.WindowsState, error) {
	f.calls = append(f.calls, "select "+m+" "+name+" "+window+" "+pane)
	return session.WindowsState{Windows: []tmux.Window{{ID: window, Index: 0, Active: true}}}, f.err
}

const testToken = "test-session-token"

// otherToken signs in a second account (u2).
const otherToken = "other-session-token"

// fakeAuth accepts testToken; Login succeeds for the password "good-password".
type fakeAuth struct {
	registered []string
	loggedOut  []string
	err        error
}

func (f *fakeAuth) Register(_ context.Context, email, _, ip string) error {
	f.registered = append(f.registered, email+" "+ip)
	return f.err
}

func (f *fakeAuth) Login(_ context.Context, _, password, _, _, _ string) (string, time.Time, error) {
	if f.err != nil {
		return "", time.Time{}, f.err
	}
	if password != "good-password" {
		return "", time.Time{}, auth.ErrInvalidCredentials
	}
	return "new-token", time.Now().Add(time.Hour), nil
}

func (f *fakeAuth) Logout(_ context.Context, token string) error {
	f.loggedOut = append(f.loggedOut, token)
	return nil
}

func (f *fakeAuth) Authenticate(_ context.Context, token string) (store.User, error) {
	if token == testToken || token == "new-token" {
		return store.User{ID: "u1", Email: "Person@example.com"}, nil
	}
	if token == otherToken {
		return store.User{ID: "u2", Email: "other@example.com"}, nil
	}
	return store.User{}, auth.ErrUnauthenticated
}

type env struct {
	h        http.Handler
	svc      *fakeService
	bus      *events.Bus
	m        *fakeMachine
	ui       *fakeUIState
	fs       *fakeFileBrowser
	projects *fakeProjects
}

type fakeFileBrowser struct {
	calls []string
	err   error
}

func (f *fakeFileBrowser) Home(context.Context) (string, error) {
	f.calls = append(f.calls, "home")
	return "/home/dev", f.err
}
func (f *fakeFileBrowser) List(_ context.Context, p string, hidden bool) (string, []fsbrowse.Entry, error) {
	f.calls = append(f.calls, "list "+p+" "+strconv.FormatBool(hidden))
	return p, []fsbrowse.Entry{{Name: "docs", Path: p + "/docs", Kind: "directory"}}, f.err
}
func (f *fakeFileBrowser) Stat(_ context.Context, p string) (fsbrowse.StatResult, error) {
	f.calls = append(f.calls, "stat "+p)
	return fsbrowse.StatResult{Entry: fsbrowse.Entry{Name: "docs", Path: p, Kind: "directory"}}, f.err
}
func (f *fakeFileBrowser) Mkdir(_ context.Context, p, name string) (string, error) {
	f.calls = append(f.calls, "mkdir "+p+" "+name)
	return p + "/" + name, f.err
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{svc: &fakeService{}, bus: events.NewBus(), m: host("a"), ui: &fakeUIState{}, fs: &fakeFileBrowser{}}
	e.projects = &fakeProjects{}
	e.h = New(Config{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Dist: fstest.MapFS{},
		Origins: AllowedOrigins("hostbud.example.com", 9055), Bus: e.bus,
		Machines: []Snapshotter{e.m}, Sessions: e.svc, Auth: &fakeAuth{}, UIState: e.ui, FileSystem: e.fs, Projects: e.projects,
	})
	return e
}

func (e *env) do(t *testing.T, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if _, ok := hdr["Origin"]; !ok && method != http.MethodGet {
		req.Header.Set("Origin", origin)
	}
	if _, ok := hdr["Cookie"]; !ok {
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testToken})
	}
	for k, v := range hdr {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestHealth(t *testing.T) {
	e := newEnv(t)
	rec := e.do(t, http.MethodGet, "/api/health", "", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" ||
		strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("%d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	if rec := e.do(t, http.MethodPost, "/api/health", "", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST health = %d", rec.Code)
	}
}

func TestHealthReportsDatabaseAndNoStore(t *testing.T) {
	ready := false
	h := New(Config{Dist: fstest.MapFS{}, DBPing: func(context.Context) error {
		if !ready {
			return context.DeadlineExceeded
		}
		return nil
	}})
	for _, tc := range []struct {
		ready  bool
		status int
		body   string
	}{
		{false, http.StatusServiceUnavailable, `{"db":"unreachable","status":"degraded"}`},
		{true, http.StatusOK, `{"status":"ok"}`},
	} {
		ready = tc.ready
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/health", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.status || strings.TrimSpace(w.Body.String()) != tc.body || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("health ready=%v: status=%d body=%s cache=%q", ready, w.Code, w.Body, w.Header().Get("Cache-Control"))
		}
	}
}

func TestRequestLimitsJSONBodyAndDeadline(t *testing.T) {
	e := newEnv(t)
	large := `{"name":"` + strings.Repeat("x", 65<<10) + `"}`
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
	}{
		{"too large", large, "application/json", http.StatusRequestEntityTooLarge},
		{"unknown field", `{"unknown":true}`, "application/json", http.StatusBadRequest},
		{"wrong media type", `{}`, "text/plain", http.StatusUnsupportedMediaType},
	} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/machines/host/sessions", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", tc.contentType)
		req.Header.Set("Origin", origin)
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testToken})
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Errorf("%s: status=%d body=%s", tc.name, w.Code, w.Body)
		}
	}
	finished := make(chan struct{})
	h := requestLimits(Config{RequestTimeout: 20 * time.Millisecond}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(finished)
		w.WriteHeader(http.StatusNoContent)
	}))
	start := time.Now()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/slow", nil))
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("handler context did not expire")
	}
	if time.Since(start) > time.Second {
		t.Fatal("request deadline exceeded test bound")
	}
}

func TestHTTPServerRejectsHeadersOver32KiB(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	srv.Config.MaxHeaderBytes = 32 << 10
	srv.Start()
	defer srv.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Hostbud-Large", strings.Repeat("x", 40<<10))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("header status=%d", resp.StatusCode)
	}
}

func TestRuntimeLimitsIsAuthenticatedAndReportsExecDeadline(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(t, http.MethodGet, "/api/runtime/limits", "", nil); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"execTimeoutMs":10000,"sftpTimeoutMs":10000}` {
		t.Fatalf("runtime limits: %d %s", rec.Code, rec.Body)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/runtime/limits", nil)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous runtime limits: %d", rec.Code)
	}
}

func TestListMachinesAndSessions(t *testing.T) {
	e := newEnv(t)
	rec := e.do(t, http.MethodGet, "/api/machines", "", nil)
	ms := decodeBody[struct{ Machines []inventory.Machine }](t, rec)
	if rec.Code != 200 || len(ms.Machines) != 1 || ms.Machines[0].ID != "host" || ms.Machines[0].Status != inventory.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = e.do(t, http.MethodGet, "/api/machines/host/sessions", "", nil)
	ss := decodeBody[struct{ Sessions []tmux.Session }](t, rec)
	if rec.Code != 200 || len(ss.Sessions) != 1 || ss.Sessions[0].Name != "a" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"attached":0`) || !strings.Contains(rec.Body.String(), `"windows":1`) {
		t.Fatalf("session JSON: %s", rec.Body)
	}
	if rec := e.do(t, http.MethodGet, "/api/machines/server-a/sessions", "", nil); rec.Code != 404 {
		t.Fatalf("unknown machine = %d", rec.Code)
	}
}

func TestEmptySessionListIsArray(t *testing.T) {
	e := newEnv(t)
	e.m.sessions = nil
	rec := e.do(t, http.MethodGet, "/api/machines/host/sessions", "", nil)
	if strings.TrimSpace(rec.Body.String()) != `{"sessions":[]}` {
		t.Fatalf("body %s", rec.Body)
	}
}

func TestEmptySessionListWithProjectPlacementIsArray(t *testing.T) {
	e := newEnv(t)
	e.m.sessions = nil
	s := &server{
		cfg:      Config{Projects: placementResolverFake{}, Log: slog.New(slog.DiscardHandler)},
		machines: map[string]Snapshotter{"host": e.m},
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/machines/host/sessions", nil)
	req.SetPathValue("machine", "host")
	rec := httptest.NewRecorder()
	s.listSessions(rec, req)
	if got := strings.TrimSpace(rec.Body.String()); got != `{"sessions":[]}` {
		t.Fatalf("body %s", got)
	}
}

func TestFilesystemAPI(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(t, http.MethodGet, "/api/machines/host/fs/home", "", nil); rec.Code != http.StatusOK ||
		decodeBody[map[string]string](t, rec)["path"] != "/home/dev" {
		t.Fatalf("home: %d %s", rec.Code, rec.Body)
	}
	rec := e.do(t, http.MethodGet, "/api/machines/host/fs?path=~/docs%20one&hidden=true", "", nil)
	listing := decodeBody[struct {
		Path    string           `json:"path"`
		Entries []fsbrowse.Entry `json:"entries"`
	}](t, rec)
	if rec.Code != http.StatusOK || listing.Path != "~/docs one" || len(listing.Entries) != 1 || listing.Entries[0].Name != "docs" {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	rec = e.do(t, http.MethodGet, "/api/machines/host/fs/stat?path=%2Fhome%2Fdev%2Fdocs", "", nil)
	if rec.Code != http.StatusOK || decodeBody[fsbrowse.StatResult](t, rec).Kind != "directory" {
		t.Fatalf("stat: %d %s", rec.Code, rec.Body)
	}
	rec = e.do(t, http.MethodPost, "/api/machines/host/fs/mkdir", `{"path":"/home/dev","name":"new folder"}`, nil)
	if rec.Code != http.StatusCreated || decodeBody[map[string]string](t, rec)["path"] != "/home/dev/new folder" {
		t.Fatalf("mkdir: %d %s", rec.Code, rec.Body)
	}
	want := []string{"home", "list ~/docs one true", "stat /home/dev/docs", "mkdir /home/dev new folder"}
	if !slices.Equal(e.fs.calls, want) {
		t.Fatalf("filesystem calls = %q, want %q", e.fs.calls, want)
	}
}

func TestFilesystemAPIAccessControlAndValidation(t *testing.T) {
	e := newEnv(t)
	unauth := e.do(t, http.MethodGet, "/api/machines/host/fs/home", "", map[string]string{"Cookie": ""})
	if unauth.Code != http.StatusUnauthorized || len(e.fs.calls) != 0 {
		t.Fatalf("unauthenticated call: %d %q", unauth.Code, e.fs.calls)
	}
	foreign := e.do(t, http.MethodPost, "/api/machines/host/fs/mkdir", `{"path":"/home/dev","name":"new"}`, map[string]string{"Origin": "http://evil.example.com"})
	if foreign.Code != http.StatusForbidden || len(e.fs.calls) != 0 {
		t.Fatalf("foreign origin: %d %q", foreign.Code, e.fs.calls)
	}
	for _, path := range []string{
		"/api/machines/server-a/fs/home",
		"/api/machines/host/fs?path=%00",
		"/api/machines/host/fs?hidden=perhaps",
		"/api/machines/host/fs?hidden=1",
		"/api/machines/host/fs?path=%2Fhome%2Fdev&unknown=1",
		"/api/machines/host/fs?path=%2Fone&path=%2Ftwo",
		"/api/machines/host/fs/stat",
		"/api/machines/host/fs/home?unknown=1",
		"/api/machines/host/fs?path=" + strings.Repeat("a", fsbrowse.MaxPathBytes+1),
	} {
		rec := e.do(t, http.MethodGet, path, "", nil)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, body %s", path, rec.Code, rec.Body)
		}
	}
	for _, req := range []struct{ method, path string }{
		{http.MethodDelete, "/api/machines/host/fs?path=%2Fhome%2Fdev"},
		{http.MethodPatch, "/api/machines/host/fs?path=%2Fhome%2Fdev"},
	} {
		if rec := e.do(t, req.method, req.path, "", nil); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want 405", req.method, req.path, rec.Code)
		}
	}
	if rec := e.do(t, http.MethodGet, "/api/machines/host/fs/mkdir", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("GET mkdir endpoint = %d, want 404", rec.Code)
	}
	for _, body := range []string{`{"name":"missing-path"}`, `{"path":"/home/dev","name":"x","extra":true}`} {
		if rec := e.do(t, http.MethodPost, "/api/machines/host/fs/mkdir", body, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("POST mkdir body %s = %d", body, rec.Code)
		}
	}
	if len(e.fs.calls) != 0 {
		t.Fatalf("invalid, unknown or unsupported request reached SFTP: %q", e.fs.calls)
	}
}

func TestFilesystemErrorsAreActionable(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
	}{{fsbrowse.ErrInvalidName, http.StatusBadRequest},
		{fsbrowse.ErrAlreadyExists, http.StatusConflict},
		{&fsbrowse.Error{Op: "list directory", Err: os.ErrPermission}, http.StatusForbidden},
		{fsbrowse.ErrTooManyEntries, http.StatusRequestEntityTooLarge},
		{fsbrowse.ErrNotDirectory, http.StatusBadRequest},
		{os.ErrNotExist, http.StatusNotFound},
		{context.DeadlineExceeded, http.StatusGatewayTimeout}} {
		e := newEnv(t)
		e.fs.err = tt.err
		rec := e.do(t, http.MethodPost, "/api/machines/host/fs/mkdir", `{"path":"/home/dev","name":"folder"}`, nil)
		body := decodeBody[errorBody](t, rec)
		if rec.Code != tt.status || body.Error == "" || body.Hint == "" {
			t.Fatalf("error response: %d %s", rec.Code, rec.Body)
		}
	}
}

func TestSFTPTimeoutIncludesConfiguredDurationAndHint(t *testing.T) {
	e := newEnv(t)
	e.fs.err = &fsbrowse.Error{Op: "list directory", Err: context.DeadlineExceeded, Timeout: 3 * time.Second}
	rec := e.do(t, http.MethodGet, "/api/machines/host/fs?path=~", "", nil)
	body := decodeBody[errorBody](t, rec)
	if rec.Code != http.StatusGatewayTimeout || body.Error != "The host's file service didn't answer within 3s" || !strings.Contains(body.Hint, "ssh <host> -s sftp") {
		t.Fatalf("SFTP timeout response: %d %+v", rec.Code, body)
	}
}

func TestMutations(t *testing.T) {
	e := newEnv(t)
	rec := e.do(t, http.MethodPost, "/api/machines/host/sessions", `{"path":"~/app","startCommand":"htop"}`, nil)
	if rec.Code != http.StatusCreated || decodeBody[map[string]string](t, rec)["name"] != "dev" {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	rec = e.do(t, http.MethodPatch, "/api/machines/host/sessions/a", `{"name":"b"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	rec = e.do(t, http.MethodDelete, "/api/machines/host/sessions/b", "", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("kill: %d %s", rec.Code, rec.Body)
	}
	want := []string{"create host  ~/app htop", "rename host a b", "kill host b"}
	if strings.Join(e.svc.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %q", e.svc.calls)
	}
}

func TestCopyModeAPIValidationAndAccess(t *testing.T) {
	e := newEnv(t)
	res := e.do(t, http.MethodPost, "/api/machines/host/sessions/a/copy-mode", `{"action":"page-up"}`, nil)
	if res.Code != 200 || decodeBody[session.CopyModeState](t, res).ScrollPosition != 4 {
		t.Fatalf("copy-mode: %d %s", res.Code, res.Body)
	}
	if got := e.svc.calls[len(e.svc.calls)-1]; got != "copy host a page-up 0" {
		t.Fatalf("service call %q", got)
	}
	for _, c := range []struct {
		path, body string
		status     int
	}{
		{"/api/machines/host/sessions/a.b/copy-mode", `{"action":"enter"}`, 400},
		{"/api/machines/host/sessions/a/copy-mode", `{"action":"wat"}`, 400},
		{"/api/machines/host/sessions/a/copy-mode", `{"action":"enter","lines":2}`, 400},
		{"/api/machines/host/sessions/a/copy-mode", `{"action":"scroll-up","lines":501}`, 400},
		{"/api/machines/nope/sessions/a/copy-mode", `{"action":"enter"}`, 404},
	} {
		if got := e.do(t, http.MethodPost, c.path, c.body, nil).Code; got != c.status {
			t.Errorf("%s = %d, want %d", c.path, got, c.status)
		}
	}
	if got := e.do(t, http.MethodPost, "/api/machines/host/sessions/a/copy-mode", `{"action":"enter"}`, map[string]string{"Cookie": ""}).Code; got != 401 {
		t.Errorf("signed out = %d", got)
	}
	if got := e.do(t, http.MethodPost, "/api/machines/host/sessions/a/copy-mode", `{"action":"enter"}`, map[string]string{"Origin": "http://evil.example.com"}).Code; got != 403 {
		t.Errorf("foreign origin = %d", got)
	}
	e.svc.err = &session.Error{Code: session.CodeTmuxVersion, Message: "needs tmux 2.4", Hint: "upgrade tmux"}
	if got := e.do(t, http.MethodPost, "/api/machines/host/sessions/a/copy-mode", `{"action":"enter"}`, nil).Code; got != 409 {
		t.Errorf("old tmux status = %d", got)
	}
}

func TestWindowsAPIValidationAndAccess(t *testing.T) {
	e := newEnv(t)
	res := e.do(t, http.MethodGet, "/api/machines/host/sessions/work/windows", "", nil)
	if res.Code != 200 {
		t.Fatalf("list windows: %d %s", res.Code, res.Body)
	}
	state := decodeBody[session.WindowsState](t, res)
	if len(state.Windows) != 1 || state.Windows[0].Panes[0].Command != "bash" {
		t.Fatalf("windows response: %+v", state)
	}
	res = e.do(t, http.MethodPost, "/api/machines/host/sessions/work/select", `{"window":"@1","pane":"%1"}`, map[string]string{"Origin": origin})
	if res.Code != 200 {
		t.Fatalf("select window/pane: %d %s", res.Code, res.Body)
	}
	if got := e.svc.calls[len(e.svc.calls)-1]; got != "select host work @1 %1" {
		t.Fatalf("select service call = %q", got)
	}
	if got := e.do(t, http.MethodPost, "/api/machines/host/sessions/work/select", `{"window":"@1"}`, map[string]string{"Origin": "https://hostbud.example.com"}).Code; got != 200 {
		t.Errorf("allowed domain Origin = %d", got)
	}
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/api/machines/host/sessions/bad.name/windows", "", 400},
		{http.MethodPost, "/api/machines/host/sessions/work/select", `{"window":"@"}`, 400},
		{http.MethodPost, "/api/machines/host/sessions/work/select", `{"window":"@1","extra":true}`, 400},
		{http.MethodGet, "/api/machines/nope/sessions/work/windows", "", 404},
		{http.MethodPost, "/api/machines/nope/sessions/work/select", `{"window":"@1"}`, 404},
		{http.MethodGet, "/api/machines/host/sessions/work/select", "", 405},
		{http.MethodPost, "/api/machines/host/sessions/work/windows", "", 405},
	} {
		if got := e.do(t, tc.method, tc.path, tc.body, nil).Code; got != tc.status {
			t.Errorf("%s %s = %d; want %d", tc.method, tc.path, got, tc.status)
		}
	}
	if got := e.do(t, http.MethodGet, "/api/machines/host/sessions/work/windows", "", map[string]string{"Cookie": ""}).Code; got != 401 {
		t.Errorf("signed out list = %d", got)
	}
	if got := e.do(t, http.MethodPost, "/api/machines/host/sessions/work/select", `{"window":"@1"}`, map[string]string{"Origin": "http://evil.example.com"}).Code; got != 403 {
		t.Errorf("foreign Origin = %d", got)
	}
	e.svc.err = &session.Error{Code: session.CodeNotFound, Message: "missing"}
	if got := e.do(t, http.MethodGet, "/api/machines/host/sessions/missing/windows", "", nil).Code; got != 404 {
		t.Errorf("unknown session = %d", got)
	}
	if got := e.do(t, http.MethodPost, "/api/machines/host/sessions/work/select", `{"window":"@1"}`, nil).Code; got != 404 {
		t.Errorf("selection not found = %d", got)
	}
}

func TestValidation400(t *testing.T) {
	e := newEnv(t)
	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/api/machines/host/sessions", `{"name":"a.b"}`},
		{http.MethodPost, "/api/machines/host/sessions", `{"name":"a:b"}`},
		{http.MethodPost, "/api/machines/host/sessions", `{"name":"a b"}`},
		{http.MethodPost, "/api/machines/host/sessions", `{"nope":1}`},
		{http.MethodPost, "/api/machines/host/sessions", `not json`},
		{http.MethodPatch, "/api/machines/host/sessions/a", `{"name":"x.y"}`},
		{http.MethodPatch, "/api/machines/host/sessions/a.b", `{"name":"ok"}`},
		{http.MethodDelete, "/api/machines/host/sessions/a.b", ``},
	}
	for _, c := range cases {
		rec := e.do(t, c.method, c.path, c.body, nil)
		body := decodeBody[errorBody](t, rec)
		if rec.Code != http.StatusBadRequest || body.Error == "" || body.Hint == "" {
			t.Errorf("%s %s %s: %d %s", c.method, c.path, c.body, rec.Code, rec.Body)
		}
	}
	if len(e.svc.calls) != 0 {
		t.Fatalf("invalid input reached the service: %q", e.svc.calls)
	}
}

func TestSessionErrorsMapToStatus(t *testing.T) {
	cases := map[session.Code]int{
		session.CodeDuplicate:    409,
		session.CodePathNotFound: 400,
		session.CodeNotFound:     404,
		session.CodeTmuxMissing:  503,
		session.CodeUnavailable:  503,
		session.CodeTimeout:      504,
		session.CodeInternal:     500,
	}
	for code, status := range cases {
		e := newEnv(t)
		e.svc.err = &session.Error{Code: code, Message: "msg " + string(code), Hint: "hint"}
		rec := e.do(t, http.MethodPost, "/api/machines/host/sessions", `{"name":"x"}`, nil)
		body := decodeBody[errorBody](t, rec)
		if rec.Code != status || body.Error != "msg "+string(code) || body.Hint != "hint" {
			t.Errorf("%s: %d %s", code, rec.Code, rec.Body)
		}
	}
}

func TestTmuxBackedRoutesMapTimeoutTo504(t *testing.T) {
	cases := []struct{ method, path, body string }{
		{http.MethodPost, "/api/machines/host/sessions", `{"name":"new"}`},
		{http.MethodPatch, "/api/machines/host/sessions/a", `{"name":"b"}`},
		{http.MethodDelete, "/api/machines/host/sessions/a", ""},
		{http.MethodPost, "/api/machines/host/sessions/a/copy-mode", `{"action":"enter"}`},
		{http.MethodGet, "/api/machines/host/sessions/a/windows", ""},
		{http.MethodPost, "/api/machines/host/sessions/a/select", `{"window":"@1"}`},
		{http.MethodPost, "/api/projects/project-a/sessions", `{"name":"new"}`},
	}
	for _, tc := range cases {
		e := newEnv(t)
		err := &session.Error{Code: session.CodeTimeout, Message: "The host didn't answer within 10s", Hint: "hostbud will retry"}
		e.svc.err, e.projects.err = err, err
		rec := e.do(t, tc.method, tc.path, tc.body, nil)
		body := decodeBody[errorBody](t, rec)
		if rec.Code != http.StatusGatewayTimeout || body.Error != err.Message || body.Hint != err.Hint {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, rec.Code, rec.Body)
		}
	}
}

func TestOriginOnStateChangingRequests(t *testing.T) {
	cases := []struct {
		origin string
		ok     bool
	}{
		{"http://localhost:9055", true},
		{"https://hostbud.example.com", true},
		{"http://hostbud.example.com", false},       // domain without TLS
		{"https://hostbud.example.com:8443", false}, // other port
		{"https://hostbud.example.com.evil.example.com", false},
		{"https://HOSTBUD.example.com", false}, // browsers send it lowercased
		{"http://evil.example.com", false},
		{"http://localhost:9056", false},
		{"http://127.0.0.1:9055", false},
		{"null", false},
		{"", false}, // missing
	}
	for _, c := range cases {
		e := newEnv(t)
		for _, req := range []struct{ method, path, body string }{
			{http.MethodPost, "/api/machines/host/sessions", `{"name":"x"}`},
			{http.MethodPatch, "/api/machines/host/sessions/a", `{"name":"b"}`},
			{http.MethodDelete, "/api/machines/host/sessions/a", ``},
		} {
			rec := e.do(t, req.method, req.path, req.body, map[string]string{"Origin": c.origin})
			if c.ok && rec.Code == http.StatusForbidden || !c.ok && rec.Code != http.StatusForbidden {
				t.Errorf("origin %q %s: %d", c.origin, req.method, rec.Code)
			}
		}
		if !c.ok && len(e.svc.calls) != 0 {
			t.Errorf("origin %q reached the service", c.origin)
		}
	}
	// GETs don't need an Origin.
	e := newEnv(t)
	if rec := e.do(t, http.MethodGet, "/api/machines", "", nil); rec.Code != 200 {
		t.Fatalf("GET without Origin: %d", rec.Code)
	}
}

func TestAllowedOrigins(t *testing.T) {
	if got := AllowedOrigins("hostbud.example.com", 9055); !slices.Equal(got,
		[]string{"http://localhost:9055", "https://hostbud.example.com"}) {
		t.Errorf("with a domain: %v", got)
	}
	if got := AllowedOrigins("", 9056); !slices.Equal(got, []string{"http://localhost:9056"}) {
		t.Errorf("without a domain: %v", got)
	}
}

func TestEventsSocketOrigin(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	// Both access paths may upgrade: the port forward and the domain.
	for _, o := range []string{"http://localhost:9055", "https://hostbud.example.com"} {
		hdr := http.Header{"Cookie": {SessionCookie + "=" + testToken}, "Origin": {o}}
		c, resp, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: hdr})
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			t.Fatalf("origin %q: upgrade refused: %v", o, err)
		}
		_ = c.CloseNow()
	}
	for _, o := range []string{"http://evil.example.com", "http://hostbud.example.com", "https://hostbud.example.com:8443", ""} {
		hdr := http.Header{"Cookie": {SessionCookie + "=" + testToken}}
		if o != "" {
			hdr.Set("Origin", o)
		}
		c, resp, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: hdr})
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err == nil {
			_ = c.CloseNow()
			t.Fatalf("origin %q: upgrade accepted", o)
		}
		if resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: %v", o, resp)
		}
	}
}

func TestEventsSocketSnapshotThenEvents(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{
		"Origin": {origin}, "Cookie": {SessionCookie + "=" + testToken}}})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()

	var snap struct {
		Type     string
		Machines []inventory.Machine
		Sessions map[string][]tmux.Session
	}
	if err := wsjson.Read(ctx, c, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Type != "snapshot" || len(snap.Machines) != 1 || len(snap.Sessions["host"]) != 1 {
		t.Fatalf("snapshot: %+v", snap)
	}

	for e.bus.Subscribers() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	e.bus.Publish(events.Event{Type: events.SessionsChanged, Machine: "host",
		Payload: inventory.SessionsChanged{Sessions: []tmux.Session{{Name: "new"}}}})
	var ev struct {
		Type    string
		Machine string
		Payload inventory.SessionsChanged
	}
	if err := wsjson.Read(ctx, c, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Type != "sessions.changed" || ev.Machine != "host" || len(ev.Payload.Sessions) != 1 || ev.Payload.Sessions[0].Name != "new" {
		t.Fatalf("event: %+v", ev)
	}
}

func TestEventsSocketCapIsPerAccount(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	header := http.Header{"Origin": {origin}, "Cookie": {SessionCookie + "=" + testToken}}
	conns := make([]*websocket.Conn, 0, 16)
	for i := 0; i < 16; i++ {
		c, resp, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: header})
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			t.Fatalf("socket %d: %v", i, err)
		}
		conns = append(conns, c)
	}
	_, resp, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: header})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err == nil || resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("17th socket: resp=%v err=%v", resp, err)
	}
	_ = conns[0].CloseNow()
	var c *websocket.Conn
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, resp, err = websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: header})
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("socket after close: %v", err)
	}
	_ = c.CloseNow()
	for _, conn := range conns[1:] {
		_ = conn.CloseNow()
	}
}

func TestEventsSocketRejectsClientData(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	c, resp, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: http.Header{
		"Origin": {origin}, "Cookie": {SessionCookie + "=" + testToken},
	}})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	var initial map[string]any
	if err := wsjson.Read(ctx, c, &initial); err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageText, []byte("not allowed")); err != nil {
		t.Fatal(err)
	}
	_, _, err = c.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusUnsupportedData {
		t.Fatalf("data frame close=%v status=%d", err, websocket.CloseStatus(err))
	}
}

func TestTerminalSocketIsOriginChecked(t *testing.T) {
	reached := 0
	h := New(Config{Dist: fstest.MapFS{}, Origins: AllowedOrigins("", 9055), Bus: events.NewBus(), Auth: &fakeAuth{},
		Terminal: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached++; w.WriteHeader(http.StatusTeapot) })})
	e := &env{h: h}
	up := map[string]string{"Upgrade": "websocket", "Connection": "Upgrade"}
	for _, o := range []string{"http://evil.example.com", ""} {
		hdr := map[string]string{"Origin": o}
		for k, v := range up {
			hdr[k] = v
		}
		if rec := e.do(t, http.MethodGet, "/ws/term?machine=host&session=a", "", hdr); rec.Code != http.StatusForbidden {
			t.Fatalf("origin %q: %d", o, rec.Code)
		}
	}
	hdr := map[string]string{"Origin": origin}
	for k, v := range up {
		hdr[k] = v
	}
	if rec := e.do(t, http.MethodGet, "/ws/term?machine=host&session=a", "", hdr); rec.Code != http.StatusTeapot || reached != 1 {
		t.Fatalf("allowed origin: %d reached=%d", rec.Code, reached)
	}
}

// Info-level logs of real create/rename/kill requests never contain the
// path or start command (they may hold user paths and secrets).
func TestInfoLogsHoldNoPathOrCommand(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	m := host()
	svc := session.New(nopExec{}, map[string]session.Tracker{"host": m}, log)
	h := New(Config{Log: log, Dist: fstest.MapFS{}, Origins: AllowedOrigins("", 9055),
		Bus: events.NewBus(), Machines: []Snapshotter{m}, Sessions: svc, Auth: &fakeAuth{}})

	e := &env{h: h}
	markPath, markCmd := "~/private-project-dir", "run-deploy --marker=zq7"
	if rec := e.do(t, http.MethodPost, "/api/machines/host/sessions",
		`{"name":"logs-a","path":"`+markPath+`","startCommand":"`+markCmd+`"}`, nil); rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	e.do(t, http.MethodPatch, "/api/machines/host/sessions/logs-a", `{"name":"logs-b"}`, nil)
	e.do(t, http.MethodDelete, "/api/machines/host/sessions/logs-b", "", nil)

	logs := buf.String()
	if !strings.Contains(logs, "session created") || !strings.Contains(logs, "session killed") {
		t.Fatalf("expected info logs, got:\n%s", logs)
	}
	for _, mark := range []string{"private-project-dir", "zq7", "run-deploy", "/home/dev"} {
		if strings.Contains(logs, mark) {
			t.Fatalf("info logs contain %q:\n%s", mark, logs)
		}
	}
}

type nopExec struct{}

func (nopExec) Exec(context.Context, string, ...string) ([]byte, error) { return nil, nil }

func TestEventsSocketHeartbeat(t *testing.T) {
	e := newEnv(t)
	e.h = New(Config{
		Dist: fstest.MapFS{}, Origins: AllowedOrigins("", 9055), Bus: e.bus,
		Machines: []Snapshotter{e.m}, Sessions: e.svc, Auth: &fakeAuth{},
		Heartbeat: 20 * time.Millisecond,
	})
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{
		"Origin": {origin}, "Cookie": {SessionCookie + "=" + testToken}}})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()

	var types []string
	for len(types) < 3 {
		var msg struct{ Type string }
		if err := wsjson.Read(ctx, c, &msg); err != nil {
			t.Fatal(err)
		}
		types = append(types, msg.Type)
	}
	if !slices.Equal(types, []string{"snapshot", "heartbeat", "heartbeat"}) {
		t.Fatalf("frames %v, want the snapshot then heartbeats", types)
	}
}
