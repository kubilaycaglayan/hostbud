package queue

import (
	"testing"

	"hostbud/internal/store"
)

// V2-M2 T3: the project's first (oldest) queue keeps V2-M1's session name;
// another queue on the same project adds its name; renaming a queue never
// changes a run's stored session.
func TestSessionNamesPerQueue(t *testing.T) {
	e := newDispEnv(t, "first")
	e.svc.SetParallelQueues(true)
	docs, err := e.svc.Create(e.ctx(), "project_a", "Release notes")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.AddItem(e.ctx(), docs.ID, "claude", "", "/goal docs"); err != nil {
		t.Fatal(err)
	}
	e.startQueue()
	if _, err := e.svc.Start(e.ctx(), docs.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	if got := e.run(1).SessionName; got != "app-q1" {
		t.Errorf("first queue's session = %q, want V2-M1's app-q1", got)
	}
	runs, _ := e.st.LatestRuns(e.ctx(), docs.ID)
	var second store.Run
	for _, r := range runs {
		second = r
	}
	if second.SessionName != "app-Release-notes-q1" {
		t.Errorf("second queue's session = %q, want app-Release-notes-q1", second.SessionName)
	}
	// Renaming the queue leaves the stored session name alone.
	if _, err := e.svc.Rename(e.ctx(), docs.ID, "Changelog"); err != nil {
		t.Fatal(err)
	}
	if r, _ := e.st.Run(e.ctx(), second.ID); r.SessionName != "app-Release-notes-q1" {
		t.Errorf("rename changed the run's session to %q", r.SessionName)
	}
	// The first queue stays "first" by age, whatever the names.
	if _, err := e.svc.Rename(e.ctx(), e.queue.ID, "Zeta"); err != nil {
		t.Fatal(err)
	}
	q, _ := e.st.Queue(e.ctx(), e.queue.ID)
	if got := e.d.sessionLabel(e.ctx(), q); got != "" {
		t.Errorf("the oldest queue's label = %q, want empty", got)
	}
	dq, _ := e.st.Queue(e.ctx(), docs.ID)
	if got := e.d.sessionLabel(e.ctx(), dq); got != "Changelog" {
		t.Errorf("the second queue's label = %q", got)
	}
}
