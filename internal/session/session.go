// Package session is the single entry point for creating, renaming and
// killing tmux sessions (docs/ARCHITECTURE.md §5.2, §10: v2 creates agent
// sessions through the same Create, with env vars and a start command).
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hostbud/internal/inventory"
	"hostbud/internal/sshx"
	"hostbud/internal/tmux"
)

// Code classifies errors for the API (HTTP status) and UI.
type Code string

const (
	CodeInvalid        Code = "invalid"         // bad name, path or env
	CodeNotFound       Code = "not_found"       // no such session
	CodeUnknownMachine Code = "unknown_machine" // no such machine
	CodeDuplicate      Code = "duplicate"       // name already taken
	CodePathNotFound   Code = "path_not_found"  // start directory missing
	CodeTmuxMissing    Code = "tmux_missing"
	CodeUnavailable    Code = "unavailable" // host unreachable / not probed yet
	CodeInternal       Code = "internal"
	CodeTmuxVersion    Code = "tmux_version"
	CodeTimeout        Code = "timeout"
)

// Error is an actionable, user-facing error.
type Error struct {
	Code    Code
	Message string
	Hint    string
}

func (e *Error) Error() string { return e.Message }

func errorf(code Code, hint, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Hint: hint}
}

// Spec describes a session to create.
type Spec struct {
	Machine      string
	Name         string            // optional: defaults to the directory's last path segment
	Path         string            // optional: defaults to the home dir; "~/x" allowed
	Env          map[string]string // needs tmux ≥ 3.2; sent through stdin, never in an argv
	StartCommand string            // optional: a command line run instead of the shell (UI)
	StartArgv    []string          // optional: a program and its arguments, quoted by sshx (v2 runs)
}

// InputExecutor runs a remote command with stdin (sshx.Client.ExecInput).
// Sessions with env vars are created through it, so the values (a run
// token) never show in a process list.
type InputExecutor interface {
	ExecInput(ctx context.Context, machine string, input []byte, args ...string) ([]byte, error)
}

// CopyModeState is the state reported by tmux after a copy-mode action.
type CopyModeState struct {
	InMode         bool `json:"inMode"`
	ScrollPosition int  `json:"scrollPosition"`
	HistorySize    int  `json:"historySize"`
}

// WindowsState is an on-demand view of tmux windows and panes. This state is
// intentionally transient and isn't published on the hostbud event bus.
type WindowsState struct {
	Windows   []tmux.Window `json:"windows"`
	Truncated bool          `json:"truncated"`
}

// Tracker is what the service needs from the inventory of a machine.
type Tracker interface {
	Snapshot() (inventory.Machine, []tmux.Session)
	Refresh(ctx context.Context) error
}

// Service creates, renames and kills sessions. The API must have the user's
// confirmation before calling Kill.
type Service struct {
	exec     inventory.Executor
	machines map[string]Tracker
	log      *slog.Logger
	hooks    LifecycleHooks
}

// LifecycleHooks synchronize metadata after a remote session mutation.
// RenameSessionLink and EndSessionLink run before inventory refresh publishes
// its complete changed snapshot.
type LifecycleHooks interface {
	RenameSessionLink(context.Context, string, string, string) error
	EndSessionLink(context.Context, string, string) error
}

// New returns a Service for the given machines (v1: just the host).
func New(exec inventory.Executor, machines map[string]Tracker, log *slog.Logger, hooks ...LifecycleHooks) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &Service{exec: exec, machines: machines, log: log}
	if len(hooks) > 0 {
		s.hooks = hooks[0]
	}
	return s
}

// ready returns the machine's tracker and state, or an error if the machine
// can't take commands right now.
func (s *Service) ready(machine string) (Tracker, inventory.Machine, []tmux.Session, error) {
	t, ok := s.machines[machine]
	if !ok {
		return nil, inventory.Machine{}, nil, errorf(CodeUnknownMachine, "", "unknown machine %q", machine)
	}
	m, sessions := t.Snapshot()
	switch m.Status {
	case inventory.StatusOK:
		return t, m, sessions, nil
	case inventory.StatusTmuxMissing:
		return nil, m, nil, &Error{Code: CodeTmuxMissing, Message: m.Error, Hint: m.Hint}
	case inventory.StatusUnreachable:
		return nil, m, nil, &Error{Code: CodeUnavailable, Message: m.Error, Hint: m.Hint}
	default:
		return nil, m, nil, errorf(CodeUnavailable, "hostbud is still connecting; try again in a moment.", "the host isn't ready yet")
	}
}

