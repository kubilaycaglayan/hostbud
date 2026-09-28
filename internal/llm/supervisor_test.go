package llm

import (
	"strings"
	"testing"
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

func TestLastLinesKeepsOnlyNewestLines(t *testing.T) {
	got := lastLines("one\ntwo\nthree\nfour", 2)
	if got != "three\nfour" {
		t.Fatalf("lastLines=%q", got)
	}
}
