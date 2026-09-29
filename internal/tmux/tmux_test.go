package tmux

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"hostbud/internal/sshx"
)

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"a", "acc-a", "my_session-2", "A1", string(make64('x'))} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("ValidateName(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a.b", "a:b", "a b", "a/b", "a\tb", "a\nb", "=a", "ä", "$(x)", "'", string(make64('x')) + "x"} {
		if err := ValidateName(bad); !errors.Is(err, ErrInvalidName) {
			t.Errorf("ValidateName(%q) = %v, want ErrInvalidName", bad, err)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	for in, want := range map[string]string{
		"new session": "new-session", "  a \t b\n": "a-b", "a   b  c": "a-b-c", "plain": "plain", "   ": "", "a.b": "a.b",
	} {
		if got := NormalizeName(in); got != want {
			t.Errorf("NormalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func make64(c byte) []byte {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return b
}

func v(major, minor int) Version { return Version{Major: major, Minor: minor} }

func TestNewSessionArgs(t *testing.T) {
	got, err := NewSessionArgs(NewSession{Name: "web", Path: "/home/dev/app"}, v(3, 4))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tmux", "new-session", "-d", "-s", "web", "-c", "/home/dev/app"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}

	got, err = NewSessionArgs(NewSession{
		Name: "web", Path: "/home/dev/my app",
		Env:          map[string]string{"B": "2", "A": "x y"},
		StartCommand: "htop -d 10",
	}, v(3, 2))
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"tmux", "new-session", "-d", "-s", "web", "-c", "/home/dev/my app", "-e", "A=x y", "-e", "B=2",
		`"${SHELL:-/bin/sh}" -lic 'htop -d 10'; exec "${SHELL:-/bin/sh}" -l`}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestNewSessionStartCommandRemainsOneArgument(t *testing.T) {
	command := `printf '%s\n' 'literal; printf injected'`
	got, err := NewSessionArgs(NewSession{Name: "work", Path: "/home/dev/work", StartCommand: command}, v(3, 4))
	if err != nil {
		t.Fatal(err)
	}
	if got[len(got)-1] != StartShell(command) {
		t.Fatalf("start command argument = %q, want %q", got[len(got)-1], StartShell(command))
	}
}

// StartShell quotes the command for the shell tmux runs it with: after that
// shell's own parsing, the login shell gets the command byte for byte.
func TestStartShellQuoting(t *testing.T) {
	for _, command := range []string{
		"claude --model 'opus 4' \"/goal ship it\"",
		`echo "$HOME" ~/x; printf '%s\n' 'it'\''s'`,
		"codex -c 'hooks.Stop=[{\"command\":\"curl $HOSTBUD_URL\"}]'",
	} {
		got := StartShell(command)
		prefix, suffix := `"${SHELL:-/bin/sh}" -lic `, `; exec "${SHELL:-/bin/sh}" -l`
		if !strings.HasPrefix(got, prefix) || !strings.HasSuffix(got, suffix) {
			t.Fatalf("StartShell(%q) = %q", command, got)
		}
		if quoted := strings.TrimSuffix(strings.TrimPrefix(got, prefix), suffix); quoted != sshx.Quote(command) {
			t.Fatalf("command quoted as %q, want %q", quoted, sshx.Quote(command))
		}
	}
}

func TestNewSessionArgsErrors(t *testing.T) {
	if _, err := NewSessionArgs(NewSession{Name: "a.b", Path: "/x"}, v(3, 4)); !errors.Is(err, ErrInvalidName) {
		t.Errorf("invalid name: %v", err)
	}
	if _, err := NewSessionArgs(NewSession{Name: "a", Path: ""}, v(3, 4)); err == nil {
		t.Error("empty path accepted")
	}
	env := map[string]string{"K": "v"}
	if _, err := NewSessionArgs(NewSession{Name: "a", Path: "/x", Env: env}, v(3, 1)); !errors.Is(err, ErrEnvUnsupported) {
		t.Errorf("-e on 3.1: %v", err)
	}
	if _, err := NewSessionArgs(NewSession{Name: "a", Path: "/x", Env: map[string]string{"A=B": "v"}}, v(3, 4)); err == nil {
		t.Error("bad env key accepted")
	}
	// No env: fine on old tmux.
	if _, err := NewSessionArgs(NewSession{Name: "a", Path: "/x"}, v(2, 9)); err != nil {
		t.Errorf("old tmux without env: %v", err)
	}
}

func TestExactTargets(t *testing.T) {
	cases := []struct {
		got  func() ([]string, error)
		want []string
	}{
		{func() ([]string, error) { return RenameSessionArgs("old", "new") }, []string{"tmux", "rename-session", "-t", "=old", "new"}},
		{func() ([]string, error) { return KillSessionArgs("acc-a") }, []string{"tmux", "kill-session", "-t", "=acc-a"}},
		{func() ([]string, error) { return HasSessionArgs("acc-a") }, []string{"tmux", "has-session", "-t", "=acc-a"}},
		{func() ([]string, error) { return AttachArgs("acc-a", Version{}) }, []string{"tmux", "attach-session", "-t", "=acc-a"}},
		{func() ([]string, error) { return AttachArgs("acc-a", Version{Major: 3, Minor: 1}) }, []string{"tmux", "attach-session", "-t", "=acc-a"}},
		{func() ([]string, error) { return AttachArgs("acc-a", Version{Major: 3, Minor: 2}) }, []string{"tmux", "-T", "sync", "attach-session", "-t", "=acc-a"}},
		{func() ([]string, error) { return AttachArgs("acc-a", Version{Major: 3, Minor: 5}) }, []string{"tmux", "-T", "sync", "attach-session", "-t", "=acc-a"}},
	}
	for _, c := range cases {
		got, err := c.got()
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("got %q, %v; want %q", got, err, c.want)
		}
	}
	if _, err := RenameSessionArgs("ok", "not ok"); !errors.Is(err, ErrInvalidName) {
		t.Error("rename to invalid name accepted")
	}
	if _, err := KillSessionArgs("a:b"); !errors.Is(err, ErrInvalidName) {
		t.Error("kill of invalid name accepted")
	}
	if _, err := AttachArgs("", Version{Major: 3, Minor: 5}); !errors.Is(err, ErrInvalidName) {
		t.Error("attach of empty name accepted")
	}
}

func TestWindowAndPaneArgs(t *testing.T) {
	wantList := []string{"env", "LC_ALL=C.UTF-8", "tmux", "list-windows", "-t", "=work", "-F", windowsFormat, ";", "list-panes", "-s", "-t", "=work", "-F", panesFormat}
	got, err := ListWindowsArgs("work")
	if err != nil || !slices.Equal(got, wantList) {
		t.Fatalf("list argv = %q, %v; want %q", got, err, wantList)
	}
	wantSelect := []string{"env", "LC_ALL=C.UTF-8", "tmux", "select-window", "-t", "=work:@2", ";", "select-pane", "-t", "=work:@2.%4", ";"}
	wantSelect = append(wantSelect, wantList[3:]...)
	got, err = SelectArgs("work", "@2", "%4")
	if err != nil || !slices.Equal(got, wantSelect) {
		t.Fatalf("select argv = %q, %v; want %q", got, err, wantSelect)
	}
	wantSelect = []string{"env", "LC_ALL=C.UTF-8", "tmux", "select-window", "-t", "=work:@1", ";"}
	wantSelect = append(wantSelect, wantList[3:]...)
	got, err = SelectArgs("work", "@1", "")
	if err != nil || !slices.Equal(got, wantSelect) {
		t.Fatalf("select window argv = %q, %v; want %q", got, err, wantSelect)
	}
	for _, tc := range []struct{ name, window, pane string }{
		{"bad.name", "@1", ""}, {"work", "@", ""}, {"work", "@x", ""}, {"work", "1", ""},
		{"work", "@1;x", ""}, {"work", "@1", "%"}, {"work", "@1", "%x"}, {"work", "@1", "1"},
		{"work", "@1", "%1;kill-server"},
	} {
		if _, err := SelectArgs(tc.name, tc.window, tc.pane); err == nil {
			t.Errorf("accepted invalid ids/name %q %q %q", tc.name, tc.window, tc.pane)
		}
	}
	if _, err := ListWindowsArgs("bad.name"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("invalid listing name: %v", err)
	}
}

func TestParseWindows(t *testing.T) {
	out := "W\t@2\t2\t0\t1\tlast\twindow\n" +
		"W\t@0\t0\t1\t2\tfirst\twin\ndow\n" +
		"W\t@3\t3\t0\t1\tthird\n" +
		"P\t@0\t%2\t1\t0\t80\t24\t\n" +
		"P\t@0\t%1\t0\t1\t80\t24\tvim\twith\ttab\n" +
		"P\t@2\t%3\t0\t1\t120\t40\tzsh\n" +
		"P\t@3\t%4\t0\t1\t120\t40\tsh\n"
	got, truncated, err := ParseWindows(out)
	if err != nil || truncated {
		t.Fatalf("parse: truncated=%v err=%v", truncated, err)
	}
	if len(got) != 3 || got[0].ID != "@0" || got[0].Name != "first win dow" ||
		len(got[0].Panes) != 2 || got[0].Panes[0].ID != "%1" ||
		got[0].Panes[0].Command != "vim with tab" || got[0].Panes[1].Command != "" ||
		got[1].Index != 2 || got[1].Panes[0].Command != "zsh" || got[2].Name != "third" {
		t.Fatalf("parsed windows = %#v", got)
	}
	if got, truncated, err := ParseWindows(""); err != nil || truncated || len(got) != 0 {
		t.Fatalf("empty listing = %#v, %v, %v", got, truncated, err)
	}
	for _, bad := range []string{"W\t@x\t0\t1\t1\tname\n", "P\t@1\t%2\t0\t1\t80\t24\tcmd\n", "X\tbad"} {
		if _, _, err := ParseWindows(bad); err == nil {
			t.Errorf("accepted malformed listing %q", bad)
		}
	}
}

func TestParseWindowsCaps(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 257; i++ {
		fmt.Fprintf(&b, "W\t@%d\t%d\t0\t0\tw\n", i, i)
	}
	got, truncated, err := ParseWindows(b.String())
	if err != nil || !truncated || len(got) != 256 {
		t.Fatalf("windows cap: len=%d truncated=%v err=%v", len(got), truncated, err)
	}
	b.Reset()
	b.WriteString("W\t@1\t0\t0\t65\tw\n")
	for i := 0; i < 65; i++ {
		fmt.Fprintf(&b, "P\t@1\t%%%d\t%d\t0\t80\t24\tsh\n", i, i)
	}
	got, truncated, err = ParseWindows(b.String())
	if err != nil || !truncated || len(got) != 1 || len(got[0].Panes) != 64 {
		t.Fatalf("pane cap: %#v truncated=%v err=%v", got, truncated, err)
	}
}

func TestCopyModeArgs(t *testing.T) {
	cases := []struct {
		action CopyAction
		lines  int
		want   []string
	}{
		{CopyEnter, 0, []string{"tmux", "copy-mode", "-e", "-u", "-t", "=work:"}},
		{CopyScrollUp, 0, []string{"tmux", "send-keys", "-X", "-N", "1", "-t", "=work:", "scroll-up"}},
		{CopyScrollDown, 500, []string{"tmux", "send-keys", "-X", "-N", "500", "-t", "=work:", "scroll-down"}},
		{CopyPageUp, 0, []string{"tmux", "send-keys", "-X", "-t", "=work:", "page-up"}},
		{CopyPageDown, 0, []string{"tmux", "send-keys", "-X", "-t", "=work:", "page-down"}},
		{CopyTop, 0, []string{"tmux", "send-keys", "-X", "-t", "=work:", "history-top"}},
		{CopyBottom, 0, []string{"tmux", "send-keys", "-X", "-t", "=work:", "cancel"}},
		{CopyExit, 0, []string{"tmux", "send-keys", "-X", "-t", "=work:", "cancel"}},
		{CopyWheelUp, 4, []string{"tmux", "copy-mode", "-e", "-t", "=work:", ";", "send-keys", "-X", "-N", "4", "-t", "=work:", "scroll-up"}},
		{CopyWheelDown, 4, []string{"tmux", "send-keys", "-X", "-N", "4", "-t", "=work:", "scroll-down"}},
	}
	for _, c := range cases {
		got, err := CopyModeArgs("work", c.action, c.lines)
		if err != nil {
			t.Fatalf("%s: %v", c.action, err)
		}
		want := append(c.want, ";", "display-message", "-p", "-t", "=work:", "#{pane_in_mode}\t#{scroll_position}\t#{history_size}")
		if !slices.Equal(got, want) {
			t.Errorf("%s argv = %q, want %q", c.action, got, want)
		}
	}
	for _, n := range []int{-1, 501} {
		if _, err := CopyModeArgs("work", CopyScrollUp, n); err == nil {
			t.Errorf("accepted lines %d", n)
		}
	}
	if _, err := CopyModeArgs("bad.name", CopyEnter, 0); !errors.Is(err, ErrInvalidName) {
		t.Errorf("invalid name: %v", err)
	}
	if _, err := CopyModeArgs("work", "wat", 0); err == nil {
		t.Error("accepted unknown action")
	}
}

func TestParseCopyModeState(t *testing.T) {
	for _, tc := range []struct {
		in        string
		mode      bool
		pos, size int
	}{{"1\t25\t300", true, 25, 300}, {"0\t0\t0", false, 0, 0}, {"0__300", false, 0, 300}} {
		mode, pos, size, err := ParseCopyModeState(tc.in)
		if err != nil || mode != tc.mode || pos != tc.pos || size != tc.size {
			t.Errorf("parse %q = %v %d %d %v", tc.in, mode, pos, size, err)
		}
	}
	if !IsNotInCopyMode("can't send keys: pane is not in a mode") {
		t.Error("did not recognize not-in-mode error")
	}
	if _, _, _, err := ParseCopyModeState("bad"); err == nil {
		t.Error("accepted malformed state")
	}
}

func TestParseSessions(t *testing.T) {
	out := "$0:acc-a:1:3:1760000000:1760000100:/home/dev/app\n" +
		"$4:b:0:1:1760000200:1760000300:/home/dev/with:colon\n"
	got, err := ParseSessions(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []Session{
		{ID: "$0", Name: "acc-a", Attached: 1, Windows: 3, Path: "/home/dev/app",
			Created: time.Unix(1760000000, 0).UTC(), Activity: time.Unix(1760000100, 0).UTC()},
		{ID: "$4", Name: "b", Attached: 0, Windows: 1, Path: "/home/dev/with:colon",
			Created: time.Unix(1760000200, 0).UTC(), Activity: time.Unix(1760000300, 0).UTC()},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}

	if got, err := ParseSessions(""); err != nil || len(got) != 0 {
		t.Fatalf("empty output: %v %v", got, err)
	}
	for _, bad := range []string{"$0:acc\n", "$0:a:x:1:1:1:/p\n"} {
		if _, err := ParseSessions(bad); err == nil {
			t.Errorf("ParseSessions(%q) accepted", bad)
		}
	}
}

func TestParsePaneMetadataAgentAliases(t *testing.T) {
	if got, want := ListPaneCommands(), []string{"sh", "-c", paneMetadataScript}; !slices.Equal(got, want) {
		t.Fatalf("ListPaneCommands() = %q, want %q", got, want)
	}
	got, err := ParsePaneMetadata("P\tacc-a\t%1\tcodex\tworking\t\nP\tacc-a\t%2\tbash\t\t\nP\tacc-a\t%3\tclaude\tblocked\t\nP\tacc-b\t%4\tvim\t\t\nP\tacc-c\t%5\tCLAUDE-CODE\tended\t\nP\tacc-d\t%6\tcodex-linux-x64\t\t\nP\tacc-e\t%7\t/usr/local/bin/codex\t\t\nP\tacc-f\t%8\tcoy\t\t\nP\tacc-g\t%9\tcly\t\t\nP\tlegacy.session\t%10\tcodex\t\t\nP\tforced-exit\t%11\tbash\tworking\t\nP\tforced-exit\t%12\tbash\tblocked\t\nP\tchild-command\t%13\tvim\tworking\t\nP\tnode-launcher\t%14\tnode\tworking\tcodex,\nP\tended-live-agent\t%16\tnode\tended\tcodex,\nP\tboth-processes\t%15\tnode\t\tcodex,claude\n")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]PaneMetadata{
		"acc-a":            {Agents: []string{"codex", "claude"}, Status: AgentBlocked},
		"acc-b":            {},
		"acc-c":            {Agents: []string{"claude"}, Status: AgentEnded},
		"acc-d":            {Agents: []string{"codex"}},
		"acc-e":            {Agents: []string{"codex"}},
		"acc-f":            {Agents: []string{"codex"}},
		"acc-g":            {Agents: []string{"claude"}},
		"legacy.session":   {Agents: []string{"codex"}},
		"forced-exit":      {Status: AgentEnded},
		"child-command":    {Status: AgentWorking},
		"node-launcher":    {Agents: []string{"codex"}, Status: AgentWorking},
		"ended-live-agent": {Agents: []string{"codex"}, Status: AgentWorking},
		"both-processes":   {Agents: []string{"codex", "claude"}},
	}
	if len(got) != len(want) {
		t.Fatalf("agents = %#v, want %#v", got, want)
	}
	for session, metadata := range want {
		if !slices.Equal(got[session].Agents, metadata.Agents) || got[session].Status != metadata.Status {
			t.Fatalf("metadata[%s] = %#v, want %#v", session, got[session], metadata)
		}
	}
	for _, bad := range []string{"x\tacc\t%1\tcodex\t\t\n", "P\t\t%1\tcodex\t\t\n", "P\tacc\tno-pane\tcodex\t\t\n", "P\tacc\t%1\tclaude\textra\textra\n", "P\tacc\t%1\tbash\t\tother\n"} {
		if _, err := ParsePaneMetadata(bad); err == nil {
			t.Errorf("ParsePaneMetadata(%q) accepted", bad)
		}
	}
}

func TestParsePaneMetadataTitle(t *testing.T) {
	if !strings.Contains(paneMetadataScript, "#{==:#{pane_title},#{host}}") {
		t.Fatal("script must blank tmux's default hostname title")
	}
	long := strings.Repeat("é", maxTitleRunes+10)
	got, err := ParsePaneMetadata("P\ttitled\t%1\tbash\t\t\t deploy the changes | commit \nP\ttitled\t%2\tvim\t\t\nP\tplain\t%3\tbash\t\t\nP\tlong\t%4\tbash\t\t\t" + long + "\nP\tctl\t%5\tbash\t\t\ta\x1bb\n")
	if err != nil {
		t.Fatal(err)
	}
	if got["titled"].Title != "deploy the changes | commit" {
		t.Errorf("title = %q", got["titled"].Title)
	}
	if got["plain"].Title != "" {
		t.Errorf("untitled session title = %q", got["plain"].Title)
	}
	if n := len([]rune(got["long"].Title)); n != maxTitleRunes {
		t.Errorf("long title has %d runes, want %d", n, maxTitleRunes)
	}
	if got["ctl"].Title != "ab" {
		t.Errorf("control characters kept: %q", got["ctl"].Title)
	}
}

func TestNoServer(t *testing.T) {
	for _, s := range []string{
		"no server running on /tmp/tmux-1000/default\n",
		"error connecting to /tmp/tmux-1000/default (No such file or directory)\n",
	} {
		if !NoServer(s) {
			t.Errorf("NoServer(%q) = false", s)
		}
	}
	if NoServer("can't find session: x") {
		t.Error("other error treated as no server")
	}
}

func TestParseVersion(t *testing.T) {
	cases := map[string]Version{
		"tmux 3.3a\n":   {Major: 3, Minor: 3, Raw: "3.3a"},
		"tmux 3.2":      {Major: 3, Minor: 2, Raw: "3.2"},
		"tmux next-3.6": {Major: 3, Minor: 6, Raw: "next-3.6"},
		"tmux 2.9a":     {Major: 2, Minor: 9, Raw: "2.9a"},
	}
	for in, want := range cases {
		got, err := ParseVersion(in)
		if err != nil || got != want {
			t.Errorf("ParseVersion(%q) = %+v, %v", in, got, err)
		}
	}
	if _, err := ParseVersion("tmux master"); err == nil {
		t.Error("unparseable version accepted")
	}
	if !v(3, 2).AtLeast(3, 2) || !v(4, 0).AtLeast(3, 2) || v(3, 1).AtLeast(3, 2) || v(2, 9).AtLeast(3, 2) {
		t.Error("AtLeast wrong")
	}
}

func TestWheelState(t *testing.T) {
	st, err := ParseWheelState("0 1 1 91 30\n")
	if err != nil || st != (WheelState{AppMouse: true, SGR: true, Width: 91, Height: 30}) || !st.ForwardToApp() {
		t.Fatalf("parse: %+v %v", st, err)
	}
	if st, _ := ParseWheelState("1 1 1 91 30"); st.ForwardToApp() {
		t.Fatal("pane in a mode keeps the wheel in tmux")
	}
	for _, bad := range []string{"", "0 1 1 91", "0 x 1 91 30", "0 1 1 0 30", "0 -1 1 91 30"} {
		if _, err := ParseWheelState(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := WheelStateArgs("bad.name"); !errors.Is(err, ErrInvalidName) {
		t.Errorf("invalid name: %v", err)
	}
}

func TestAppWheelArgs(t *testing.T) {
	tail := []string{";", "display-message", "-p", "-t", "=work:", "#{pane_in_mode}\t#{scroll_position}\t#{history_size}"}
	hex := func(s string) []string {
		var out []string
		for _, b := range []byte(s) {
			out = append(out, fmt.Sprintf("%02x", b))
		}
		return out
	}
	got, err := AppWheelArgs("work", CopyWheelUp, 6, WheelState{AppMouse: true, SGR: true, Width: 91, Height: 30})
	want := slices.Concat([]string{"tmux", "send-keys", "-t", "=work:", "-H"}, hex("\x1b[<64;46;16M\x1b[<64;46;16M"), tail)
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("sgr: %q %v", got, err)
	}
	got, _ = AppWheelArgs("work", CopyWheelDown, 1, WheelState{AppMouse: true, Width: 460, Height: 30})
	want = slices.Concat([]string{"tmux", "send-keys", "-t", "=work:", "-H"}, hex("\x1b[M"+string([]byte{32 + 65, 32 + 223, 32 + 16})), tail)
	if !slices.Equal(got, want) {
		t.Fatalf("x10: %q", got)
	}
	got, _ = AppWheelArgs("work", CopyWheelUp, 500, WheelState{AppMouse: true, SGR: true, Width: 10, Height: 10})
	if n := len(got) - 5 - len(tail); n != maxWheelEvents*len("\x1b[<64;6;6M") {
		t.Fatalf("events not capped: %d bytes", n)
	}
	for _, c := range []struct {
		name   string
		action CopyAction
		lines  int
	}{{"bad.name", CopyWheelUp, 1}, {"work", CopyScrollUp, 1}, {"work", CopyWheelUp, 0}, {"work", CopyWheelUp, 501}} {
		if _, err := AppWheelArgs(c.name, c.action, c.lines, WheelState{Width: 1, Height: 1}); err == nil {
			t.Errorf("accepted %+v", c)
		}
	}
}

func TestNewSessionStartArgvIsQuotedBySshx(t *testing.T) {
	argv := []string{"claude", "--settings", `{"hooks":{"Stop":[{"command":"curl \"$HOSTBUD_URL\""}]}}`, "/goal it's done"}
	got, err := NewSessionArgs(NewSession{Name: "app-q1", Path: "/home/dev/app", StartArgv: argv}, v(3, 4))
	if err != nil {
		t.Fatal(err)
	}
	if want := StartShell(sshx.Command(argv...)); got[len(got)-1] != want {
		t.Fatalf("start argument %q, want %q", got[len(got)-1], want)
	}
	if _, err := NewSessionArgs(NewSession{Name: "a", Path: "/x", StartCommand: "top", StartArgv: []string{"top"}}, v(3, 4)); err == nil {
		t.Fatal("command and argv together accepted")
	}
}

func TestNewSessionScript(t *testing.T) {
	argv, script, err := NewSessionScript(NewSession{Name: "app-q1", Path: "/home/dev/my app", Env: map[string]string{"K": "a 'b' $HOME ~ ;#"}, StartCommand: "echo hi"}, v(3, 4))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(argv, []string{"tmux", "start-server", ";", "source-file", "-"}) {
		t.Fatalf("argv %q", argv)
	}
	want := `'new-session' '-d' '-s' 'app-q1' '-c' '/home/dev/my app' '-e' 'K=a '\''b'\'' $HOME ~ ;#' ` + sshx.Quote(StartShell("echo hi")) + "\n"
	if string(script) != want {
		t.Fatalf("script %q\nwant   %q", script, want)
	}
	for _, bad := range []NewSession{
		{Name: "a", Path: "/x", Env: map[string]string{"K": "line\nbreak"}},
		{Name: "a", Path: "/x", StartArgv: []string{"echo", "a\rb"}},
	} {
		if _, _, err := NewSessionScript(bad, v(3, 4)); err == nil {
			t.Errorf("line break accepted: %+v", bad)
		}
	}
	if _, _, err := NewSessionScript(NewSession{Name: "a", Path: "/x", Env: map[string]string{"K": "v"}}, v(3, 1)); !errors.Is(err, ErrEnvUnsupported) {
		t.Errorf("tmux 3.1: %v", err)
	}
}
