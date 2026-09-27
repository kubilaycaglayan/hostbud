package session

import (
	"context"
	"errors"
	"strings"

	"hostbud/internal/sshx"
	"hostbud/internal/tmux"
)

// Output reads the active pane's retained history and current screen without
// entering copy mode, moving its cursor, or changing any tmux options.
func (s *Service) Output(ctx context.Context, machine, name string) (string, error) {
	if _, _, _, err := s.ready(machine); err != nil {
		return "", err
	}
	stateArgs, err := tmux.OutputStateArgs(name)
	if err != nil {
		return "", errorf(CodeInvalid, "Use letters, digits, '-' and '_' only (up to 64 characters).", "invalid session name")
	}
	state, err := s.exec.Exec(ctx, machine, stateArgs...)
	if err != nil {
		return "", s.outputError(err)
	}
	alternate, history, err := tmux.ParseOutputState(string(state))
	if err != nil {
		return "", errorf(CodeInternal, "Try again; if this continues, check the host's tmux version.", "could not read terminal state")
	}
	args, _ := tmux.CaptureOutputArgs(name, alternate, history)
	out, err := s.exec.Exec(ctx, machine, args...)
	var remote *sshx.Error
	if err != nil && alternate && errors.As(err, &remote) && strings.Contains(remote.Stderr, "no alternate screen") {
		// The full-screen app exited between the two calls.
		args, _ = tmux.CaptureOutputArgs(name, false, 0)
		out, err = s.exec.Exec(ctx, machine, args...)
	}
	if err != nil {
		return "", s.outputError(err)
	}
	return tmux.TrimOutput(string(out)), nil
}

func (s *Service) outputError(err error) error {
	if isNotFound(err) {
		return errorf(CodeNotFound, "It may have been closed; reopen a running session.", "session is no longer available")
	}
	return s.remoteError(err)
}
