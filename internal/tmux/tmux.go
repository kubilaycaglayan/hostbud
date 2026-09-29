// Package tmux builds tmux command lines (as argv for sshx.Exec) and parses
// their output. It never runs anything itself and never touches the user's
// tmux config or global options (docs/ARCHITECTURE.md §5).
package tmux

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"hostbud/internal/sshx"
)

var nameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var windowIDRE = regexp.MustCompile(`^@[0-9]+$`)
var paneIDRE = regexp.MustCompile(`^%[0-9]+$`)

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
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Path      string      `json:"path"`
	Agents    []string    `json:"agents,omitempty"`
	Status    AgentStatus `json:"status,omitempty"`
	Title     string      `json:"title,omitempty"` // active pane's title, unless it is the default hostname
	ProjectID string      `json:"projectId,omitempty"`
	Attached  int         `json:"attached"` // number of attached clients
	Windows   int         `json:"windows"`
	Created   time.Time   `json:"created"`
	Activity  time.Time   `json:"activity"`
}

// AgentStatus is the most urgent hook-reported agent state in a session.
type AgentStatus string

const (
	AgentWorking AgentStatus = "working"
	AgentBlocked AgentStatus = "blocked"
	AgentEnded   AgentStatus = "ended"
)

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

// ListPaneCommands returns a host-wide, low-detail view of each pane's
// foreground command. It is used to decorate session rows without loading
// full window metadata or exposing arbitrary process arguments.
func ListPaneCommands() []string {
	return []string{"sh", "-c", paneMetadataScript}
}

// paneMetadataScript combines tmux's foreground command with recognized
// process names attached to the pane's TTY. Some launchers (including Codex)
// leave a generic runtime such as node as tmux's foreground command while the
// agent process remains in the same terminal process group. Only recognized
// agent names leave the host; other process names and all arguments stay local.
// The session's focused pane also reports its title (what Claude Code and
// Codex set to the current task); tmux's default title, the hostname, is
// blanked. The title is last so '|' in it stays inside the field.
const paneMetadataScript = `tmux list-panes -a -F 'P|#{session_name}|#{pane_id}|#{pane_current_command}|#{@hostbud_agent_status}|#{pane_tty}|#{window_active}#{pane_active}|#{?#{||:#{==:#{pane_title},#{host}},#{==:#{pane_title},#{host_short}}},,#{pane_title}}' |
while IFS='|' read -r marker session pane command status tty focus title; do
	[ "$marker" = P ] || continue
	tty=${tty#/dev/}
	agents=$(ps -t "$tty" -o comm= 2>/dev/null | awk '
		{
			command = tolower($1)
			if (command == "codex" || command == "coy" || command ~ /^codex-/) codex = 1
			if (command == "claude" || command == "claude-code" || command == "cly") claude = 1
		}
		END { if (codex) printf "codex"; printf ","; if (claude) printf "claude" }
	')
	if [ "$focus" = 11 ] && [ -n "$title" ]; then
		title=$(printf '%s' "$title" | tr -d '\t\r\n')
		printf 'P\t%s\t%s\t%s\t%s\t%s\t%s\n' "$session" "$pane" "$command" "$status" "$agents" "$title"
	else
		printf 'P\t%s\t%s\t%s\t%s\t%s\n' "$session" "$pane" "$command" "$status" "$agents"
	fi
done`

type PaneMetadata struct {
	Agents []string
	Status AgentStatus
	Title  string
}

// maxTitleRunes bounds a pane title; the tree shows one truncated line.
const maxTitleRunes = 200

