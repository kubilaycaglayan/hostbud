package llm

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"hostbud/internal/store"
)

type fakeCapture struct {
	output string
	args   []string
	got    string
}

func (f *fakeCapture) ExecTo(_ context.Context, machine string, w io.Writer, args ...string) error {
	f.got = machine
	f.args = append([]string(nil), args...)
	_, err := io.WriteString(w, f.output)
	return err
}

func TestBoundedCaptureStopsAt256KiB(t *testing.T) {
	var capture boundedCapture
	input := strings.Repeat("x", (256<<10)+32)
	n, err := capture.Write([]byte(input))
	if err != nil || n != len(input) {
		t.Fatalf("write n=%d err=%v", n, err)
	}
	if capture.Len() != 256<<10 || capture.String() != input[:256<<10] {
		t.Fatalf("captured %d bytes, want first 256 KiB", capture.Len())
	}
}

func TestDueUsesQuietWindowEdgeAndLatestSignal(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	quiet := 20 * time.Minute
	started := now.Add(-quiet)
	run := store.Run{Status: store.RunRunning, StartedAt: started}
	if !due(run, now, quiet) {
		t.Fatal("run at quiet-window boundary should be due")
	}
	if due(run, now.Add(-time.Nanosecond), quiet) {
		t.Fatal("run just before quiet-window boundary should not be due")
	}
	signal := now.Add(-quiet + time.Minute)
	run.LastSignalAt = &signal
	if due(run, now, quiet) {
		t.Fatal("recent signal should restart quiet window")
	}
	run.Status = store.RunStale
	if !due(run, now, quiet) {
		t.Fatal("stale transition should be due immediately")
	}
}

func TestCaptureArgsValidateNameAndUseExactTarget(t *testing.T) {
	args, ok := captureArgs("run-1")
	want := []string{"tmux", "capture-pane", "-p", "-J", "-t", "=run-1:", "-S", "-200"}
	if !ok || !reflect.DeepEqual(args, want) {
		t.Fatalf("captureArgs=%v, %v; want %v, true", args, ok, want)
	}
	if args, ok := captureArgs("bad;name"); ok || args != nil {
		t.Fatalf("invalid session name accepted: %v, %v", args, ok)
	}
}

func TestRunTokenReadUsesExactSessionAndParsesOnlyTokenLine(t *testing.T) {
	args, ok := tokenArgs("run-1")
	want := []string{"tmux", "show-environment", "-t", "=run-1", "HOSTBUD_RUN_TOKEN"}
	if !ok || !reflect.DeepEqual(args, want) {
		t.Fatalf("tokenArgs=%v, %v; want %v, true", args, ok, want)
	}
	if _, ok := tokenArgs("bad;name"); ok {
		t.Fatal("invalid session name accepted")
	}
	const token = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if got, ok := parseRunToken("HOSTBUD_RUN_TOKEN=" + token + "\n"); !ok || got != token {
		t.Fatalf("parseRunToken=%q, %v", got, ok)
	}
	for _, output := range []string{"", "HOSTBUD_RUN_TOKEN=short\n", "OTHER=" + token + "\n", "HOSTBUD_RUN_TOKEN=" + token + "\nextra\n"} {
		if _, ok := parseRunToken(output); ok {
			t.Errorf("accepted malformed token output %q", output)
		}
	}
	capture := &fakeCapture{output: "HOSTBUD_RUN_TOKEN=" + token + "\n"}
	s := &Supervisor{ssh: capture}
	got, err := s.runToken(context.Background(), store.Run{MachineID: "host", SessionName: "run-1"})
	if err != nil || got != token || capture.got != "host" || !reflect.DeepEqual(capture.args, want) {
		t.Fatalf("runToken=%q err=%v machine=%q args=%v", got, err, capture.got, capture.args)
	}
	prepared := PreparePane("echo "+token, false, got)
	if strings.Contains(prepared, token) {
		t.Fatalf("token remained with scrub disabled: %q", prepared)
	}
}

func TestLastLinesKeepsOnlyNewestLines(t *testing.T) {
	got := lastLines("one\ntwo\nthree\nfour", 2)
	if got != "three\nfour" {
		t.Fatalf("lastLines=%q", got)
	}
}
