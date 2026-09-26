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
	Env          map[string]string // needs tmux ≥ 3.2
	StartCommand string            // optional: run instead of the shell
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
}

// New returns a Service for the given machines (v1: just the host).
func New(exec inventory.Executor, machines map[string]Tracker, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{exec: exec, machines: machines, log: log}
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

	auto := spec.Name == ""
	name := spec.Name
	if auto {
		name = uniqueName(defaultName(dir), sessions)
	} else if err := tmux.ValidateName(name); err != nil {
		return "", errorf(CodeInvalid, "Use letters, digits, '-' and '_' only (up to 64 characters).",
			"invalid session name %q", name)
	}

	if _, err := s.exec.Exec(ctx, spec.Machine, "test", "-d", dir); err != nil {
		var e *sshx.Error
		if errors.As(err, &e) && e.Kind == sshx.KindRemote {
			return "", errorf(CodePathNotFound, "Pick an existing directory, or create it first.",
				"directory %s doesn't exist on the host", dir)
		}
		return "", s.remoteError(err)
	}

	version, _ := tmux.ParseVersion(m.TmuxVersion)
	// An auto-derived name may race with a session created meanwhile:
	// retry with the next suffix.
	for attempt := 0; ; attempt++ {
		args, err := tmux.NewSessionArgs(tmux.NewSession{
			Name: name, Path: dir, Env: spec.Env, StartCommand: spec.StartCommand,
		}, version)
		if errors.Is(err, tmux.ErrEnvUnsupported) {
			return "", errorf(CodeInvalid, "Upgrade tmux on the host to 3.2 or newer.", "%s", err)
		}
		if err != nil {
			return "", errorf(CodeInvalid, "", "%s", err)
		}
		_, err = s.exec.Exec(ctx, spec.Machine, args...)
		if isServerGone(err) {
			// Transient: the new server raced one that was still exiting
			// (e.g. right after a kill-server). One retry after a pause.
			select {
			case <-time.After(250 * time.Millisecond):
			case <-ctx.Done():
				return "", s.remoteError(ctx.Err())
			}
			_, err = s.exec.Exec(ctx, spec.Machine, args...)
		}
		if err == nil {
			break
		}
		if isDuplicate(err) {
			if auto && attempt < 20 {
				name = nextName(name)
				continue
			}
			return "", errorf(CodeDuplicate, "Pick another name, or open the existing session.",
				"a session named %q already exists", name)
		}
		return "", s.remoteError(err)
	}

	s.log.Info("session created", "machine", spec.Machine, "session", name)
	s.refresh(ctx, t)
	return name, nil
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
	s.log.Info("session renamed", "machine", machine, "from", from, "to", to)
	s.refresh(ctx, t)
	return nil
}

// Kill kills a session. The caller must have the user's confirmation.
func (s *Service) Kill(ctx context.Context, machine, name string) error {
	t, _, _, err := s.ready(machine)
	if err != nil {
		return err
	}
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
	s.log.Info("session killed", "machine", machine, "session", name)
	s.refresh(ctx, t)
	return nil
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
func isNotFound(err error) bool  { return strings.Contains(stderrOf(err), "can't find session") }
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
	name := strings.Trim(invalidNameChars.ReplaceAllString(base, "-"), "-")
	if len(name) > 60 {
		name = name[:60]
	}
	if name == "" {
		name = "session"
	}
	return name
}

// uniqueName returns base, or base-1, base-2, … if taken.
func uniqueName(base string, sessions []tmux.Session) string {
	taken := map[string]bool{}
	for _, s := range sessions {
		taken[s.Name] = true
	}
	name := base
	for n := 1; taken[name]; n++ {
		name = base + "-" + strconv.Itoa(n)
	}
	return name
}

var suffixRE = regexp.MustCompile(`^(.*)-(\d+)$`)

// nextName bumps a "-<n>" suffix (or adds "-1").
func nextName(name string) string {
	if m := suffixRE.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[2])
		return m[1] + "-" + strconv.Itoa(n+1)
	}
	return name + "-1"
}
