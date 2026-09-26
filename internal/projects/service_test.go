package projects

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hostbud/internal/events"
	"hostbud/internal/session"
	"hostbud/internal/store"
)

type fakeRepo struct {
	projects   []store.Project
	links      map[string]store.SessionLink
	err        error
	created    store.Project
	createCall []string
	updated    bool
	deleted    bool
	commands   []store.RecentCommand
	remembered []string
}

func linkKey(machine, name string) string { return machine + ":" + name }
func (f *fakeRepo) Projects(_ context.Context, machine string) ([]store.Project, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []store.Project
	for _, p := range f.projects {
		if p.MachineID == machine {
			out = append(out, p)
		}
	}
	return out, nil
}
func (f *fakeRepo) Project(_ context.Context, id string) (store.Project, error) {
	for _, p := range f.projects {
		if p.ID == id {
			return p, nil
		}
	}
	if f.created.ID == id {
		return f.created, nil
	}
	return store.Project{}, store.ErrNotFound
}
func (f *fakeRepo) CreateProject(_ context.Context, machine, p, name string) (store.Project, error) {
	f.createCall = []string{machine, p, name}
	if f.err != nil {
		return store.Project{}, f.err
	}
	for _, old := range f.projects {
		if old.MachineID == machine && old.Path == p {
			return old, nil
		}
	}
	f.created = store.Project{ID: "new", MachineID: machine, Path: p, Name: name}
	f.projects = append(f.projects, f.created)
	return f.created, nil
}
func (f *fakeRepo) RenameProject(_ context.Context, id, name string) (store.Project, error) {
	if f.err != nil {
		return store.Project{}, f.err
	}
	for i := range f.projects {
		if f.projects[i].ID == id {
			f.projects[i].Name = name
			return f.projects[i], nil
		}
	}
	return store.Project{}, store.ErrNotFound
}
func (f *fakeRepo) SessionLink(_ context.Context, machine, name string) (store.SessionLink, error) {
	link, ok := f.links[linkKey(machine, name)]
	if !ok {
		return store.SessionLink{}, store.ErrNotFound
	}
	return link, nil
}
func (f *fakeRepo) UpsertSessionLink(_ context.Context, machine, name, project string) error {
	f.links[linkKey(machine, name)] = store.SessionLink{MachineID: machine, SessionName: name, ProjectID: project}
	return f.err
}
func (f *fakeRepo) RenameSessionLink(_ context.Context, machine, from, to string) error {
	link, ok := f.links[linkKey(machine, from)]
	if !ok {
		return store.ErrNotFound
	}
	delete(f.links, linkKey(machine, from))
	link.SessionName = to
	f.links[linkKey(machine, to)] = link
	f.updated = true
	return nil
}
func (f *fakeRepo) DeleteSessionLink(_ context.Context, machine, name string) error {
	delete(f.links, linkKey(machine, name))
	f.deleted = true
	return nil
}
func (f *fakeRepo) PruneSessionLinks(_ context.Context, machine string, active []string) error {
	for key, link := range f.links {
		if link.MachineID != machine {
			continue
		}
		found := false
		for _, name := range active {
			if name == link.SessionName {
				found = true
			}
		}
		if !found {
			delete(f.links, key)
		}
	}
	return nil
}

func (f *fakeRepo) RecentCommands(_ context.Context, projectID string) ([]store.RecentCommand, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []store.RecentCommand
	for _, command := range f.commands {
		if command.ProjectID == projectID {
			out = append(out, command)
		}
	}
	return out, nil
}
func (f *fakeRepo) RememberRecentCommand(_ context.Context, projectID, command string) error {
	if f.err != nil {
		return f.err
	}
	f.remembered = append(f.remembered, projectID+":"+command)
	f.commands = append(f.commands, store.RecentCommand{ProjectID: projectID, Command: command})
	return nil
}

type fakeCreator struct {
	spec session.Spec
	err  error
}

func (f *fakeCreator) Create(_ context.Context, spec session.Spec) (string, error) {
	f.spec = spec
	return "created-session", f.err
}

