package tmux

import (
	"reflect"
	"testing"
)

func TestCaptureOutputArgs(t *testing.T) {
	capture := []string{"capture-pane", "-p", "-e", "-J"}
	base := []string{"env", "LC_ALL=C.UTF-8", "tmux"}
	cases := []struct {
		alternate bool
		history   int
		want      []string
	}{
		{false, 5, concat(base, capture, []string{"-S", "-", "-t", "=work:"})},
		{true, 0, concat(base, capture, []string{"-a", "-t", "=work:", ";"}, capture, []string{"-t", "=work:"})},
		{true, 5, concat(base, capture, []string{"-S", "-", "-E", "-1", "-t", "=work:", ";"}, capture, []string{"-a", "-t", "=work:", ";"}, capture, []string{"-t", "=work:"})},
	}
	for _, c := range cases {
		args, err := CaptureOutputArgs("work", c.alternate, c.history)
		if err != nil || !reflect.DeepEqual(args, c.want) {
			t.Fatalf("capture args %v/%d: %v %v", c.alternate, c.history, args, err)
		}
	}
	if _, err := CaptureOutputArgs("work;bad", false, 0); err == nil {
		t.Fatal("invalid name accepted")
	}
	if _, err := OutputStateArgs("work;bad"); err == nil {
		t.Fatal("invalid name accepted")
	}
}

func concat(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestParseOutputState(t *testing.T) {
	if alt, h, err := ParseOutputState("1 42\n"); err != nil || !alt || h != 42 {
		t.Fatalf("parse: %v %d %v", alt, h, err)
	}
	if alt, h, err := ParseOutputState("0 0\n"); err != nil || alt || h != 0 {
		t.Fatalf("parse: %v %d %v", alt, h, err)
	}
	for _, bad := range []string{"", "1", "1 x", "1 -1", "1 2 3"} {
		if _, _, err := ParseOutputState(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestTrimOutput(t *testing.T) {
	in := "prompt$ ls   \n\x1b[31mred\x1b[0m    \x1b[0m\n  indented\n\n   \n\x1b[0m  \n"
	want := "prompt$ ls\n\x1b[31mred\x1b[0m\x1b[0m\n  indented\n"
	if got := TrimOutput(in); got != want {
		t.Fatalf("trim: %q", got)
	}
	if got := TrimOutput("\n  \n"); got != "" {
		t.Fatalf("blank: %q", got)
	}
}
