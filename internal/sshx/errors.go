package sshx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Kind classifies SSH failures so the UI can show an actionable message.
type Kind string

const (
	KindUnreachable Kind = "unreachable" // connection refused, timeout, DNS
	KindAuth        Kind = "auth"        // permission denied
	KindAgent       Kind = "agent"       // agent socket missing or has no keys
	KindHostKey     Kind = "host_key"    // pinned key mismatch / unknown key
	KindTimeout     Kind = "timeout"     // our own deadline expired
	KindRemote      Kind = "remote"      // the remote command exited non-zero
	KindSSH         Kind = "ssh"         // any other ssh failure
)

// Error is an SSH or remote-command failure. Message and Hint are safe to
// show and log; Stderr may contain host details and is only for debug logs
// and for callers that parse remote errors (e.g. tmux).
type Error struct {
	Kind     Kind
	Message  string
	Hint     string
	ExitCode int
	Timeout  time.Duration
	Stderr   string
}

func (e *Error) Error() string {
	if e.Kind == KindRemote {
		return fmt.Sprintf("remote command exited %d", e.ExitCode)
	}
	return e.Message
}

// IsKind reports whether err is an *Error of kind k.
func IsKind(err error, k Kind) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == k
}

// agentState describes the ssh-agent the container was given.
type agentState int

const (
	agentOK agentState = iota
	agentMissing
	agentEmpty
)

// classify maps ssh's exit status 255 + stderr to an actionable error.
func classify(ctx context.Context, stderr string, agent func(context.Context) agentState) *Error {
	s := strings.ToLower(stderr)
	e := &Error{ExitCode: 255, Stderr: stderr}
	switch {
	case strings.Contains(s, "remote host identification has changed"),
		strings.Contains(s, "host key verification failed"),
		strings.Contains(s, "host key for") && strings.Contains(s, "has changed"),
		strings.Contains(s, "no matching host key type found"):
		e.Kind = KindHostKey
		e.Message = "the host's SSH host key doesn't match the key hostbud pinned"
		e.Hint = "The files mounted from /etc/ssh/ssh_host_*_key.pub don't belong to the machine at HOSTBUD_HOST_ADDR. " +
			"If the host's keys were regenerated, redeploy (make deploy) to re-pin them; otherwise check HOSTBUD_HOST_ADDR."
	case strings.Contains(s, "connection refused"):
		e.Kind = KindUnreachable
		e.Message = "can't reach sshd on the host (connection refused)"
		e.Hint = "Check that sshd is running on the host: `sudo systemctl status ssh` (install with `sudo apt install openssh-server`)."
	case strings.Contains(s, "could not resolve hostname"), strings.Contains(s, "name or service not known"):
		e.Kind = KindUnreachable
		e.Message = "can't resolve the host's address"
		e.Hint = "Check HOSTBUD_HOST_ADDR in .env (default host.docker.internal)."
	case strings.Contains(s, "connection timed out"), strings.Contains(s, "operation timed out"),
		strings.Contains(s, "no route to host"), strings.Contains(s, "network is unreachable"),
		strings.Contains(s, "connection closed by"), strings.Contains(s, "connection reset"):
		e.Kind = KindUnreachable
		e.Message = "can't reach sshd on the host"
		e.Hint = "Check that sshd is running and that the Docker network can reach the host (HOSTBUD_HOST_ADDR)."
	case strings.Contains(s, "permission denied"), strings.Contains(s, "too many authentication failures"):
		switch agent(ctx) {
		case agentMissing:
			e.Kind = KindAgent
			e.Message = "no ssh-agent socket in the container"
			e.Hint = "Set HOST_SSH_AUTH_SOCK in .env to the host's stable agent socket (see README) and run `make deploy`."
		case agentEmpty:
			e.Kind = KindAgent
			e.Message = "the ssh-agent has no keys loaded"
			e.Hint = "Load the hostbud key on the host: `ssh-add ~/.ssh/hostbud_ed25519` " +
				"(or `systemctl --user start hostbud-ssh-add.service`, see README)."
		default:
			e.Kind = KindAuth
			e.Message = "the host refused hostbud's SSH key (permission denied)"
			e.Hint = "Add ~/.ssh/hostbud_ed25519.pub to ~/.ssh/authorized_keys on the host with the " +
				`from="172.16.0.0/12" restriction (see README), and check HOST_SSH_USER.`
		}
	default:
		e.Kind = KindSSH
		e.Message = "ssh to the host failed"
		e.Hint = "Run `make logs` with HOSTBUD_LOG_LEVEL=debug for ssh's error output."
	}
	return e
}