func TestPlaceLongestPathComponentPrefixAndUnmatched(t *testing.T) {
	repo := &fakeRepo{projects: []store.Project{
		{ID: "root", MachineID: "host", Path: "/"},
		{ID: "app", MachineID: "host", Path: "/home/dev/app/"},
		{ID: "application", MachineID: "host", Path: "/home/dev/application"},
		{ID: "foreign", MachineID: "server-a", Path: "/home/dev/app/nested"},
	}, links: map[string]store.SessionLink{}}
	svc := New(repo, events.NewBus(), nil)
	for _, tc := range []struct {
		machine, sessionPath, want string
	}{
		{"host", "/home/dev/app/sub/../child", "app"},
		{"host", "/home/dev/application/file", "application"},
		{"host", "/home/dev/app-old", "root"},
		{"host", "/unmatched", "root"},
		{"server-a", "/home/dev/app/nested/file", "foreign"},
	} {
		got, err := svc.Place(context.Background(), tc.machine, "", tc.sessionPath)
		if err != nil || !got.Matched || got.ProjectID != tc.want || got.MachineID != tc.machine {
			t.Errorf("Place(%s,%s) = %+v, %v; want %s", tc.machine, tc.sessionPath, got, err, tc.want)
		}
	}
	got, err := svc.Place(context.Background(), "host", "", "relative")
	if err != nil || got.Matched {
		t.Fatalf("relative unmatched path = %+v, %v", got, err)
	}
	withoutRoot := &fakeRepo{projects: []store.Project{{ID: "app", MachineID: "host", Path: "/home/dev/app"}}, links: map[string]store.SessionLink{}}
	got, err = New(withoutRoot, nil, nil).Place(context.Background(), "host", "", "/home/dev/app-old")
	if err != nil || got.Matched {
		t.Fatalf("sibling-prefix path falsely matched: %+v, %v", got, err)
	}
}

func TestExplicitLinkTakesPrecedenceAndStaysMachineScoped(t *testing.T) {
	repo := &fakeRepo{projects: []store.Project{
		{ID: "parent", MachineID: "host", Path: "/home/dev"},
		{ID: "nested", MachineID: "host", Path: "/home/dev/app"},
		{ID: "foreign", MachineID: "server-a", Path: "/home/dev/app"},
	}, links: map[string]store.SessionLink{
		linkKey("host", "linked"):     {MachineID: "host", SessionName: "linked", ProjectID: "parent"},
		linkKey("server-a", "linked"): {MachineID: "server-a", SessionName: "linked", ProjectID: "foreign"},
	}}
	svc := New(repo, nil, nil)
	got, err := svc.Place(context.Background(), "host", "linked", "/home/dev/app/file")
	if err != nil || got.ProjectID != "parent" {
		t.Fatalf("link precedence = %+v, %v", got, err)
	}
	got, err = svc.Place(context.Background(), "host", "other", "/home/dev/app/file")
	if err != nil || got.ProjectID != "nested" {
		t.Fatalf("machine-scoped fallback = %+v, %v", got, err)
	}
}

func TestCreateSessionUsesProjectSpecAndLinksIt(t *testing.T) {
	repo := &fakeRepo{projects: []store.Project{{ID: "p", MachineID: "host", Path: "/home/dev/project", Name: "project"}}, links: map[string]store.SessionLink{}}
	creator := &fakeCreator{}
	svc := New(repo, nil, nil)
	svc.SetSessionCreator(creator)
	name, err := svc.CreateSession(context.Background(), "p", session.Spec{
		Machine: "wrong-machine", Name: "work", Path: "/wrong/path", Env: map[string]string{"K": "v"}, StartCommand: `echo '$HOME'`,
	})
	if err != nil || name != "created-session" {
		t.Fatalf("create project session = %q, %v", name, err)
	}
	if creator.spec.Machine != "host" || creator.spec.Path != "/home/dev/project" || creator.spec.Name != "work" ||
		creator.spec.Env["K"] != "v" || creator.spec.StartCommand != `echo '$HOME'` {
		t.Fatalf("session spec = %+v", creator.spec)
	}
	if link, err := repo.SessionLink(context.Background(), "host", name); err != nil || link.ProjectID != "p" {
		t.Fatalf("created session link = %+v, %v", link, err)
	}
	if len(repo.remembered) != 1 || repo.remembered[0] != `p:echo '$HOME'` {
		t.Fatalf("remembered commands = %v", repo.remembered)
	}
	creator.err = errors.New("session start failed")
	if _, err := svc.CreateSession(context.Background(), "p", session.Spec{StartCommand: "echo failed"}); err == nil {
		t.Fatal("failed session creation returned nil")
	}
	if len(repo.remembered) != 1 {
		t.Fatalf("failed command was remembered: %v", repo.remembered)
	}
}

