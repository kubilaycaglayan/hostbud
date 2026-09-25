package sshx

import (
	"os/exec"
	"testing"
)

func runShell(t *testing.T, line string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "sh", "-c", line).Output() //nolint:gosec // test: line is built by Command
	if err != nil {
		t.Fatalf("sh -c %q: %v", line, err)
	}
	return string(out)
}
