// Package tmux builds tmux command lines (as argv for sshx.Exec) and parses
// their output. It never runs anything itself and never touches the user's
// tmux config or global options (docs/ARCHITECTURE.md §5).
package tmux

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

var nameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ErrInvalidName explains the session-name rule.
var ErrInvalidName = errors.New("session names may only contain letters, digits, '-' and '_' (1–64 characters)")

// ValidateName checks a session name against ^[A-Za-z0-9_-]{1,64}$ (tmux
// itself forbids '.' and ':'; we are stricter).
func ValidateName(name string) error {
	if !nameRE.MatchString(name) {
		return ErrInvalidName
	}
	return nil
}

// target is an exact-match session target (no prefix or pattern matching).
func target(name string) string { return "=" + name }

// Session is one line of list-sessions.
type Session struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	ProjectID string    `json:"projectId,omitempty"`
	Attached  int       `json:"attached"` // number of attached clients
	Windows   int       `json:"windows"`
	Created   time.Time `json:"created"`
	Activity  time.Time `json:"activity"`
}

// listFormat is ':'-separated. tmux never allows ':' in session names (it
// rewrites them), the other fields are ids and numbers, and the path comes
// last so colons in it can't shift fields. (Not tabs: without a UTF-8 locale,
// as in a non-interactive ssh session, tmux prints control characters as '_'.)
const listFormat = "#{session_id}:#{session_name}:#{session_attached}:#{session_windows}:#{session_created}:#{session_activity}:#{session_path}"

// ListSessions returns the argv for list-sessions.
func ListSessions() []string {
	return []string{"tmux", "list-sessions", "-F", listFormat}
}

// NoServer reports whether tmux's stderr means "no server running" (which
// is an empty list, not an error).
func NoServer(stderr string) bool {
	return strings.Contains(stderr, "no server running") ||
		strings.Contains(stderr, "error connecting to") && strings.Contains(stderr, "No such file or directory")
}

// ParseSessions parses list-sessions output in listFormat.
func ParseSessions(out string) ([]Session, error) {
	var sessions []Session
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, ":", 7)
		if len(f) != 7 {
			return nil, fmt.Errorf("unexpected list-sessions line with %d fields", len(f))
		}
		attached, err1 := strconv.Atoi(f[2])
		windows, err2 := strconv.Atoi(f[3])
		created, err3 := strconv.ParseInt(f[4], 10, 64)
		activity, err4 := strconv.ParseInt(f[5], 10, 64)
		if err := errors.Join(err1, err2, err3, err4); err != nil {
			return nil, fmt.Errorf("unexpected list-sessions line: %w", err)
		}
		sessions = append(sessions, Session{
			ID: f[0], Name: f[1], Attached: attached, Windows: windows,
			Created: time.Unix(created, 0).UTC(), Activity: time.Unix(activity, 0).UTC(), Path: f[6],
		})
	}
	return sessions, nil
}

// NewSession describes a session to create.
type NewSession struct {
	Name         string
	Path         string            // absolute start directory
	Env          map[string]string // -e KEY=VAL, only with tmux ≥ 3.2
	StartCommand string            // run by the default shell; empty = shell
}

// ErrEnvUnsupported means env vars were requested on tmux < 3.2.
var ErrEnvUnsupported = errors.New("tmux on the host is older than 3.2 and can't set session environment variables (-e)")

var envKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// NewSessionArgs returns the argv for new-session -d.
func NewSessionArgs(s NewSession, v Version) ([]string, error) {
	if err := ValidateName(s.Name); err != nil {
		return nil, err
	}
	if s.Path == "" {
		return nil, errors.New("new-session: path is required")
	}
	args := []string{"tmux", "new-session", "-d", "-s", s.Name, "-c", s.Path}
	if len(s.Env) > 0 {
		if !v.AtLeast(3, 2) {
			return nil, ErrEnvUnsupported
		}
		keys := make([]string, 0, len(s.Env))
		for k := range s.Env {
			if !envKeyRE.MatchString(k) {
				return nil, fmt.Errorf("invalid environment variable name %q", k)
			}
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			args = append(args, "-e", k+"="+s.Env[k])
		}
	}
	if s.StartCommand != "" {
		// One argument: tmux hands it to the user's shell, like typing it.
		args = append(args, s.StartCommand)
	}
	return args, nil
}

// RenameSessionArgs returns the argv for rename-session.
func RenameSessionArgs(from, to string) ([]string, error) {
	if err := errors.Join(ValidateName(from), ValidateName(to)); err != nil {
		return nil, ErrInvalidName
	}
	return []string{"tmux", "rename-session", "-t", target(from), to}, nil
}