// Create creates a detached session and returns its name.
func (s *Service) Create(ctx context.Context, spec Spec) (string, error) {
	t, m, sessions, err := s.ready(spec.Machine)
	if err != nil {
		return "", err
	}
	dir, err := resolvePath(spec.Path, m.Home)
	if err != nil {
		return "", err
	}

	baseName := spec.Name
	if baseName == "" {
		baseName = defaultName(dir)
	} else if err := tmux.ValidateName(baseName); err != nil {
		return "", errorf(CodeInvalid, "Use letters, digits, '-' and '_' only (up to 64 characters).",
			"invalid session name %q", baseName)
	}
	name := uniqueName(baseName, sessions)

	if _, err := s.exec.Exec(ctx, spec.Machine, "test", "-d", dir); err != nil {
		var e *sshx.Error
		if errors.As(err, &e) && e.Kind == sshx.KindRemote {
			return "", errorf(CodePathNotFound, "Pick an existing directory, or create it first.",
				"directory %s doesn't exist on the host", dir)
		}
		return "", s.remoteError(err)
	}

	version, _ := tmux.ParseVersion(m.TmuxVersion)
	// A name may race with a session created after the inventory snapshot.
	// Reserve each collision locally and derive the next suffix from the
	// original requested name, so a typed "work-1" becomes "work-1-1".
	reserved := append([]tmux.Session(nil), sessions...)
	for attempt := 0; ; attempt++ {
		ns := tmux.NewSession{Name: name, Path: dir, Env: spec.Env, StartCommand: spec.StartCommand, StartArgv: spec.StartArgv}
		run, err := s.newSessionRunner(ns, version)
		if errors.Is(err, tmux.ErrEnvUnsupported) {
			return "", errorf(CodeInvalid, "Upgrade tmux on the host to 3.2 or newer.", "%s", err)
		}
		if err != nil {
			return "", errorf(CodeInvalid, "", "%s", err)
		}
		err = run(ctx, spec.Machine)
		if isServerGone(err) {
			// Transient: the new server raced one that was still exiting
			// (e.g. right after a kill-server). One retry after a pause.
			select {
			case <-time.After(250 * time.Millisecond):
			case <-ctx.Done():
				return "", s.remoteError(ctx.Err())
			}
			err = run(ctx, spec.Machine)
		}
		if err == nil {
			break
		}
		if isDuplicate(err) {
			if attempt < 20 {
				reserved = append(reserved, tmux.Session{Name: name})
				name = uniqueName(baseName, reserved)
				continue
			}
			return "", errorf(CodeDuplicate, "Pick another name, or open the existing session.",
				"a session named %q already exists", name)
		}
		return "", s.remoteError(err)
	}

	s.log.Info("session created", "machine", spec.Machine)
	s.log.Debug("session created details", "machine", spec.Machine, "session", name)
	s.refresh(ctx, t)
	return name, nil
}

// SendCommand dispatches one user-authored command to an existing session's
// active pane. It sends the command as a single tmux key argument, so shell
// metacharacters are interpreted by that pane's shell as entered by the user.
func (s *Service) SendCommand(ctx context.Context, machine, name, command string) error {
	if err := tmux.ValidateName(name); err != nil {
		return errorf(CodeInvalid, "Choose an existing session.", "invalid session name")
	}
	if strings.TrimSpace(command) == "" || len(command) > 16<<10 || strings.ContainsAny(command, "\x00\n\r") {
		return errorf(CodeInvalid, "Use one command line (up to 16 KiB).", "invalid session command")
	}
	t, ok := s.machines[machine]
	if !ok {
		return errorf(CodeUnknownMachine, "", "unknown machine")
	}
	if err := t.Refresh(ctx); err != nil {
		return s.remoteError(err)
	}
	_, _, sessions, err := s.ready(machine)
	if err != nil {
		return err
	}
	found := false
	for _, item := range sessions {
		if item.Name == name {
			found = true
			break
		}
	}
	if !found {
		return errorf(CodeNotFound, "Choose a session that is still open.", "session is no longer available")
	}
	target := "=" + name + ":"
	_, err = s.exec.Exec(ctx, machine, "tmux", "send-keys", "-l", "-t", target, command, ";", "send-keys", "-t", target, "Enter")
	if err != nil {
		return s.remoteError(err)
	}
	return nil
}

