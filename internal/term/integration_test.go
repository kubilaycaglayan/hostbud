//go:build integration

package term_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"hostbud/internal/sshx"
	"hostbud/internal/term"
	"hostbud/internal/testenv"
)

type env struct {
	t   *testing.T
	c   *sshx.Client
	h   *term.Handler
	url string
}

func setup(t *testing.T, opts ...func(*term.Handler)) *env {
	t.Helper()
	c := testenv.Connected(t, testenv.SSHD)
	testenv.Sh(t, c, "tmux kill-server 2>/dev/null; true")
	t.Cleanup(func() { testenv.Sh(t, c, "tmux kill-server 2>/dev/null; rm -f ~/term-it.txt; true") })
	h := &term.Handler{SSH: c}
	for _, o := range opts {
		o(h)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &env{t: t, c: c, h: h, url: "ws" + strings.TrimPrefix(srv.URL, "http")}
}

func (e *env) sh(line string) string { return strings.TrimSpace(testenv.Sh(e.t, e.c, line)) }

func (e *env) newSession(name string) {
	e.sh("tmux new-session -d -s " + name + " -x 80 -y 24")
}

// attach opens /ws/term and drains output in the background.
func (e *env) attach(name string, cols, rows int) (*websocket.Conn, *output) {
	e.t.Helper()
	url := fmt.Sprintf("%s?machine=host&session=%s&cols=%d&rows=%d", e.url, name, cols, rows)
	conn, resp, err := websocket.Dial(e.t.Context(), url, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = conn.CloseNow() })
	out := &output{text: make(chan string, 1024), control: make(chan term.Control, 8)}
	go func() {
		for {
			typ, data, err := conn.Read(context.Background())
			if err != nil {
				close(out.control)
				return
			}
			if typ == websocket.MessageBinary {
				out.text <- string(data)
				continue
			}
			var c term.Control
			if json.Unmarshal(data, &c) == nil {
				out.control <- c
			}
		}
	}()
	return conn, out
}

type output struct {
	text    chan string
	control chan term.Control
	seen    strings.Builder
}

// waitFor reads output until it contains s.
func (o *output) waitFor(t *testing.T, s string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for !strings.Contains(o.seen.String(), s) {
		select {
		case chunk := <-o.text:
			o.seen.WriteString(chunk)
		case <-deadline:
			t.Fatalf("output never contained %q", s)
		}
	}
}

func send(t *testing.T, c *websocket.Conn, s string) {
	t.Helper()
	if err := c.Write(context.Background(), websocket.MessageBinary, []byte(s)); err != nil {
		t.Fatal(err)
	}
}

