package llm

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"hostbud/internal/store"
)

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

func TestLastLinesKeepsOnlyNewestLines(t *testing.T) {
	got := lastLines("one\ntwo\nthree\nfour", 2)
	if got != "three\nfour" {
		t.Fatalf("lastLines=%q", got)
	}
}
