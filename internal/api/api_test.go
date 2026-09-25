package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/session"
	"hostbud/internal/tmux"
)

const origin = "http://localhost:9055"

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

type env struct {
	h   http.Handler
	svc *fakeService
	bus *events.Bus
	m   *fakeMachine
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{svc: &fakeService{}, bus: events.NewBus(), m: host("a")}
	e.h = New(Config{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Dist: fstest.MapFS{},
		Origins: AllowedOrigins("hostbud.example.com", 9055), Bus: e.bus,
		Machines: []Snapshotter{e.m}, Sessions: e.svc,
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

func TestOriginOnStateChangingRequests(t *testing.T) {
	cases := []struct {
		origin string
		ok     bool
	}{
		{"http://localhost:9055", true},
		{"https://hostbud.example.com", true},
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

func TestEventsSocketOrigin(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/events"
	for _, o := range []string{"http://evil.example.com", ""} {
		hdr := http.Header{}
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
	c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}})
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

func TestTerminalSocketIsOriginChecked(t *testing.T) {
	reached := 0
	h := New(Config{Dist: fstest.MapFS{}, Origins: AllowedOrigins("", 9055), Bus: events.NewBus(),
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
		Bus: events.NewBus(), Machines: []Snapshotter{m}, Sessions: svc})

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
