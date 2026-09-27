package tmux

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// OutputStateFormat reports whether the pane shows the alternate screen
// (full-screen apps) and how many history lines tmux retains.
const OutputStateFormat = "#{alternate_on} #{history_size}"

// OutputStateArgs reads OutputStateFormat for the session's active pane.
func OutputStateArgs(name string) ([]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	return []string{"tmux", "display-message", "-p", "-t", "=" + name + ":", OutputStateFormat}, nil
}

// ParseOutputState parses the OutputStateArgs response.
func ParseOutputState(out string) (alternate bool, history int, err error) {
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return false, 0, fmt.Errorf("unexpected output state %q", out)
	}
	history, err = strconv.Atoi(fields[1])
	if err != nil || history < 0 {
		return false, 0, fmt.Errorf("unexpected history size %q", fields[1])
	}
	return fields[0] == "1", history, nil
}

// CaptureOutputArgs preserves SGR styles (-e), joins soft-wrapped rows (-J),
// and includes all retained history (-S -), including output before attaching.
//
// While a full-screen app has the alternate screen on, tmux holds three parts:
// the normal history, the saved normal screen (-a) and the app's screen. They
// are captured in that order in one tmux invocation so none is skipped.
func CaptureOutputArgs(name string, alternate bool, history int) ([]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	t := "=" + name + ":"
	capture := []string{"capture-pane", "-p", "-e", "-J"}
	args := []string{"env", "LC_ALL=C.UTF-8", "tmux"}
	if !alternate {
		return append(append(args, capture...), "-S", "-", "-t", t), nil
	}
	if history > 0 {
		args = append(append(append(args, capture...), "-S", "-", "-E", "-1", "-t", t), ";")
	}
	args = append(append(append(args, capture...), "-a", "-t", t), ";")
	return append(append(args, capture...), "-t", t), nil
}

// trailingSpace matches spaces padding a row to the pane width, ahead of any
// trailing SGR sequences, which are kept.
var trailingSpace = regexp.MustCompile(`[ \t]+((?:\x1b\[[0-9;:]*m)*)$`)

// TrimOutput removes the padding -J leaves at row ends and blank rows below
// the last output, so narrow readers don't wrap into empty lines.
func TrimOutput(out string) string {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		lines[i] = trailingSpace.ReplaceAllString(line, "$1")
	}
	for len(lines) > 0 && strings.Trim(stripSGR(lines[len(lines)-1]), " ") == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

var sgr = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func stripSGR(s string) string { return sgr.ReplaceAllString(s, "") }
