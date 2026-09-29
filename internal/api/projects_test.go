package api

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"testing/fstest"

	"hostbud/internal/projects"
	"hostbud/internal/session"
	"hostbud/internal/store"
)

type fakeProjects struct {
	items       []store.Project
	get         store.Project
	created     store.Project
	renamed     store.Project
	err         error
	calls       []string
	machineID   string
	path        string
	name        string
	sessionSpec session.Spec
	recent      []store.RecentCommand
	deleted     bool
}

func (f *fakeProjects) List(_ context.Context, machineID string) ([]store.Project, error) {
	f.calls = append(f.calls, "list")
	f.machineID = machineID
	return f.items, f.err
}
func (f *fakeProjects) RecentCommands(_ context.Context, id string) ([]store.RecentCommand, error) {
	f.calls = append(f.calls, "recent "+id)
	return f.recent, f.err
}
func (f *fakeProjects) Get(_ context.Context, id string) (store.Project, error) {
	f.calls = append(f.calls, "get "+id)
	return f.get, f.err
}
func (f *fakeProjects) Create(_ context.Context, machineID, p, name string) (store.Project, error) {
	f.calls = append(f.calls, "create")
	f.machineID, f.path, f.name = machineID, p, name
	return f.created, f.err
}
func (f *fakeProjects) Rename(_ context.Context, id, name string) (store.Project, error) {
	f.calls = append(f.calls, "rename "+id)
	f.name = name
	return f.renamed, f.err
}
func (f *fakeProjects) Delete(_ context.Context, id string) error {
	f.calls = append(f.calls, "delete "+id)
	if f.err != nil {
		return f.err
	}
	f.deleted = true
	return nil
}
func (f *fakeProjects) CreateSession(_ context.Context, id string, spec session.Spec) (string, error) {
	f.calls = append(f.calls, "session "+id)
	f.sessionSpec = spec
	return "created-session", f.err
}