func (e *env) eventually(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (e *env) capture(name string) string { return e.sh("tmux capture-pane -p -t =" + name + ":") }

func TestIntegrationSilentAttachGetsTimeoutClose(t *testing.T) {
	e := setup(t, func(h *term.Handler) { h.AttachTimeout = 300 * time.Millisecond })
	e.newSession("stall")
	deadline := time.Now().Add(4 * time.Second).Unix()
	e.sh("mkdir -p /home/dev/.hostbud-stall && printf '%d\\n' " + strconv.FormatInt(deadline, 10) + " >/home/dev/.hostbud-stall/tmux")
	t.Cleanup(func() { e.sh("rm -f /home/dev/.hostbud-stall/tmux") })
	conn, resp, err := websocket.Dial(t.Context(), e.url+"?machine=host&session=stall&cols=80&rows=24", nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, _, err = conn.Read(ctx)
	if websocket.CloseStatus(err) != 4408 || !strings.Contains(err.Error(), "host didn't answer") {
		t.Fatalf("silent attach close = %v (%d)", err, websocket.CloseStatus(err))
	}
}

func TestIntegrationAttachTypeResizeClose(t *testing.T) {
	e := setup(t)
	e.newSession("term-a")
	conn, out := e.attach("term-a", 100, 30)
	e.eventually("attached", func() bool { return e.sh("tmux display -p -t =term-a: '#{session_attached}'") == "1" })

	send(t, conn, "echo term-marker-$((6*7))\r")
	out.waitFor(t, "term-marker-42")
	e.eventually("marker in capture-pane", func() bool { return strings.Contains(e.capture("term-a"), "term-marker-42") })

	if w := e.sh("tmux display -p -t =term-a: '#{window_width}'"); w != "100" {
		t.Fatalf("initial width %s, want 100", w)
	}
	if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"resize","cols":132,"rows":40}`)); err != nil {
		t.Fatal(err)
	}
	e.eventually("resized to 132 columns", func() bool {
		return e.sh("tmux list-clients -t =term-a -F '#{client_width}x#{client_height}'") == "132x40" &&
			e.sh("tmux display -p -t =term-a: '#{window_width}'") == "132"
	})

	_ = conn.Close(websocket.StatusNormalClosure, "tab closed")
	e.eventually("ssh process gone", func() bool { return e.h.Active() == 0 })
	e.eventually("client detached", func() bool { return e.sh("tmux display -p -t =term-a: '#{session_attached}'") == "0" })
	if got := e.sh("tmux has-session -t =term-a && echo alive"); got != "alive" {
		t.Fatal("session died with the socket")
	}
}

func TestIntegrationTerminalAttachLimit(t *testing.T) {
	e := setup(t, func(h *term.Handler) {
		h.MaxPerUser = 32
		h.MaxTotal = 40
		h.AccountID = func(*http.Request) string { return "integration-user" }
	})
	e.newSession("cap-term")
	conns := make([]*websocket.Conn, 0, 32)
	for i := 0; i < 32; i++ {
		conn, resp, err := websocket.Dial(t.Context(), fmt.Sprintf("%s?machine=host&session=cap-term", e.url), nil)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			t.Fatalf("attach %d: %v", i+1, err)
		}
		conns = append(conns, conn)
	}
	conn, resp, err := websocket.Dial(t.Context(), e.url+"?machine=host&session=cap-term", nil)
	if resp != nil && resp.Body != nil {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(body), "Too many open terminals (32)") {
			t.Fatalf("33rd attach: status=%d body=%s", resp.StatusCode, body)
		}
	} else if err == nil {
		_ = conn.CloseNow()
		t.Fatal("33rd attach accepted")
	}
	e.eventually("32 clients attached", func() bool { return e.sh("tmux list-clients -t =cap-term -F '#{client_pid}' | wc -l") == "32" })
	for _, c := range conns {
		_ = c.CloseNow()
	}
	e.eventually("all terminal processes exit", func() bool { return e.h.Active() == 0 })
}

func TestIntegrationSlowTerminalClientDropped(t *testing.T) {
	e := setup(t)
	e.newSession("slow-term")
	conn, resp, err := websocket.Dial(t.Context(), e.url+"?machine=host&session=slow-term", nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	e.eventually("client attached", func() bool { return e.sh("tmux list-clients -t =slow-term -F '#{client_pid}' | wc -l") == "1" })
	e.sh("tmux send-keys -t =slow-term: 'yes | head -c 50M' Enter")
	e.eventually("slow client is dropped", func() bool { return e.h.Active() == 0 })
	if e.sh("tmux has-session -t =slow-term && echo alive") != "alive" {
		t.Fatal("tmux session ended with slow client")
	}
	if got := e.sh("tmux list-clients -t =slow-term -F '#{client_pid}' | wc -l"); got != "0" {
		t.Fatalf("tmux clients after drop=%s", got)
	}
	_, out := e.attach("slow-term", 80, 24)
	if len(out.text) == 0 { // the attach's initial redraw is asynchronous; at least ensure attach works.
		e.eventually("re-attached", func() bool { return e.h.Active() == 1 })
	}
}

func TestIntegrationDetachSendsExitFrame(t *testing.T) {
	e := setup(t)
	e.newSession("term-d")
	_, out := e.attach("term-d", 80, 24)
	e.eventually("attached", func() bool { return e.sh("tmux display -p -t =term-d: '#{session_attached}'") == "1" })
	e.sh("tmux detach-client -s =term-d")
	select {
	case c, ok := <-out.control:
		if !ok || c.Type != "exit" || c.Code == nil || *c.Code != 0 {
			t.Fatalf("control frame %+v (ok=%v)", c, ok)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no exit frame after detach")
	}
	e.eventually("ssh process gone", func() bool { return e.h.Active() == 0 })
}

func TestIntegrationSessionKilledDuringAttachHandshakeSendsExit(t *testing.T) {
	e := setup(t)
	const name = "term-killed-handshake"
	e.newSession(name)
	e.h.Start = func(ctx context.Context, argv []string, cols, rows int) (term.Process, error) {
		e.sh("tmux kill-session -t =" + name)
		return term.StartPTY(ctx, argv, cols, rows)
	}
	_, out := e.attach(name, 80, 24)
	select {
	case control, ok := <-out.control:
		if !ok || control.Type != "exit" || control.Code == nil || *control.Code != 1 {
			t.Fatalf("attach handshake exit frame = %+v (ok=%v); want remote exit", control, ok)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no exit frame after session was killed during attach handshake")
	}
	e.eventually("ssh process gone", func() bool { return e.h.Active() == 0 })
}

func TestIntegrationVim(t *testing.T) {
	e := setup(t)
	e.newSession("term-vim")
	conn, _ := e.attach("term-vim", 100, 30)
	send(t, conn, "vim ~/term-it.txt\r")
	e.eventually("vim running", func() bool {
		return e.sh("tmux display -p -t =term-vim: '#{pane_current_command}'") == "vim"
	})
	send(t, conn, "i")
	e.eventually("insert mode", func() bool { return strings.Contains(e.capture("term-vim"), "-- INSERT --") })
	send(t, conn, "hello from vim\x1b:wq\r")
	e.eventually("vim exited", func() bool {
		return e.sh("tmux display -p -t =term-vim: '#{pane_current_command}'") == "bash"
	})
	if got := e.sh("cat ~/term-it.txt"); got != "hello from vim" {
		t.Fatalf("file = %q", got)
	}
}

func TestIntegrationHtop(t *testing.T) {
	e := setup(t)
	e.newSession("term-htop")
	conn, out := e.attach("term-htop", 120, 40)
	send(t, conn, "htop\r")
	e.eventually("htop running", func() bool {
		return e.sh("tmux display -p -t =term-htop: '#{pane_current_command}'") == "htop"
	})
	out.waitFor(t, "F10") // htop's function-key bar reached the browser side
	send(t, conn, "q")
	e.eventually("htop quit", func() bool {
		return e.sh("tmux display -p -t =term-htop: '#{pane_current_command}'") == "bash"
	})
}

// osc52 matches an OSC 52 clipboard write (tmux sends an empty selection).
var osc52 = regexp.MustCompile(`\x1b\]52;[a-z0-9]*;([A-Za-z0-9+/=]+)(?:\x07|\x1b\\)`)

// A tmux copy-mode yank reaches the browser side as OSC 52 with tmux's
// defaults (set-clipboard external; TERM=xterm-256color has the clipboard
// feature): the tmux half of the M3 copy path.
func TestIntegrationCopyModeEmitsOSC52(t *testing.T) {
	e := setup(t)
	e.newSession("term-osc")
	conn, out := e.attach("term-osc", 100, 30)
	send(t, conn, "echo yank-$((6*7))\r")
	out.waitFor(t, "yank-42")
	e.eventually("output in the pane", func() bool { return strings.Contains(e.capture("term-osc"), "\nyank-42") })

	e.sh("tmux copy-mode -t =term-osc: && " +
		"tmux send-keys -t =term-osc: -X search-backward yank-42 && " +
		"tmux send-keys -t =term-osc: -X select-line && " +
		"tmux send-keys -t =term-osc: -X copy-selection-and-cancel")

	deadline := time.After(10 * time.Second)
	for {
		if m := osc52.FindStringSubmatch(out.seen.String()); m != nil {
			text, err := base64.StdEncoding.DecodeString(m[1])
			if err != nil {
				t.Fatalf("OSC 52 payload %q: %v", m[1], err)
			}
			if strings.TrimRight(string(text), "\n") != "yank-42" {
				t.Fatalf("OSC 52 carried %q, want the yanked line", text)
			}
			return
		}
		select {
		case chunk := <-out.text:
			out.seen.WriteString(chunk)
		case <-deadline:
			t.Fatalf("no OSC 52 in the terminal output after the yank: %q", out.seen.String())
		}
	}
}

// A client that vanished without closing (its network dropped: no reads, no
// pong) is dropped after the ping interval + timeout; its ssh process exits
// and the tmux session lives on for the client's reconnect.
func TestIntegrationSilentClientDropped(t *testing.T) {
	e := setup(t, func(h *term.Handler) {
		h.PingInterval = 300 * time.Millisecond
		h.PingTimeout = 300 * time.Millisecond
	})
	e.newSession("term-gone")
	url := e.url + "?machine=host&session=term-gone&cols=80&rows=24"
	conn, resp, err := websocket.Dial(t.Context(), url, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	// Never read: pings go unanswered, as over a cut network.
	e.eventually("attached", func() bool { return e.sh("tmux display -p -t =term-gone: '#{session_attached}'") == "1" })

	start := time.Now()
	e.eventually("ssh process gone", func() bool { return e.h.Active() == 0 })
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("dropped after %v, want about ping interval + timeout", d)
	}
	e.eventually("client detached", func() bool { return e.sh("tmux display -p -t =term-gone: '#{session_attached}'") == "0" })
	if got := e.sh("tmux has-session -t =term-gone && echo alive"); got != "alive" {
		t.Fatal("session died with the client")
	}
}

// The browser's liveness ping gets a pong through the real bridge.
func TestIntegrationClientPingPong(t *testing.T) {
	e := setup(t)
	e.newSession("term-ping")
	conn, out := e.attach("term-ping", 80, 24)
	if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-out.control:
		if c.Type != "pong" {
			t.Fatalf("control frame %+v, want pong", c)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no pong")
	}
}