// KillSessionArgs returns the argv for kill-session. Callers must have the
// user's confirmation.
func KillSessionArgs(name string) ([]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	return []string{"tmux", "kill-session", "-t", target(name)}, nil
}

// CopyAction is a closed set of side-channel copy-mode operations.
type CopyAction string

const (
	CopyEnter      CopyAction = "enter"
	CopyScrollUp   CopyAction = "scroll-up"
	CopyScrollDown CopyAction = "scroll-down"
	CopyPageUp     CopyAction = "page-up"
	CopyPageDown   CopyAction = "page-down"
	CopyTop        CopyAction = "top"
	CopyBottom     CopyAction = "bottom"
	CopyExit       CopyAction = "exit"
)

// CopyModeArgs builds an allowlisted copy-mode action followed by a state query.
func CopyModeArgs(name string, action CopyAction, lines int) ([]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	t := "=" + name + ":"
	var args []string
	switch action {
	case CopyEnter:
		args = []string{"tmux", "copy-mode", "-e", "-u", "-t", t}
	case CopyScrollUp, CopyScrollDown:
		if lines == 0 {
			lines = 1
		}
		if lines < 1 || lines > 500 {
			return nil, errors.New("lines must be between 1 and 500")
		}
		direction := "scroll-up"
		if action == CopyScrollDown {
			direction = "scroll-down"
		}
		args = []string{"tmux", "send-keys", "-X", "-N", strconv.Itoa(lines), "-t", t, direction}
	case CopyPageUp, CopyPageDown, CopyTop, CopyBottom, CopyExit:
		key := map[CopyAction]string{CopyPageUp: "page-up", CopyPageDown: "page-down", CopyTop: "history-top", CopyBottom: "history-bottom", CopyExit: "cancel"}[action]
		args = []string{"tmux", "send-keys", "-X", "-t", t, key}
	default:
		return nil, fmt.Errorf("unknown copy-mode action %q", action)
	}
	return append(args, ";", "display-message", "-p", "-t", t, "#{pane_in_mode}\t#{scroll_position}\t#{history_size}"), nil
}

// ParseCopyModeState parses the tmux display-message response.
func ParseCopyModeState(out string) (inMode bool, scrollPosition, historySize int, err error) {
	// In a non-interactive SSH locale tmux renders control characters as '_'.
	f := strings.Split(strings.ReplaceAll(strings.TrimSpace(out), "\t", "_"), "_")
	if len(f) != 3 {
		return false, 0, 0, fmt.Errorf("unexpected copy-mode state")
	}
	mode, e1 := strconv.Atoi(strings.TrimSpace(f[0]))
	var e2 error
	if strings.TrimSpace(f[1]) != "" {
		scrollPosition, e2 = strconv.Atoi(strings.TrimSpace(f[1]))
	}
	historySize, e3 := strconv.Atoi(strings.TrimSpace(f[2]))
	if err = errors.Join(e1, e2, e3); err != nil || (mode != 0 && mode != 1) || scrollPosition < 0 || historySize < 0 {
		return false, 0, 0, fmt.Errorf("unexpected copy-mode state")
	}
	return mode == 1, scrollPosition, historySize, nil
}

// IsNotInCopyMode identifies tmux's state-race response for -X commands.
func IsNotInCopyMode(stderr string) bool {
	return strings.Contains(strings.ToLower(stderr), "not in a mode")
}

// HasSessionArgs returns the argv for has-session (exit 0 if it exists).
func HasSessionArgs(name string) ([]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	return []string{"tmux", "has-session", "-t", target(name)}, nil
}

// AttachArgs returns the argv for attach-session (interactive, via PTY).
func AttachArgs(name string) ([]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	return []string{"tmux", "attach-session", "-t", target(name)}, nil
}

// VersionArgs returns the argv for tmux -V.
func VersionArgs() []string { return []string{"tmux", "-V"} }

// Version is a parsed tmux version ("tmux 3.3a" → 3.3, "tmux next-3.5" → 3.5).
type Version struct {
	Major, Minor int
	Raw          string
}

var versionRE = regexp.MustCompile(`(\d+)\.(\d+)`)

// ParseVersion parses `tmux -V` output.
func ParseVersion(out string) (Version, error) {
	raw := strings.TrimSpace(out)
	m := versionRE.FindStringSubmatch(raw)
	if m == nil {
		return Version{Raw: raw}, fmt.Errorf("can't parse tmux version from %q", raw)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return Version{Major: major, Minor: minor, Raw: strings.TrimPrefix(raw, "tmux ")}, nil
}

// AtLeast reports whether v ≥ major.minor.
func (v Version) AtLeast(major, minor int) bool {
	return v.Major > major || v.Major == major && v.Minor >= minor
}
