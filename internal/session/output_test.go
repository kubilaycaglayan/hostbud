package session

import (
	"context"
	"slices"
	"testing"

	"hostbud/internal/sshx"
)

func TestOutput(t *testing.T) {
	f := &fakeExec{}
	f.handler = func(args []string) error {
		if slices.Contains(args, "display-message") {
			f.output = []byte("0 3\n")
		} else {
			f.output = []byte("old history   \n\x1b[31mred\x1b[0m\n\n")
		}
		return nil
	}
	svc := newSvc(f, okHost("work"))
	out, err := svc.Output(context.Background(), "host", "work")
	if err != nil || out != "old history\n\x1b[31mred\x1b[0m\n" || len(f.calls) != 2 {
		t.Fatalf("output: %q %v", out, err)
	}
	if _, err := svc.Output(context.Background(), "host", "bad;name"); code(err) != CodeInvalid {
		t.Fatalf("invalid name: %v", err)
	}
	if _, err := svc.Output(context.Background(), "missing", "work"); code(err) != CodeUnknownMachine {
		t.Fatalf("unknown machine: %v", err)
	}
	if len(f.calls) != 2 {
		t.Fatal("invalid request executed remotely")
	}
}

func TestOutputAlternateScreen(t *testing.T) {
	f := &fakeExec{}
	f.handler = func(args []string) error {
		switch {
		case slices.Contains(args, "display-message"):
			f.output = []byte("1 2\n")
		case slices.Contains(args, "-a"):
			f.output = nil
			return &sshx.Error{Kind: sshx.KindRemote, ExitCode: 1, Stderr: "no alternate screen\n"}
		default:
			f.output = []byte("shell\n")
		}
		return nil
	}
	svc := newSvc(f, okHost("work"))
	out, err := svc.Output(context.Background(), "host", "work")
	if err != nil || out != "shell\n" || len(f.calls) != 3 {
		t.Fatalf("fallback after full-screen app exit: %q %v %v", out, err, f.calls)
	}
}