func TestProjectAPICreateListRenameAndValidation(t *testing.T) {
	e := newEnv(t)
	f := &fakeProjects{created: store.Project{ID: "project-a", MachineID: "host", Path: "/home/dev/app", Name: "app"},
		items: []store.Project{{ID: "project-a", MachineID: "host", Path: "/home/dev/app", Name: "app"}}}
	e.h = New(Config{Dist: fstest.MapFS{}, Origins: AllowedOrigins("hostbud.example.com", 9055), Bus: e.bus,
		Machines: []Snapshotter{e.m}, Sessions: e.svc, Auth: &fakeAuth{}, Projects: f})

	created := e.do(t, http.MethodPost, "/api/projects", `{"path":"/home/dev/app/","name":" App "}`, nil)
	if created.Code != http.StatusCreated || f.machineID != "host" || f.path != "/home/dev/app/" || f.name != " App " {
		t.Fatalf("create = %d %s, args=%s %s %s", created.Code, created.Body, f.machineID, f.path, f.name)
	}
	list := e.do(t, http.MethodGet, "/api/projects?machine=host", "", nil)
	if list.Code != http.StatusOK || f.machineID != "host" || len(decodeBody[struct {
		Projects []store.Project `json:"projects"`
	}](t, list).Projects) != 1 {
		t.Fatalf("list = %d %s", list.Code, list.Body)
	}
	if rec := e.do(t, http.MethodGet, "/api/projects/project-a", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("get = %d %s", rec.Code, rec.Body)
	}
	f.recent = []store.RecentCommand{{ProjectID: "project-a", Command: "make test"}, {ProjectID: "project-a", Command: "go test ./..."}}
	recent := e.do(t, http.MethodGet, "/api/projects/project-a/recent-commands", "", nil)
	if recent.Code != http.StatusOK || !slices.Equal(decodeBody[struct {
		Commands []string `json:"commands"`
	}](t, recent).Commands, []string{"make test", "go test ./..."}) {
		t.Fatalf("recent commands = %d %s", recent.Code, recent.Body)
	}
	if rec := e.do(t, http.MethodPatch, "/api/projects/project-a", `{"name":"renamed"}`, nil); rec.Code != http.StatusOK || f.name != "renamed" {
		t.Fatalf("rename = %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(t, http.MethodPatch, "/api/projects/project-a", `{"name":"renamed","pinned":true}`, nil); rec.Code != http.StatusBadRequest || f.name != "renamed" {
		t.Fatalf("pinned project field must be rejected without changing the project: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(t, http.MethodPost, "/api/projects/project-a/sessions", `{"name":"my-session","startCommand":"make run"}`, nil); rec.Code != http.StatusCreated || f.sessionSpec.Name != "my-session" || f.sessionSpec.StartCommand != "make run" {
		t.Fatalf("project session = %d %s, spec=%+v", rec.Code, rec.Body, f.sessionSpec)
	}
	if rec := e.do(t, http.MethodPost, "/api/projects/project-a/sessions", `{"name":" new session "}`, nil); rec.Code != http.StatusCreated || f.sessionSpec.Name != "new-session" {
		t.Fatalf("spaced project session name = %d %s, spec=%+v", rec.Code, rec.Body, f.sessionSpec)
	}
	if rec := e.do(t, http.MethodPost, "/api/projects/project-a/sessions", `{"name":"bad/name"}`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid project session name = %d %s", rec.Code, rec.Body)
	}
	f.err = &session.Error{Code: session.CodePathNotFound, Message: "project directory is unavailable"}
	if rec := e.do(t, http.MethodPost, "/api/projects/project-a/sessions", `{}`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("project session error mapping = %d %s", rec.Code, rec.Body)
	}
	f.err = nil
	for _, c := range []struct{ path, body string }{
		{"/api/projects", `{"path":"relative","name":"bad"}`},
		{"/api/projects", `{"path":"/home/dev","extra":true}`},
		{"/api/projects?machine=host&machine=host", ""},
		{"/api/projects?unknown=x", ""},
	} {
		method := http.MethodGet
		if c.body != "" {
			method = http.MethodPost
		}
		if rec := e.do(t, method, c.path, c.body, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s = %d %s", method, c.path, rec.Code, rec.Body)
		}
	}
	if rec := e.do(t, http.MethodDelete, "/api/projects/project-a", "", nil); rec.Code != http.StatusNoContent || !f.deleted {
		t.Fatalf("project delete = %d %s, deleted=%t", rec.Code, rec.Body, f.deleted)
	}
}

func TestProjectAPIAuthOriginAndErrors(t *testing.T) {
	e := newEnv(t)
	f := &fakeProjects{created: store.Project{ID: "project-a", MachineID: "host"}}
	e.h = New(Config{Dist: fstest.MapFS{}, Origins: AllowedOrigins("hostbud.example.com", 9055), Bus: e.bus,
		Machines: []Snapshotter{e.m}, Auth: &fakeAuth{}, Projects: f})
	if rec := e.do(t, http.MethodGet, "/api/projects", "", map[string]string{"Cookie": ""}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d", rec.Code)
	}
	if rec := e.do(t, http.MethodGet, "/api/projects/project-a/recent-commands", "", map[string]string{"Cookie": ""}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous recent commands = %d", rec.Code)
	}
	if rec := e.do(t, http.MethodDelete, "/api/projects/project-a", "", map[string]string{"Cookie": ""}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous project delete = %d", rec.Code)
	}
	if rec := e.do(t, http.MethodPost, "/api/projects", `{"path":"/home/dev/app"}`,
		map[string]string{"Origin": "https://evil.example.com"}); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign-origin create = %d", rec.Code)
	}
	if rec := e.do(t, http.MethodDelete, "/api/projects/project-a", "",
		map[string]string{"Origin": "https://evil.example.com"}); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign-origin delete = %d", rec.Code)
	}
	if len(f.calls) != 0 {
		t.Fatalf("unauthorized/foreign request reached project service: %v", f.calls)
	}
	for _, tc := range []struct {
		err    error
		status int
	}{
		{store.ErrNotFound, http.StatusNotFound},
		{projects.ErrInvalidInput, http.StatusBadRequest},
		{context.DeadlineExceeded, http.StatusServiceUnavailable},
	} {
		f.err = tc.err
		if rec := e.do(t, http.MethodGet, "/api/projects", "", nil); rec.Code != tc.status {
			t.Errorf("error %v: status=%d body=%s", tc.err, rec.Code, rec.Body)
		}
	}
	f.err = store.ErrNotFound
	if rec := e.do(t, http.MethodDelete, "/api/projects/unknown", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown project delete = %d %s", rec.Code, rec.Body)
	}
}

func TestEmptyProjectListIsAnArray(t *testing.T) {
	e := newEnv(t)
	rec := e.do(t, http.MethodGet, "/api/projects", "", nil)
	body := decodeBody[struct {
		Projects []store.Project `json:"projects"`
	}](t, rec)
	if rec.Code != http.StatusOK || body.Projects == nil || len(body.Projects) != 0 {
		t.Fatalf("empty project list = %d %s", rec.Code, rec.Body)
	}
}