// newSessionRunner returns how to run new-session: with env vars, as a
// script on stdin (values stay out of every argv); otherwise as plain argv.
func (s *Service) newSessionRunner(ns tmux.NewSession, version tmux.Version) (func(context.Context, string) error, error) {
	if len(ns.Env) == 0 {
		args, err := tmux.NewSessionArgs(ns, version)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, machine string) error {
			_, err := s.exec.Exec(ctx, machine, args...)
			return err
		}, nil
	}
	in, ok := s.exec.(InputExecutor)
	if !ok {
		return nil, errors.New("session env needs an executor with stdin")
	}
	args, script, err := tmux.NewSessionScript(ns, version)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, machine string) error {
		_, err := in.ExecInput(ctx, machine, script, args...)
		return err
	}, nil
}

// SanitizeName turns any text into a valid session name base the way
// directory names are ("my.app" → "my-app"); empty becomes "session".
func SanitizeName(text string) string {
	name := strings.Trim(invalidNameChars.ReplaceAllString(text, "-"), "-")
	if len(name) > 60 {
		name = strings.TrimRight(name[:60], "-")
	}
	if name == "" {
		name = "session"
	}
	return name
}

// Rename renames a session.
func (s *Service) Rename(ctx context.Context, machine, from, to string) error {
	t, _, _, err := s.ready(machine)
	if err != nil {
		return err
	}
	args, err := tmux.RenameSessionArgs(from, to)
	if err != nil {
		return errorf(CodeInvalid, "Use letters, digits, '-' and '_' only (up to 64 characters).", "invalid session name")
	}
	if _, err := s.exec.Exec(ctx, machine, args...); err != nil {
		switch {
		case isDuplicate(err):
			return errorf(CodeDuplicate, "Pick another name.", "a session named %q already exists", to)
		case isNotFound(err):
			return errorf(CodeNotFound, "It may have been closed; the list refreshes automatically.", "no session named %q", from)
		}
		return s.remoteError(err)
	}
	if s.hooks != nil {
		if err := s.hooks.RenameSessionLink(ctx, machine, from, to); err != nil {
			s.log.Warn("session renamed but project link update failed", "machine", machine, "err", err)
		}
	}
	s.log.Info("session renamed", "machine", machine)
	s.log.Debug("session renamed details", "machine", machine, "from", from, "to", to)
	s.refresh(ctx, t)
	return nil
}

// Kill kills a session. The caller must have the user's confirmation.
func (s *Service) Kill(ctx context.Context, machine, name string) error {
	t, _, _, err := s.ready(machine)
	if err != nil {
		return err
	}
	if err := s.kill(ctx, machine, name); err != nil {
		return err
	}
	s.refresh(ctx, t)
	return nil
}

// MaxKillMany caps the sessions one KillMany call accepts.
const MaxKillMany = 256

// KillFailure is one session KillMany couldn't kill.
type KillFailure struct {
	Name    string `json:"name"`
	Message string `json:"error"`
	Hint    string `json:"hint,omitempty"`
}

