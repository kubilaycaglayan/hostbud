package queue

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"

	"hostbud/internal/session"
	"hostbud/internal/store"
)

var (
	testProject = store.Project{ID: "project_a", MachineID: store.HostMachineID, Path: "/home/dev/my app", Name: "My App.v2"}
	testItem    = store.QueueItem{ID: "item_a", Position: 2, Agent: "claude", Instruction: "/goal ship M2"}
)

func TestRunSessionName(t *testing.T) {
	for _, c := range []struct {
		project string
		pos     int
		want    string
	}{
		{"hostbud", 1, "hostbud-q1"},
		{"My App.v2", 12, "My-App-v2-q12"},
		{"", 3, "session-q3"},
		{strings.Repeat("x", 80), 7, strings.Repeat("x", 57) + "-q7"},
	} {
		if got := RunSessionName(c.project, c.pos); got != c.want {
			t.Errorf("RunSessionName(%q, %d) = %q, want %q", c.project, c.pos, got, c.want)
		}
	}
}

func TestStartCreatesTheSessionThroughTheService(t *testing.T) {
	st, sessions := newMemStore(), &fakeSessions{}
	agent := &fakeAgent{version: "2.1.283", argv: []string{"claude", "--model", "opus 4", "--settings", `{"hooks":{}}`, "/goal ship M2"}}
	var logs bytes.Buffer
	s := NewStarter(st, sessions, "http://127.0.0.1:9055", slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	run, err := s.Start(context.Background(), store.SourceUser, testProject, testItem, agent)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != store.RunStarting || run.SessionName != "My-App-v2-q2" || run.ClientVersion != "2.1.283" {
		t.Fatalf("run %+v", run)
	}
	if len(sessions.specs) != 1 {
		t.Fatalf("Create calls: %d", len(sessions.specs))
	}
	spec := sessions.specs[0]
	if spec.Machine != "host" || spec.Path != "/home/dev/my app" || spec.Name != "My-App-v2-q2" || spec.StartCommand != "" {
		t.Fatalf("spec %+v", spec)
	}
	if !slices.Equal(spec.StartArgv, agent.argv) {
		t.Fatalf("argv changed: %q", spec.StartArgv)
	}
	if keys := slices.Sorted(maps.Keys(spec.Env)); !slices.Equal(keys, []string{EnvRunID, EnvToken, EnvURL}) {
		t.Fatalf("env keys %v", keys)
	}
	token := spec.Env[EnvToken]
	if spec.Env[EnvURL] != "http://127.0.0.1:9055" || spec.Env[EnvRunID] != run.ID || !TokenMatches(token, run.TokenHash) {
		t.Fatalf("env %v doesn't match run %s", spec.Env, run.ID)
	}
	// started_at is set before the session exists; the command sees the run.
	if !run.StartedAt.Before(sessions.at[0]) || len(agent.built) != 1 || agent.built[0].ID != run.ID {
		t.Fatalf("started_at %v, session created %v", run.StartedAt, sessions.at[0])
	}
	// The token is only in the env handed to the service: never in the argv
	// or the logs (not even at debug).
	if strings.Contains(strings.Join(spec.StartArgv, " "), token) || strings.Contains(logs.String(), token) {
		t.Fatalf("token leaked: argv %q logs %s", spec.StartArgv, logs.String())
	}
}

func TestStartRetriedItemGetsNewTokenAndSuffixedSession(t *testing.T) {
	st, sessions := newMemStore(), &fakeSessions{}
	s := NewStarter(st, sessions, "http://127.0.0.1:9055", nil)
	agent := &fakeAgent{version: "0.157.1", argv: []string{"codex", "ship M2"}}
	first, _ := s.Start(context.Background(), store.SourceUser, testProject, testItem, agent)
	second, _ := s.Start(context.Background(), store.SourceUser, testProject, testItem, agent)
	if first.ID == second.ID || bytes.Equal(first.TokenHash, second.TokenHash) || sessions.specs[0].Env[EnvToken] == sessions.specs[1].Env[EnvToken] {
		t.Fatal("a retry reused the run id or token")
	}
	if second.SessionName != "My-App-v2-q2-1" {
		t.Fatalf("retry session %q, want the -1 suffix", second.SessionName)
	}
}

func TestStartFailuresFailTheRunWithADetail(t *testing.T) {
	versionErr := errors.New("Claude Code 2.1.283 or newer is needed on the host; found 1.9.3 — update with `claude update`")
	sessionErr := &session.Error{Code: session.CodePathNotFound, Message: "directory /home/dev/my app doesn't exist on the host", Hint: "Pick an existing directory, or create it first."}
	for _, c := range []struct {
		name      string
		agent     RunAgent
		sessErr   error
		detail    string
		wantBuild bool
	}{
		{"unknown agent", nil, nil, `unknown agent "claude" — edit the item and pick claude or codex`, false},
		{"client too old", &fakeAgent{versionErr: versionErr}, nil, versionErr.Error(), false},
		{"command fails", &fakeAgent{version: "2.1.283", buildErr: errors.New("flags: unbalanced quote")}, nil, "flags: unbalanced quote", true},
		{"path gone", &fakeAgent{version: "2.1.283", argv: []string{"claude"}}, sessionErr, sessionErr.Message + " — " + sessionErr.Hint, true},
	} {
		st, sessions := newMemStore(), &fakeSessions{err: c.sessErr}
		s := NewStarter(st, sessions, "http://127.0.0.1:9055", nil)
		run, err := s.Start(context.Background(), store.SourceUser, testProject, testItem, c.agent)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if run.Status != store.RunFailed || run.Detail != c.detail || run.EndedAt == nil {
			t.Errorf("%s: run %+v", c.name, run)
		}
		if got := st.eventKinds(run.ID); !slices.Equal(got, []string{"user:starting", "user:failed"}) {
			t.Errorf("%s: events %v", c.name, got)
		}
		wantCreate := c.sessErr != nil
		if (len(sessions.specs) == 1) != wantCreate {
			t.Errorf("%s: session created = %v", c.name, len(sessions.specs) == 1)
		}
	}
}