// ParsePaneMetadata extracts recognized foreground harness names and hook
// status markers by session. Other command names and all command arguments
// are discarded. Active markers whose agent process has disappeared are
// treated as ended when a known interactive shell has resumed, covering common
// forced exits that cannot run a session-end hook without mistaking child tools
// for process exit.
func ParsePaneMetadata(out string) (map[string]PaneMetadata, error) {
	type aggregate struct {
		agents map[string]bool
		status AgentStatus
		title  string
	}
	found := make(map[string]aggregate)
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\t", 7)
		if len(f) < 6 || f[0] != "P" || f[1] == "" || !paneIDRE.MatchString(f[2]) || strings.ContainsAny(strings.Join(f[:6], ""), "\r") {
			return nil, fmt.Errorf("unexpected pane metadata record")
		}
		paneAgents := make(map[string]bool)
		// tmux normally reports only the executable name, but installations
		// that invoke a platform-specific Codex launcher can expose that name
		// instead. Reduce a possible path to its basename and accept Codex's
		// own launcher prefix while keeping unrelated commands unclassified.
		command := strings.ToLower(path.Base(strings.TrimSpace(f[3])))
		switch {
		case command == "codex" || command == "coy" || strings.HasPrefix(command, "codex-"):
			paneAgents["codex"] = true
		case command == "claude" || command == "claude-code" || command == "cly":
			paneAgents["claude"] = true
		}
		for _, processAgent := range strings.Split(f[5], ",") {
			switch processAgent {
			case "codex":
				paneAgents["codex"] = true
			case "claude":
				paneAgents["claude"] = true
			case "":
			default:
				return nil, fmt.Errorf("unexpected pane agent process")
			}
		}
		meta := found[f[1]]
		if meta.agents == nil {
			meta.agents = make(map[string]bool)
		}
		for agent := range paneAgents {
			meta.agents[agent] = true
		}
		status := AgentStatus(f[4])
		if status != AgentWorking && status != AgentBlocked && status != AgentEnded {
			status = ""
		}
		if status != "" && len(paneAgents) == 0 && status != AgentEnded && isShellCommand(f[3]) {
			status = AgentEnded
		}
		if statusPriority(status) > statusPriority(meta.status) {
			meta.status = status
		}
		if len(f) == 7 {
			meta.title = paneTitle(f[6])
		}
		found[f[1]] = meta
	}
	result := make(map[string]PaneMetadata, len(found))
	for name, meta := range found {
		list := make([]string, 0, len(meta.agents))
		// Stable icon ordering when a session has more than one agent.
		for _, agent := range []string{"codex", "claude"} {
			if meta.agents[agent] {
				list = append(list, agent)
			}
		}
		result[name] = PaneMetadata{Agents: list, Status: meta.status, Title: meta.title}
	}
	return result, nil
}

// paneTitle trims a title to printable text of bounded length.
func paneTitle(raw string) string {
	title := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, raw)
	if runes := []rune(title); len(runes) > maxTitleRunes {
		title = string(runes[:maxTitleRunes])
	}
	return strings.TrimSpace(title)
}

func isShellCommand(command string) bool {
	switch strings.ToLower(path.Base(strings.TrimSpace(command))) {
	case "sh", "bash", "zsh", "fish", "dash", "ksh", "csh", "tcsh":
		return true
	default:
		return false
	}
}

func statusPriority(status AgentStatus) int {
	switch status {
	case AgentBlocked:
		return 3
	case AgentWorking:
		return 2
	case AgentEnded:
		return 1
	default:
		return 0
	}
}

// NewSession describes a session to create.
type NewSession struct {
	Name         string
	Path         string            // absolute start directory
	Env          map[string]string // -e KEY=VAL, only with tmux ≥ 3.2
	StartCommand string            // shell command line, run by StartShell; empty = shell
	StartArgv    []string          // or: a program and its arguments, each quoted
}

// ErrEnvUnsupported means env vars were requested on tmux < 3.2.
var ErrEnvUnsupported = errors.New("tmux on the host is older than 3.2 and can't set session environment variables (-e)")

var envKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateEnvKey checks a session environment variable name for -e.
func ValidateEnvKey(k string) error {
	if !envKeyRE.MatchString(k) {
		return fmt.Errorf("invalid environment variable name %q: use letters, digits and '_', not starting with a digit", k)
	}
	return nil
}

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
			if err := ValidateEnvKey(k); err != nil {
				return nil, err
			}
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			args = append(args, "-e", k+"="+s.Env[k])
		}
	}
	switch {
	case s.StartCommand != "" && len(s.StartArgv) > 0:
		return nil, errors.New("new-session: give a start command or an argv, not both")
	case s.StartCommand != "":
		// One argument: tmux hands it to its default shell with -c.
		args = append(args, StartShell(s.StartCommand))
	case len(s.StartArgv) > 0:
		args = append(args, StartShell(sshx.Command(s.StartArgv...)))
	}
	return args, nil
}