// KillMany kills several sessions and refreshes the inventory once at the
// end: a refresh lists every pane on the host, so refreshing after each kill
// made killing a project's sessions take seconds. A failed kill doesn't stop
// the rest, but once the host is unreachable the remaining ones fail with the
// same error instead of each waiting for its own timeout. The caller must have
// the user's confirmation.
func (s *Service) KillMany(ctx context.Context, machine string, names []string) (killed []string, failed []KillFailure, err error) {
	t, _, _, err := s.ready(machine)
	if err != nil {
		return nil, nil, err
	}
	if len(names) == 0 || len(names) > MaxKillMany {
		return nil, nil, errorf(CodeInvalid, "", "send between 1 and %d session names", MaxKillMany)
	}
	for _, name := range names {
		if tmux.ValidateName(name) != nil {
			return nil, nil, errorf(CodeInvalid, "", "invalid session name")
		}
	}
	killed = []string{}
	var unreachable *Error
	for _, name := range names {
		var kerr error
		if unreachable != nil {
			kerr = unreachable
		} else {
			kerr = s.kill(ctx, machine, name)
		}
		if kerr == nil {
			killed = append(killed, name)
			continue
		}
		var e *Error
		if !errors.As(kerr, &e) {
			e = &Error{Code: CodeInternal, Message: kerr.Error()}
		}
		if e.Code == CodeUnavailable || e.Code == CodeTimeout {
			unreachable = e
		}
		failed = append(failed, KillFailure{Name: name, Message: e.Message, Hint: e.Hint})
	}
	if len(killed) > 0 {
		s.refresh(ctx, t)
	}
	return killed, failed, nil
}

// kill kills one session without refreshing the inventory.
func (s *Service) kill(ctx context.Context, machine, name string) error {
	args, err := tmux.KillSessionArgs(name)
	if err != nil {
		return errorf(CodeInvalid, "", "invalid session name")
	}
	if _, err := s.exec.Exec(ctx, machine, args...); err != nil {
		if isNotFound(err) {
			return errorf(CodeNotFound, "It may have been closed already.", "no session named %q", name)
		}
		return s.remoteError(err)
	}
	if s.hooks != nil {
		if err := s.hooks.EndSessionLink(ctx, machine, name); err != nil {
			s.log.Warn("session ended but project link cleanup failed", "machine", machine, "err", err)
		}
	}
	s.log.Info("session killed", "machine", machine)
	s.log.Debug("session killed details", "machine", machine, "session", name)
	return nil
}

// CopyMode runs one allowlisted side-channel copy-mode operation.
func (s *Service) CopyMode(ctx context.Context, machine, name string, action tmux.CopyAction, lines int) (CopyModeState, error) {
	_, m, sessions, err := s.ready(machine)
	if err != nil {
		return CopyModeState{}, err
	}
	if err := tmux.ValidateName(name); err != nil {
		return CopyModeState{}, errorf(CodeInvalid, "Use letters, digits, '-' and '_' only (up to 64 characters).", "invalid session name")
	}
	found := false
	for _, v := range sessions {
		if v.Name == name {
			found = true
			break
		}
	}
	if !found {
		return CopyModeState{}, errorf(CodeNotFound, "It may have been closed; the list refreshes automatically.", "no session named %q", name)
	}
	version, _ := tmux.ParseVersion(m.TmuxVersion)
	if !version.AtLeast(2, 4) {
		return CopyModeState{}, errorf(CodeTmuxVersion, "Upgrade tmux on the host to 2.4 or newer.", "scrolling needs tmux 2.4 or newer on the host (found %s)", m.TmuxVersion)
	}
	args, err := tmux.CopyModeArgs(name, action, lines)
	if err != nil {
		return CopyModeState{}, errorf(CodeInvalid, "Use a supported copy-mode action and 1–500 lines.", "%s", err)
	}
	if (action == tmux.CopyWheelUp || action == tmux.CopyWheelDown) && version.AtLeast(3, 1) {
		if args, err = s.wheelArgs(ctx, machine, name, action, lines, args); err != nil {
			return CopyModeState{}, err
		}
	}
	out, err := s.exec.Exec(ctx, machine, args...)
	if err != nil {
		var remote *sshx.Error
		if errors.As(err, &remote) && remote.Kind == sshx.KindRemote && tmux.IsNotInCopyMode(remote.Stderr) {
			out, err = s.exec.Exec(ctx, machine, "tmux", "display-message", "-p", "-t", "="+name+":", "#{pane_in_mode}\t#{scroll_position}\t#{history_size}")
		}
		if err != nil {
			if isNotFound(err) {
				return CopyModeState{}, errorf(CodeNotFound, "It may have been closed; the list refreshes automatically.", "no session named %q", name)
			}
			return CopyModeState{}, s.remoteError(err)
		}
	}
	inMode, pos, size, err := tmux.ParseCopyModeState(string(out))
	if err != nil {
		return CopyModeState{}, errorf(CodeInternal, "Try again; if this continues, check the host's tmux version.", "could not read copy-mode state")
	}
	return CopyModeState{InMode: inMode, ScrollPosition: pos, HistorySize: size}, nil
}