func TestRecentCommandsAreProjectScopedAndUnknownProjectsFail(t *testing.T) {
	repo := &fakeRepo{
		projects: []store.Project{{ID: "one", MachineID: "host"}, {ID: "two", MachineID: "host"}},
		commands: []store.RecentCommand{{ProjectID: "one", Command: "one-command"}, {ProjectID: "two", Command: "two-command"}},
	}
	svc := New(repo, nil, nil)
	commands, err := svc.RecentCommands(context.Background(), "one")
	if err != nil || len(commands) != 1 || commands[0].Command != "one-command" {
		t.Fatalf("recent commands = %+v, %v", commands, err)
	}
	if _, err := svc.RecentCommands(context.Background(), "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown project recent commands = %v", err)
	}
}

func TestCreateSessionRejectsOversizeCommandBeforeRemoteCreate(t *testing.T) {
	repo := &fakeRepo{projects: []store.Project{{ID: "p", MachineID: "host", Path: "/work"}}, links: map[string]store.SessionLink{}}
	creator := &fakeCreator{}
	svc := New(repo, nil, nil)
	svc.SetSessionCreator(creator)
	if _, err := svc.CreateSession(context.Background(), "p", session.Spec{StartCommand: strings.Repeat("x", 4097)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversize start command error = %v", err)
	}
	if creator.spec.StartCommand != "" || len(repo.remembered) != 0 {
		t.Fatalf("invalid command reached creator or history: %+v, %v", creator.spec, repo.remembered)
	}
}

func TestChangedEventOnlyAfterSuccessfulCreateOrRename(t *testing.T) {
	bus := events.NewBus()
	ch, cancel := bus.Subscribe(4)
	defer cancel()
	repo := &fakeRepo{projects: []store.Project{}, links: map[string]store.SessionLink{}}
	svc := New(repo, bus, nil)
	p, err := svc.Create(context.Background(), "host", "/home/dev/app", "App")
	if err != nil {
		t.Fatal(err)
	}
	event := <-ch
	payload, ok := event.Payload.(Changed)
	if event.Type != events.ProjectsChanged || event.Machine != "host" || !ok || payload.Project.ID != p.ID || payload.Action != "upsert" {
		t.Fatalf("project event = %+v", event)
	}
	repo.err = errors.New("write failed")
	if _, err := svc.Rename(context.Background(), p.ID, "failed"); err == nil {
		t.Fatal("failed rename returned nil")
	}
	select {
	case e := <-ch:
		t.Fatalf("failed persistence published event: %+v", e)
	default:
	}
}

func TestLifecycleHooksAndPruning(t *testing.T) {
	repo := &fakeRepo{links: map[string]store.SessionLink{
		linkKey("host", "old"):      {MachineID: "host", SessionName: "old", ProjectID: "p"},
		linkKey("host", "ended"):    {MachineID: "host", SessionName: "ended", ProjectID: "p"},
		linkKey("server-a", "keep"): {MachineID: "server-a", SessionName: "keep", ProjectID: "p2"},
	}}
	svc := New(repo, nil, nil)
	if err := svc.RenameSessionLink(context.Background(), "host", "old", "new"); err != nil || !repo.updated {
		t.Fatalf("rename hook: %v", err)
	}
	if err := svc.EndSessionLink(context.Background(), "host", "new"); err != nil || !repo.deleted {
		t.Fatalf("end hook: %v", err)
	}
	if err := repo.PruneSessionLinks(context.Background(), "host", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := repo.links[linkKey("host", "ended")]; ok {
		t.Fatal("ended session link was not pruned")
	}
	if _, ok := repo.links[linkKey("server-a", "keep")]; !ok {
		t.Fatal("pruning host links affected another machine")
	}
}