// NewSessionScript returns new-session as a tmux command script for
// `tmux start-server ; source-file -` (the returned argv): the script is fed
// through stdin, so env values (a run token) never appear in an argv. Every
// word is single-quoted, so tmux expands nothing ($VAR, ~) and ; # { } are
// literal. Newlines are refused.
func NewSessionScript(s NewSession, v Version) (argv []string, script []byte, err error) {
	args, err := NewSessionArgs(s, v)
	if err != nil {
		return nil, nil, err
	}
	words := make([]string, 0, len(args)-1)
	for _, a := range args[1:] { // drop "tmux"
		if strings.ContainsAny(a, "\n\r") {
			return nil, nil, errors.New("new-session: arguments can't contain line breaks")
		}
		words = append(words, sshx.Quote(a))
	}
	return []string{"tmux", "start-server", ";", "source-file", "-"}, []byte(strings.Join(words, " ") + "\n"), nil
}

// StartShell wraps a start command line so it runs as if typed at a prompt:
// in the user's interactive login shell, with that shell's PATH (a tmux
// server started over SSH only has the bare non-interactive PATH, so tools
// in ~/.local/bin or an npm prefix weren't found and the session vanished).
// When the command ends, the session stays open on a login shell, with the
// command's output (or its "not found" error) still on screen.
func StartShell(command string) string {
	const shell = `"${SHELL:-/bin/sh}"`
	return shell + " -lic " + sshx.Quote(command) + "; exec " + shell + " -l"
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

// Pane is a pane in a window, with only the fields shown in the tree.
type Pane struct {
	ID      string `json:"id"`
	Index   int    `json:"index"`
	Active  bool   `json:"active"`
	Command string `json:"command"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

// Window is a tmux window and its panes.
type Window struct {
	ID     string `json:"id"`
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
	Panes  []Pane `json:"panes"`
}

// Tmux sanitizes tab characters to underscores for a non-interactive SSH
// client with the default C locale. Use printable separators and a UTF-8
// locale; free-form fields remain last and are split with a field limit.
const windowsFormat = "W\t#{window_id}\t#{window_index}\t#{window_active}\t#{window_panes}\t#{window_name}"
const panesFormat = "P\t#{window_id}\t#{pane_id}\t#{pane_index}\t#{pane_active}\t#{pane_width}\t#{pane_height}\t#{pane_current_command}"

func listWindowsCommands(name string) ([]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	t := target(name)
	return []string{"env", "LC_ALL=C.UTF-8", "tmux", "list-windows", "-t", t, "-F", windowsFormat, ";", "list-panes", "-s", "-t", t, "-F", panesFormat}, nil
}

// ListWindowsArgs lists windows and panes in one tmux invocation.
func ListWindowsArgs(name string) ([]string, error) { return listWindowsCommands(name) }

// SelectArgs selects a window and optional pane, then returns the refreshed
// listing in the same invocation. Callers must check membership first.
func SelectArgs(name, windowID, paneID string) ([]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	if !windowIDRE.MatchString(windowID) || (paneID != "" && !paneIDRE.MatchString(paneID)) {
		return nil, errors.New("invalid tmux window or pane id")
	}
	t := target(name)
	args := []string{"env", "LC_ALL=C.UTF-8", "tmux", "select-window", "-t", t + ":" + windowID}
	if paneID != "" {
		args = append(args, ";", "select-pane", "-t", t+":"+windowID+"."+paneID)
	}
	list, _ := listWindowsCommands(name)
	args = append(args, ";")
	args = append(args, list[3:]...)
	return args, nil
}

// ParseWindows parses the combined list-windows/list-panes output. Free-form
// names and commands are last in their respective records and control bytes
// are normalized so they cannot corrupt the row structure.
func ParseWindows(out string) ([]Window, bool, error) {
	windows := make([]Window, 0)
	byID := make(map[string]int)
	truncated := false
	records := make([]string, 0)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(line, "W\t") || strings.HasPrefix(line, "P\t") {
			records = append(records, line)
		} else if len(records) > 0 {
			// A literal newline in the last (free-form) field continues that
			// field. The name/command sanitizer replaces it with a space.
			records[len(records)-1] += "\n" + line
		} else if line != "" {
			return nil, false, fmt.Errorf("unexpected window listing record")
		}
	}
	for _, line := range records {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "W\t") {
			f := strings.SplitN(line, "\t", 6)
			if len(f) != 6 || !windowIDRE.MatchString(f[1]) {
				return nil, false, fmt.Errorf("unexpected window record")
			}
			idx, e1 := strconv.Atoi(f[2])
			active, e2 := strconv.Atoi(f[3])
			paneCount, e3 := strconv.Atoi(f[4])
			if errors.Join(e1, e2, e3) != nil || idx < 0 || (active != 0 && active != 1) || paneCount < 0 {
				return nil, false, fmt.Errorf("invalid window fields")
			}
			if len(windows) >= 256 {
				truncated = true
				continue
			}
			if _, exists := byID[f[1]]; exists {
				return nil, false, fmt.Errorf("duplicate window id")
			}
			byID[f[1]] = len(windows)
			windows = append(windows, Window{ID: f[1], Index: idx, Active: active == 1, Name: cleanTmuxText(f[5], 256), Panes: []Pane{}})
			continue
		}
		if strings.HasPrefix(line, "P\t") {
			f := strings.SplitN(line, "\t", 8)
			if len(f) != 8 || !windowIDRE.MatchString(f[1]) || !paneIDRE.MatchString(f[2]) {
				return nil, false, fmt.Errorf("unexpected pane record")
			}
			wi, ok := byID[f[1]]
			if !ok {
				if truncated {
					continue
				}
				return nil, false, fmt.Errorf("pane has unknown window")
			}
			idx, e1 := strconv.Atoi(f[3])
			active, e2 := strconv.Atoi(f[4])
			width, e3 := strconv.Atoi(f[5])
			height, e4 := strconv.Atoi(f[6])
			if errors.Join(e1, e2, e3, e4) != nil || idx < 0 || (active != 0 && active != 1) || width < 0 || height < 0 {
				return nil, false, fmt.Errorf("invalid pane fields")
			}
			if len(windows[wi].Panes) >= 64 {
				truncated = true
				continue
			}
			windows[wi].Panes = append(windows[wi].Panes, Pane{ID: f[2], Index: idx, Active: active == 1, Width: width, Height: height, Command: cleanTmuxText(f[7], 256)})
			continue
		}
		return nil, false, fmt.Errorf("unexpected window listing record")
	}
	slices.SortFunc(windows, func(a, b Window) int { return a.Index - b.Index })
	for i := range windows {
		slices.SortFunc(windows[i].Panes, func(a, b Pane) int { return a.Index - b.Index })
	}
	return windows, truncated, nil
}

func cleanTmuxText(s string, maxBytes int) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			r = ' '
		}
		if b.Len()+len(string(r)) > maxBytes {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
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
	// CopyWheelUp and CopyWheelDown scroll like a mouse wheel over the pane
	// (touch swipes): see WheelArgs. Without a mouse-aware app they scroll
	// tmux copy mode, entering it on the way up.
	CopyWheelUp   CopyAction = "wheel-up"
	CopyWheelDown CopyAction = "wheel-down"
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
	case CopyScrollUp, CopyScrollDown, CopyWheelUp, CopyWheelDown:
		if lines == 0 {
			lines = 1
		}
		if lines < 1 || lines > 500 {
			return nil, errors.New("lines must be between 1 and 500")
		}
		direction := "scroll-up"
		if action == CopyScrollDown || action == CopyWheelDown {
			direction = "scroll-down"
		}
		args = []string{"tmux", "send-keys", "-X", "-N", strconv.Itoa(lines), "-t", t, direction}
		if action == CopyWheelUp {
			// Without -u: entering doesn't jump a page, only the swipe's lines.
			args = append([]string{"tmux", "copy-mode", "-e", "-t", t, ";"}, args[1:]...)
		}
	case CopyPageUp, CopyPageDown, CopyTop, CopyBottom, CopyExit:
		// Bottom leaves copy mode: the live screen is the bottom, and -e (which
		// only reacts to scrolling) would leave the pane in copy mode there.
		key := map[CopyAction]string{CopyPageUp: "page-up", CopyPageDown: "page-down", CopyTop: "history-top", CopyBottom: "cancel", CopyExit: "cancel"}[action]
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
// With tmux 3.2 or newer the client declares the terminal feature "sync"
// (-T, this client only; no server option or user config changes): tmux
// then wraps each redraw in synchronized-output marks (DEC mode 2026), and
// xterm shows it as one frame instead of line by line, which is what made
// copy-mode wheel scrolling flicker (M8 T6).
func AttachArgs(name string, v Version) ([]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	if v.AtLeast(3, 2) {
		return []string{"tmux", "-T", "sync", "attach-session", "-t", target(name)}, nil
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

// WheelStateFormat reads what a wheel event over the pane should do: tmux's
// default WheelUpPane binding forwards it to an app that enabled mouse
// reporting (Claude Code, Codex, vim with mouse) unless the pane is in a mode.
const WheelStateFormat = "#{pane_in_mode} #{mouse_any_flag} #{mouse_sgr_flag} #{pane_width} #{pane_height}"

// WheelState is the parsed WheelStateFormat.
type WheelState struct {
	InMode, AppMouse, SGR bool
	Width, Height         int
}

// ForwardToApp reports whether wheel events belong to the pane's app.
func (w WheelState) ForwardToApp() bool { return w.AppMouse && !w.InMode }

// WheelStateArgs reads WheelStateFormat for the session's active pane.
func WheelStateArgs(name string) ([]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	return []string{"tmux", "display-message", "-p", "-t", "=" + name + ":", WheelStateFormat}, nil
}

// ParseWheelState parses the WheelStateArgs response.
func ParseWheelState(out string) (WheelState, error) {
	f := strings.Fields(out)
	if len(f) != 5 {
		return WheelState{}, fmt.Errorf("unexpected wheel state")
	}
	n := make([]int, len(f))
	for i, v := range f {
		var err error
		if n[i], err = strconv.Atoi(v); err != nil || n[i] < 0 {
			return WheelState{}, fmt.Errorf("unexpected wheel state")
		}
	}
	if n[3] < 1 || n[4] < 1 {
		return WheelState{}, fmt.Errorf("unexpected wheel state")
	}
	return WheelState{InMode: n[0] != 0, AppMouse: n[1] != 0, SGR: n[2] != 0, Width: n[3], Height: n[4]}, nil
}

// LinesPerWheelEvent approximates one wheel notch, as terminals scroll it.
const LinesPerWheelEvent = 3

const maxWheelEvents = 20

// AppWheelArgs writes mouse-wheel reports straight into the pane's input
// (send-keys -H, tmux 3.1+), encoded as the app requested, at the pane's
// centre. It works whatever the user's tmux `mouse` option is.
func AppWheelArgs(name string, action CopyAction, lines int, st WheelState) ([]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	if (action != CopyWheelUp && action != CopyWheelDown) || lines < 1 || lines > 500 {
		return nil, fmt.Errorf("invalid wheel action")
	}
	button := 64
	if action == CopyWheelDown {
		button = 65
	}
	x, y := st.Width/2+1, st.Height/2+1
	var report []byte
	if st.SGR {
		report = fmt.Appendf(nil, "\x1b[<%d;%d;%dM", button, x, y)
	} else {
		// X10 encoding can't address beyond column/row 223.
		report = []byte{0x1b, '[', 'M', x10(button), x10(min(x, 223)), x10(min(y, 223))}
	}
	events := min(maxWheelEvents, (lines+LinesPerWheelEvent-1)/LinesPerWheelEvent)
	t := "=" + name + ":"
	args := []string{"tmux", "send-keys", "-t", t, "-H"}
	for range events {
		for _, b := range report {
			args = append(args, fmt.Sprintf("%02x", b))
		}
	}
	return append(args, ";", "display-message", "-p", "-t", t, "#{pane_in_mode}\t#{scroll_position}\t#{history_size}"), nil
}

// x10 encodes a value (0–223) as an X10 mouse byte.
func x10(v int) byte { return byte(32 + max(0, min(v, 223))) } //nolint:gosec // clamped to 32–255
