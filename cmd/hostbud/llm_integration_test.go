//go:build integration

package main

import (
	"context"
	"io"
	"testing"

	"hostbud/internal/events"
	"hostbud/internal/llm"
	"hostbud/internal/sshx"
	"hostbud/internal/testenv"
)

type recordingCapture struct {
	client *sshx.Client
	calls  int
}

func (r *recordingCapture) ExecTo(ctx context.Context, machine string, w io.Writer, args ...string) error {
	r.calls++
	return r.client.ExecTo(ctx, machine, w, args...)
}

// V2-M5 T1/T2: without provider configuration app wiring constructs no
// supervisor and makes no SSH capture call against the throwaway target.
func TestIntegrationNoProviderConstructsNoSupervisorOrCapture(t *testing.T) {
	client := testenv.Connected(t, testenv.SSHD)
	ssh := &recordingCapture{client: client}
	supervisor, status := newLLMSupervisor(llm.Config{}, nil, ssh, events.NewBus(), nil)
	if supervisor != nil || status.Enabled {
		t.Fatalf("off config constructed supervisor=%v status=%+v", supervisor, status)
	}
	if ssh.calls != 0 {
		t.Fatalf("provider-off startup made %d SSH calls", ssh.calls)
	}
}
