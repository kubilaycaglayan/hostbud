package term

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Reproduce the screenshot at the actual PTY boundary, without SSH, tmux or
// xterm mocks. The child deliberately has not entered raw mode yet.
func TestStartPTYDoesNotEchoMouseReportsDuringSetup(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	gate := filepath.Join(t.TempDir(), "continue")
	proc, err := StartPTY(ctx, []string{"sh", "-c", `printf 'setup-ready\n'; while [ ! -f "$1" ]; do sleep 0.01; done; printf 'setup-finished\n'`, "hostbud-test", gate}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proc.Kill(); _, _ = proc.Wait() }()
	chunks := make(chan string, 32)
	go func() {
		defer close(chunks)
		buf := make([]byte, 4096)
		for {
			n, readErr := proc.Read(buf)
			if n > 0 {
				chunks <- string(buf[:n])
			}
			if readErr != nil {
				return
			}
		}
	}()
	var output strings.Builder
	for !strings.Contains(output.String(), "setup-ready") {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				t.Fatal("child exited before setup")
			}
			output.WriteString(chunk)
		case <-ctx.Done():
			t.Fatal("setup timed out")
		}
	}
	// SGR no-button mouse-motion reports, using the screenshot's coordinates.
	if _, err = io.WriteString(proc, "\x1b[<35;124;17M\x1b[<35;134;20M"); err != nil {
		t.Fatal(err)
	}
	// Release only after input was written: the child cannot have changed the
	// terminal mode before the reports reached the PTY.
	if err = os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				if strings.Contains(output.String(), "[<35;") {
					t.Fatalf("mouse reports leaked into output during setup: %q", output.String())
				}
				if !strings.Contains(output.String(), "setup-finished") {
					t.Fatalf("missing real child output: %q", output.String())
				}
				return
			}
			output.WriteString(chunk)
		case <-ctx.Done():
			t.Fatal("PTY output timed out")
		}
	}
}

// Raw mode must preserve keyboard/control/mouse bytes and output line endings;
// suppressing echo must not disable terminal input or consume Ctrl-C locally.
func TestStartPTYRawByteTransport(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	proc, err := StartPTY(ctx, []string{"sh", "-c", "printf 'ready\\n'; exec cat"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proc.Kill(); _, _ = proc.Wait() }()
	ready := make([]byte, len("ready\n"))
	if _, err = io.ReadFull(proc, ready); err != nil {
		t.Fatal(err)
	}
	if string(ready) != "ready\n" {
		t.Fatalf("output was transformed: %q", ready)
	}
	payload := "\x03\x1b[<35;124;17M\r\n\x7fhostbud-\u00e9"
	if _, err = io.WriteString(proc, payload); err != nil {
		t.Fatal(err)
	}
	received := make([]byte, len(payload))
	if _, err = io.ReadFull(proc, received); err != nil {
		t.Fatal(err)
	}
	if string(received) != payload {
		t.Fatalf("raw input/output changed: got %q, want %q", received, payload)
	}
}
