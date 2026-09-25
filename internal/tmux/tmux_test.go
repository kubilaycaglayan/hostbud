package tmux

import (
	"errors"
	"slices"
	"testing"
	"time"
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
	want = []string{"tmux", "new-session", "-d", "-s", "web", "-c", "/home/dev/my app", "-e", "A=x y", "-e", "B=2", "htop -d 10"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
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
		{func() ([]string, error) { return AttachArgs("acc-a") }, []string{"tmux", "attach-session", "-t", "=acc-a"}},
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
	if _, err := AttachArgs(""); !errors.Is(err, ErrInvalidName) {
		t.Error("attach of empty name accepted")
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
	if !slices.Equal(got, want) {
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