// ListWindows reads a session's window and pane layout on demand.
func (s *Service) ListWindows(ctx context.Context, machine, name string) (WindowsState, error) {
	if _, _, _, err := s.ready(machine); err != nil {
		return WindowsState{}, err
	}
	if err := tmux.ValidateName(name); err != nil {
		return WindowsState{}, errorf(CodeInvalid, "Use letters, digits, '-' and '_' only (up to 64 characters).", "invalid session name")
	}
	args, _ := tmux.ListWindowsArgs(name)
	out, err := s.exec.Exec(ctx, machine, args...)
	if err != nil {
		if isNotFound(err) {
			return WindowsState{}, errorf(CodeNotFound, "It may have been closed; the list refreshes automatically.", "no session named %q", name)
		}
		return WindowsState{}, s.remoteError(err)
	}
	windows, truncated, err := tmux.ParseWindows(string(out))
	if err != nil {
		s.log.Warn("could not parse tmux window listing")
		return WindowsState{}, errorf(CodeInternal, "Try again; if this continues, check the host's tmux version.", "could not read session windows")
	}
	return WindowsState{Windows: windows, Truncated: truncated}, nil
}

// SelectWindow selects a window and optional pane after verifying both ids
// belong to the named session. No event is emitted; attached tmux clients see
// the same selection directly.
func (s *Service) SelectWindow(ctx context.Context, machine, name, windowID, paneID string) (WindowsState, error) {
	if _, _, _, err := s.ready(machine); err != nil {
		return WindowsState{}, err
	}
	if err := tmux.ValidateName(name); err != nil {
		return WindowsState{}, errorf(CodeInvalid, "Use letters, digits, '-' and '_' only (up to 64 characters).", "invalid session name")
	}
	if _, err := tmux.SelectArgs(name, windowID, paneID); err != nil {
		return WindowsState{}, errorf(CodeInvalid, "Choose a valid window and pane.", "invalid tmux window or pane id")
	}
	before, err := s.ListWindows(ctx, machine, name)
	if err != nil {
		return WindowsState{}, err
	}
	found := false
	for _, window := range before.Windows {
		if window.ID != windowID {
			continue
		}
		if paneID == "" {
			found = true
			break
		}
		for _, pane := range window.Panes {
			if pane.ID == paneID {
				found = true
				break
			}
		}
	}
	if !found {
		return WindowsState{}, errorf(CodeNotFound, "The window or pane is no longer in this session.", "tmux window or pane not found in session")
	}
	args, _ := tmux.SelectArgs(name, windowID, paneID)
	out, err := s.exec.Exec(ctx, machine, args...)
	if err != nil {
		if isNotFound(err) {
			return WindowsState{}, errorf(CodeNotFound, "The session or window may have been closed.", "tmux target not found")
		}
		return WindowsState{}, s.remoteError(err)
	}
	windows, truncated, err := tmux.ParseWindows(string(out))
	if err != nil {
		s.log.Warn("could not parse tmux window listing after selection")
		return WindowsState{}, errorf(CodeInternal, "Try again; if this continues, check the host's tmux version.", "could not read session windows")
	}
	return WindowsState{Windows: windows, Truncated: truncated}, nil
}

func (s *Service) refresh(ctx context.Context, t Tracker) {
	if err := t.Refresh(ctx); err != nil {
		s.log.Debug("refresh after mutation", "err", err)
	}
}

