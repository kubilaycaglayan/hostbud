package nudge

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"hostbud/internal/session"
	"hostbud/internal/store"
)

type memoryRepository struct{ settings store.WindowNudge }

func (r *memoryRepository) WindowNudge(context.Context) (store.WindowNudge, error) {
	return r.settings, nil
}
func (r *memoryRepository) PutWindowNudge(_ context.Context, settings store.WindowNudge) (store.WindowNudge, error) {
	r.settings = settings
	return settings, nil
}
func (*memoryRepository) ClaimWindowNudgeOccurrence(context.Context, time.Time, time.Time) (store.WindowNudgeOccurrence, bool, error) {
	return store.WindowNudgeOccurrence{}, false, nil
}
func (*memoryRepository) SkipMissedWindowNudge(context.Context, time.Time, time.Time) (bool, error) {
	return false, nil
}
func (*memoryRepository) FinishWindowNudgeOccurrence(context.Context, time.Time, string, string) error {
	return nil
}
func (*memoryRepository) RecoverWindowNudgeOccurrences(context.Context) error { return nil }
func (*memoryRepository) TryWindowNudgeLock(context.Context) (func(), bool, error) {
	return func() {}, true, nil
}

type recordingSessions struct {
	steps     []string
	killErr   error
	createErr error
	sendErr   error
	spec      session.Spec
	prompt    string
}

func (s *recordingSessions) Kill(_ context.Context, machine, name string) error {
	s.steps = append(s.steps, "kill:"+machine+":"+name)
	return s.killErr
}
func (s *recordingSessions) Create(_ context.Context, spec session.Spec) (string, error) {
	s.steps = append(s.steps, "create")
	s.spec = spec
	return spec.Name, s.createErr
}
func (s *recordingSessions) SendCommand(_ context.Context, machine, name, prompt string) error {
	s.steps = append(s.steps, "prompt:"+machine+":"+name)
	s.prompt = prompt
	return s.sendErr
}

func TestReplaceSessionKillsThenCreatesExactNameThenSendsPrompt(t *testing.T) {
	ctx := context.Background()
	sessions := &recordingSessions{killErr: &session.Error{Code: session.CodeNotFound}}
	svc := New(Config{Sessions: sessions})
	due := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	err := svc.replaceSession(ctx, store.WindowNudgeOccurrence{
		ScheduledAt: due, MachineID: "server-a", Directory: "/home/dev/project", Provider: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"kill:server-a:" + SessionName, "create", "prompt:server-a:" + SessionName}; !reflect.DeepEqual(sessions.steps, want) {
		t.Fatalf("step order = %#v, want %#v", sessions.steps, want)
	}
	if sessions.spec.Name != SessionName || sessions.spec.Machine != "server-a" || sessions.spec.Path != "/home/dev/project" || !sessions.spec.ExactName || !reflect.DeepEqual(sessions.spec.StartArgv, []string{"claude"}) {
		t.Fatalf("created spec = %#v", sessions.spec)
	}
	if sessions.prompt != Prompt {
		t.Fatalf("prompt = %q, want %q", sessions.prompt, Prompt)
	}
}

func TestReplaceSessionStopsOnKillOrCreateError(t *testing.T) {
	for _, tc := range []struct {
		name string
		fail string
	}{{"kill", "kill"}, {"create", "create"}} {
		t.Run(tc.name, func(t *testing.T) {
			boom := errors.New("failed")
			sessions := &recordingSessions{}
			if tc.fail == "kill" {
				sessions.killErr = boom
			} else {
				sessions.createErr = boom
			}
			svc := New(Config{Sessions: sessions})
			err := svc.replaceSession(context.Background(), store.WindowNudgeOccurrence{MachineID: "host", Directory: "/home/dev", Provider: "codex"})
			if !errors.Is(err, boom) {
				t.Fatalf("error = %v, want %v", err, boom)
			}
			if tc.fail == "kill" && !reflect.DeepEqual(sessions.steps, []string{"kill:host:" + SessionName}) {
				t.Fatalf("steps = %#v", sessions.steps)
			}
			if tc.fail == "create" && !reflect.DeepEqual(sessions.steps, []string{"kill:host:" + SessionName, "create"}) {
				t.Fatalf("steps = %#v", sessions.steps)
			}
		})
	}
}

func TestPutSettingsArmsOnlyTheNextFutureSlotAndValidatesEnabledTarget(t *testing.T) {
	now := time.Date(2026, 11, 26, 1, 0, 0, 0, time.UTC)
	repo := &memoryRepository{}
	validated := 0
	svc := New(Config{Store: repo, Now: func() time.Time { return now }, ValidateTarget: func(_ context.Context, machine, directory string) error {
		validated++
		if machine != "host" || directory != "/home/dev" {
			t.Fatalf("validated target = %q %q", machine, directory)
		}
		return nil
	}})
	saved, err := svc.PutSettings(context.Background(), store.WindowNudge{
		Enabled: true, MachineID: "host", Directory: "/home/dev", Provider: " CLAUDE ", Schedule: string(Preset01),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 26, 3, 0, 0, 0, time.UTC) // 06:00 at fixed UTC+03:00.
	if saved.Provider != "claude" || saved.NextRunAt == nil || !saved.NextRunAt.Equal(want) || validated != 1 {
		t.Fatalf("saved settings = %+v; validated %d times", saved, validated)
	}
	// Disabling is always available and does not require the target to be up.
	svc.validate = func(context.Context, string, string) error { t.Fatal("validated disabled target"); return nil }
	saved, err = svc.PutSettings(context.Background(), store.WindowNudge{Enabled: false, MachineID: "host", Directory: "/home/dev", Provider: "codex", Schedule: string(Preset01)})
	if err != nil || saved.NextRunAt != nil {
		t.Fatalf("disabled settings = %+v, %v", saved, err)
	}
}
