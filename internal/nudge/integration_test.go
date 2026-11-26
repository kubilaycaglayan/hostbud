//go:build integration

package nudge

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/session"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
	"hostbud/internal/testenv"
)

func TestIntegrationReplacementNudgesOnlyTheExactSession(t *testing.T) {
	ctx := context.Background()
	client := testenv.Connected(t, testenv.SSHD)
	testenv.Sh(t, client, `tmux kill-server 2>/dev/null || true
rm -rf /home/dev/nudge-it /home/dev/bin/codex
mkdir -p /home/dev/nudge-it /home/dev/bin
cat > /home/dev/bin/codex <<'EOF'
#!/bin/sh
printf 'stub-started\n' >> /home/dev/nudge-it/provider.log
while IFS= read -r line; do printf '%s\n' "$line" >> /home/dev/nudge-it/input.log; done
EOF
chmod +x /home/dev/bin/codex
tmux set-environment -g PATH /home/dev/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
mkdir -p /home/dev/nudge-it/first /home/dev/nudge-it/selected`)
	inv := inventory.New(client, events.NewBus(), inventory.Options{MachineID: sshx.HostMachineID, Interval: time.Second})
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { inv.Run(runCtx); close(done) }()
	t.Cleanup(func() {
		testenv.Sh(t, client, "tmux kill-server 2>/dev/null || true; rm -rf /home/dev/nudge-it /home/dev/bin/codex")
		cancel()
		<-done
	})
	if err := inv.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	sessions := session.New(client, session.Trackers{sshx.HostMachineID: inv}, nil)
	const adjacent = "nudge-neighbor"
	if _, err := sessions.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: adjacent, Path: "/home/dev/nudge-it/first", StartArgv: []string{"sleep", "3600"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Create(ctx, session.Spec{Machine: sshx.HostMachineID, Name: SessionName, Path: "/home/dev/nudge-it/first", StartArgv: []string{"sleep", "3600"}, ExactName: true}); err != nil {
		t.Fatal(err)
	}
	service := New(Config{Sessions: sessions})
	if err := service.replaceSession(ctx, storeOccurrence("/home/dev/nudge-it/selected")); err != nil {
		t.Fatal(err)
	}
	if got := integrationDisplay(t, client, SessionName, "#{session_path}"); got != "/home/dev/nudge-it/selected" {
		t.Fatalf("replacement path = %q", got)
	}
	testenv.Sh(t, client, fmt.Sprintf("tmux has-session -t '=%s'", adjacent))
	waitForRemoteText(t, client, "/home/dev/nudge-it/provider.log", "stub-started")
	waitForRemoteText(t, client, "/home/dev/nudge-it/input.log", Prompt)
}

func storeOccurrence(directory string) store.WindowNudgeOccurrence {
	return store.WindowNudgeOccurrence{MachineID: sshx.HostMachineID, Directory: directory, Provider: "codex", ScheduledAt: time.Now().UTC()}
}

func integrationDisplay(t *testing.T, client *sshx.Client, name, format string) string {
	t.Helper()
	output, err := client.Exec(context.Background(), sshx.HostMachineID, "tmux", "display-message", "-p", "-t", "="+name+":", format)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}

func waitForRemoteText(t *testing.T, client *sshx.Client, file, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		output, err := client.Exec(context.Background(), sshx.HostMachineID, "cat", file)
		if err == nil && strings.Contains(string(output), want) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%q did not appear in %s", want, file)
}