// remoteError maps an Exec failure to an actionable error.
func (s *Service) remoteError(err error) error {
	var e *sshx.Error
	if errors.As(err, &e) {
		if e.Kind == sshx.KindTimeout {
			return errorf(CodeTimeout, e.Hint, "%s", e.Message)
		}
		if e.Kind == sshx.KindRemote {
			if e.ExitCode == 127 {
				return errorf(CodeTmuxMissing, "Install it with `sudo apt install tmux`.", "tmux not found on the host")
			}
			// Unexpected: worth a warning. stderr may hold paths: debug only.
			s.log.Warn("tmux command failed", "exit", e.ExitCode)
			s.log.Debug("tmux command failed", "exit", e.ExitCode, "stderr", e.Stderr)
			return errorf(CodeInternal, "Check `make logs` with HOSTBUD_LOG_LEVEL=debug.",
				"tmux failed on the host (exit %d)", e.ExitCode)
		}
		return &Error{Code: CodeUnavailable, Message: e.Message, Hint: e.Hint}
	}
	return errorf(CodeInternal, "", "%s", err)
}

func stderrOf(err error) string {
	var e *sshx.Error
	if errors.As(err, &e) && e.Kind == sshx.KindRemote {
		return e.Stderr
	}
	return ""
}

func isDuplicate(err error) bool { return strings.Contains(stderrOf(err), "duplicate session") }
func isNotFound(err error) bool {
	return strings.Contains(stderrOf(err), "can't find session") || tmux.NoServer(stderrOf(err))
}
func isServerGone(err error) bool {
	return strings.Contains(stderrOf(err), "server exited unexpectedly") ||
		strings.Contains(stderrOf(err), "lost server")
}

// resolvePath expands "", "~" and "~/…" against home and requires an
// absolute result.
func resolvePath(p, home string) (string, error) {
	p = strings.TrimSpace(p)
	switch {
	case p == "" || p == "~":
		p = home
	case strings.HasPrefix(p, "~/"):
		p = strings.TrimRight(home, "/") + "/" + p[2:]
	}
	if p == "" {
		return "", errorf(CodeUnavailable, "hostbud is still connecting; try again in a moment.", "the host's home directory isn't known yet")
	}
	if !strings.HasPrefix(p, "/") {
		return "", errorf(CodeInvalid, "Use an absolute path like /srv/app, or ~/app.", "path %q isn't absolute", p)
	}
	if strings.ContainsAny(p, "\x00\n") {
		return "", errorf(CodeInvalid, "", "path contains invalid characters")
	}
	return path.Clean(p), nil
}

var invalidNameChars = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// defaultName is the directory's last path segment ("/root/docs/dev" →
// "dev"), with characters tmux names can't hold replaced ("my.app" →
// "my-app"); "/" → "root".
func defaultName(dir string) string {
	base := path.Base(dir)
	if base == "/" {
		base = "root"
	}
	return SanitizeName(base)
}

// uniqueName returns base, or base-1, base-2, … if taken.
func uniqueName(base string, sessions []tmux.Session) string {
	taken := map[string]bool{}
	for _, s := range sessions {
		taken[s.Name] = true
	}
	if !taken[base] {
		return base
	}
	for n := 1; ; n++ {
		suffix := "-" + strconv.Itoa(n)
		trimmed := base
		if len(trimmed)+len(suffix) > 64 {
			trimmed = trimmed[:64-len(suffix)]
		}
		name := trimmed + suffix
		if !taken[name] {
			return name
		}
	}
}

// wheelArgs follows tmux's default wheel binding: a mouse-aware app outside a
// mode (Claude Code, Codex) gets wheel reports and scrolls its own history;
// otherwise copyArgs scrolls tmux's history in copy mode.
func (s *Service) wheelArgs(ctx context.Context, machine, name string, action tmux.CopyAction, lines int, copyArgs []string) ([]string, error) {
	stateArgs, _ := tmux.WheelStateArgs(name)
	out, err := s.exec.Exec(ctx, machine, stateArgs...)
	if err != nil {
		if isNotFound(err) {
			return nil, errorf(CodeNotFound, "It may have been closed; the list refreshes automatically.", "no session named %q", name)
		}
		return nil, s.remoteError(err)
	}
	state, err := tmux.ParseWheelState(string(out))
	if err != nil {
		return nil, errorf(CodeInternal, "Try again; if this continues, check the host's tmux version.", "could not read terminal state")
	}
	if !state.ForwardToApp() {
		return copyArgs, nil
	}
	return tmux.AppWheelArgs(name, action, lines, state)
}
