package agents

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"hostbud/internal/sshx"
)

func TestSplitFlags(t *testing.T) {
	for in, want := range map[string][]string{
		"":                                     nil,
		"  --dangerously-skip-permissions ":    {"--dangerously-skip-permissions"},
		`--model 'opus 4' -c "x=\"y\" \$HOME"`: {"--model", "opus 4", "-c", `x="y" $HOME`},
		`--add-dir ~/x $HOME a\ b`:             {"--add-dir", "~/x", "$HOME", "a b"},
		`--x='a'"b"c`:                          {"--x=abc"},
		`''`:                                   {""},
	} {
		got, err := SplitFlags(in)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("SplitFlags(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{`--model 'opus`, `-c "x`, `trailing\`, "a\nb"} {
		if _, err := SplitFlags(bad); err == nil {
			t.Errorf("SplitFlags(%q) accepted", bad)
		}
	}
}

func TestConditionAndValidation(t *testing.T) {
	if c, err := Condition("/goal work on milestone 2  per docs"); err != nil || c != "work on milestone 2  per docs" {
		t.Fatalf("Condition = %q, %v", c, err)
	}
	if c, err := Condition("work on M2"); err != nil || c != "work on M2" {
		t.Fatalf("plain Condition = %q, %v", c, err)
	}
	for _, bad := range []string{"", "  ", "work on M2\nnext"} {
		if _, err := Condition(bad); err == nil {
			t.Errorf("Condition(%q) accepted", bad)
		}
	}
	if !SameCondition(" a  b\tc ", "a b c") || SameCondition("a b", "a c") {
		t.Fatal("SameCondition doesn't normalize whitespace only")
	}
	r := NewRegistry(NewClaude(nil, nil, nil))
	if err := r.ValidateItem("gemini", "", "/goal x"); !errors.Is(err, ErrUnknownAgent) {
		t.Fatalf("unknown agent: %v", err)
	}
	if err := r.ValidateItem("claude", `--model 'x`, "/goal x"); err == nil {
		t.Fatal("unbalanced flags accepted")
	}
	if err := r.ValidateItem("claude", "--model opus", "work on M2"); err != nil {
		t.Fatal(err)
	}
}

func TestVersions(t *testing.T) {
	if v, ok := ParseVersion("noise\n2.1.283 (Claude Code)\n"); !ok || v != "2.1.283" {
		t.Fatalf("ParseVersion = %q", v)
	}
	for _, c := range []struct {
		have, want string
		ok         bool
	}{{"2.1.283", "2.1.283", true}, {"2.10.0", "2.9.9", true}, {"2.1.282", "2.1.283", false}, {"1.9.3", "2.1.0", false}, {"x", "1.0.0", false}} {
		if VersionAtLeast(c.have, c.want) != c.ok {
			t.Errorf("VersionAtLeast(%s, %s) != %v", c.have, c.want, c.ok)
		}
	}
}

type fakeHost struct {
	out  string
	err  error
	args [][]string
}

func (f *fakeHost) Exec(_ context.Context, _ string, args ...string) ([]byte, error) {
	f.args = append(f.args, args)
	return []byte(f.out), f.err
}

func (f *fakeHost) ExecInput(ctx context.Context, m string, _ []byte, args ...string) ([]byte, error) {
	return f.Exec(ctx, m, args...)
}

func TestClaudeCheckVersion(t *testing.T) {
	ok := &fakeHost{out: "2.1.283 (Claude Code)\n"}
	if v, err := NewClaude(ok, nil, nil).CheckVersion(context.Background(), "host"); err != nil || v != "2.1.283" {
		t.Fatalf("CheckVersion = %q, %v", v, err)
	}
	if !slices.Equal(ok.args[0], LoginShell("claude", "--version")) || ok.args[0][4] != "'claude' '--version'" {
		t.Fatalf("argv %q", ok.args[0])
	}
	old := &fakeHost{out: "1.9.3 (Claude Code)"}
	if _, err := NewClaude(old, nil, nil).CheckVersion(context.Background(), "host"); err == nil ||
		err.Error() != "Claude Code 2.1.283 or newer is needed on the host; found 1.9.3 — update with `claude update`" {
		t.Fatalf("old client: %v", err)
	}
	missing := &fakeHost{err: &sshx.Error{Kind: sshx.KindRemote, ExitCode: 127}}
	if _, err := NewClaude(missing, nil, nil).CheckVersion(context.Background(), "host"); err == nil || err.Error() != "claude not found on the host — install Claude Code first" {
		t.Fatalf("missing client: %v", err)
	}
	garbled := &fakeHost{out: "welcome to my shell"}
	if _, err := NewClaude(garbled, nil, nil).CheckVersion(context.Background(), "host"); err == nil || !strings.Contains(err.Error(), "could not read") {
		t.Fatalf("garbled output: %v", err)
	}
}

func TestHookCommandReferencesOnlyEnv(t *testing.T) {
	cmd := HookCommand(EventTurnEnd)
	for _, want := range []string{`$HOSTBUD_RUN_TOKEN`, `$HOSTBUD_URL/api/hooks/$HOSTBUD_RUN_ID/turn_end`, "--data-binary @-", "|| true", "--max-time 5"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("hook command lacks %q: %s", want, cmd)
		}
	}
}
