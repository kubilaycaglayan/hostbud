//go:build integration

package agents

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"hostbud/internal/fsbrowse"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
	"hostbud/internal/testenv"
)

// V2-M1 T5: the Claude adapter against the test/sshd target: the version
// check through the login shell (a fake claude in ~/.local/bin, found only
// via ~/.profile), and ReadGoalState over read-only SFTP.
func TestIntegrationClaudeAdapterOnTarget(t *testing.T) {
	ctx := context.Background()
	c := testenv.Connected(t, testenv.SSHD)
	files := fsbrowse.New(c, sshx.HostMachineID, time.Minute, 5*time.Second)
	t.Cleanup(func() {
		_ = files.Close()
		testenv.Sh(t, c, "rm -rf ~/.claude/projects/-home-dev-agents-it ~/.claude/projects/escape ~/.local/bin/claude")
	})
	claude := NewClaude(c, func(string) Files { return files }, func(string) string { return "/home/dev" })

	testenv.Sh(t, c, `mkdir -p ~/.local/bin && printf '#!/bin/sh\necho "2.1.283 (Claude Code)"\n' > ~/.local/bin/claude && chmod 755 ~/.local/bin/claude`)
	if v, err := claude.CheckVersion(ctx, sshx.HostMachineID); err != nil || v != "2.1.283" {
		t.Fatalf("CheckVersion = %q, %v", v, err)
	}
	testenv.Sh(t, c, `printf '#!/bin/sh\necho "1.9.3 (Claude Code)"\n' > ~/.local/bin/claude`)
	if _, err := claude.CheckVersion(ctx, sshx.HostMachineID); err == nil || !strings.Contains(err.Error(), "found 1.9.3") {
		t.Fatalf("old client: %v", err)
	}
	testenv.Sh(t, c, "rm -f ~/.local/bin/claude")
	if _, err := claude.CheckVersion(ctx, sshx.HostMachineID); err == nil || err.Error() != "claude not found on the host — install Claude Code first" {
		t.Fatalf("missing client: %v", err)
	}

	achieved, err := os.ReadFile(fixtureDir + "achieved.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	dir := "/home/dev/.claude/projects/-home-dev-agents-it"
	path := dir + "/" + fixSession + ".jsonl"
	metAt := strings.Index(string(achieved), `"met":true`)
	metStart := strings.LastIndex(string(achieved[:metAt]), "\n") + 1
	if _, err := c.ExecInput(ctx, sshx.HostMachineID, achieved[:metStart], "sh", "-c", "mkdir -p "+dir+" && cat > "+path); err != nil {
		t.Fatal(err)
	}
	before := testenv.Sh(t, c, "sha256sum "+path+"; stat -c %Y "+path)
	run := store.Run{StartedAt: beforeFixtures}
	b := Binding{SessionID: fixSession, TranscriptPath: path}
	first, err := claude.ReadGoalState(ctx, sshx.HostMachineID, b, run, condDone)
	if err != nil || first.Status != Pending || first.Offset != int64(metStart) {
		t.Fatalf("first read: %+v, %v", first, err)
	}
	if after := testenv.Sh(t, c, "sha256sum "+path+"; stat -c %Y "+path); after != before {
		t.Fatal("reading changed the transcript")
	}
	// The client appends the verdict: the next read sees it once.
	if _, err := c.ExecInput(ctx, sshx.HostMachineID, achieved[metStart:], "sh", "-c", "cat >> "+path); err != nil {
		t.Fatal(err)
	}
	run.TranscriptOffset = first.Offset
	second, err := claude.ReadGoalState(ctx, sshx.HostMachineID, b, run, condDone)
	if err != nil || second.Status != Achieved {
		t.Fatalf("second read: %+v, %v", second, err)
	}

	// A symlink inside the projects tree that points out of it is refused.
	testenv.Sh(t, c, "ln -sfn /etc ~/.claude/projects/escape")
	escaped, err := claude.ReadGoalState(ctx, sshx.HostMachineID, Binding{SessionID: fixSession, TranscriptPath: "/home/dev/.claude/projects/escape/passwd"}, run, condDone)
	if err != nil || escaped.Status != Unknown || !strings.Contains(escaped.Reason, "resolves outside") {
		t.Fatalf("symlink escape: %+v, %v", escaped, err)
	}
}
