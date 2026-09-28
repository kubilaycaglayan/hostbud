//go:build integration

package llm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"hostbud/internal/sshx"
	"hostbud/internal/store"
	"hostbud/internal/testenv"
)

type captureClassifier struct{ pane string }

func (c *captureClassifier) Classify(_ context.Context, pane string) (Result, error) {
	c.pane = pane
	return Result{Label: WaitingInput, Reason: "waiting for input"}, nil
}

// T2/T4: exercise the actual sshx and test/sshd tmux paths, including exact
// session targeting and removal of the run token when general scrubbing is off.
func TestIntegrationSupervisorCapturesExactPaneAndRemovesRunToken(t *testing.T) {
	ctx := context.Background()
	c := testenv.Connected(t, testenv.SSHD)
	name, err := store.NewULID(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sessionName := "m5-" + strings.ToLower(name)
	token := strings.Repeat("a", 32)
	target := "printf 'target-marker " + token + "\\n'"
	neighbor := "printf 'neighbor-marker\\n'"
	if _, err := c.Exec(ctx, sshx.HostMachineID, "tmux", "new-session", "-d", "-s", sessionName, "-e", "HOSTBUD_RUN_TOKEN="+token, "sh"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = c.Exec(context.Background(), sshx.HostMachineID, "tmux", "kill-session", "-t", "="+sessionName)
	})
	if _, err := c.Exec(ctx, sshx.HostMachineID, "tmux", "new-session", "-d", "-s", sessionName+"x", "sh"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = c.Exec(context.Background(), sshx.HostMachineID, "tmux", "kill-session", "-t", "="+sessionName+"x")
	})
	for _, pair := range []struct{ name, command string }{{sessionName, target}, {sessionName + "x", neighbor}} {
		if _, err := c.Exec(ctx, sshx.HostMachineID, "tmux", "send-keys", "-t", "="+pair.name+":", pair.command, "Enter"); err != nil {
			t.Fatal(err)
		}
	}

	dbHost := os.Getenv("HOSTBUD_TEST_DB_HOST")
	if dbHost == "" {
		dbHost = "hostbud-test-postgres"
	}
	dbPassword := os.Getenv("HOSTBUD_TEST_DB_PASSWORD")
	if dbPassword == "" {
		dbPassword = "hostbud-test-password" //nolint:gosec // disposable integration database
	}
	schemaHash := sha256.Sum256([]byte(t.TempDir()))
	schema := fmt.Sprintf("llm_it_%x", schemaHash[:8])
	st, err := store.Open(ctx, store.Config{Host: dbHost, Port: 5432, Name: "hostbud_test", User: "hostbud_test", Password: dbPassword, SSLMode: "disable", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	project, err := st.CreateProject(ctx, store.HostMachineID, "/home/dev/llm-it", "llm-it")
	if err != nil {
		t.Fatal(err)
	}
	queue, err := st.CreateQueue(ctx, project.ID, "llm-it")
	if err != nil {
		t.Fatal(err)
	}
	item, err := st.AddQueueItem(ctx, queue.ID, "claude", "", "integration")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(token))
	run, err := st.CreateRun(ctx, item.ID, hash[:], time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateRun(ctx, run.ID, store.RunUpdate{SessionName: &sessionName}); err != nil {
		t.Fatal(err)
	}
	run, err = st.Run(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.TransitionRun(ctx, run.ID, []string{store.RunStarting}, store.RunRunning, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.TransitionQueueItem(ctx, item.ID, []string{store.ItemQueued}, store.ItemRunning); err != nil {
		t.Fatal(err)
	}

	classifier := &captureClassifier{}
	var logs bytes.Buffer
	supervisor := NewSupervisor(st, c, classifier, nil, time.Minute, 2, false, slog.New(slog.NewTextHandler(&logs, nil)))
	supervisor.classify(ctx, run)
	if !strings.Contains(classifier.pane, "target-marker") || strings.Contains(classifier.pane, "neighbor-marker") {
		events, _ := st.RunEvents(ctx, run.ID, 10)
		kinds := make([]string, 0, len(events))
		for _, event := range events {
			kinds = append(kinds, event.Kind)
		}
		t.Fatalf("captured wrong pane: %q (events %v)", classifier.pane, kinds)
	}
	if strings.Contains(classifier.pane, token) {
		t.Fatalf("run token reached classifier with scrub disabled: %q", classifier.pane)
	}
	if strings.Contains(logs.String(), token) || strings.Contains(logs.String(), "target-marker") {
		t.Fatalf("pane text or token appeared in logs: %q", logs.String())
	}
}
