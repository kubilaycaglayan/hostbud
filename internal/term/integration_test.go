//go:build integration

package term_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
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

func setup(t *testing.T) *env {
	t.Helper()
	c := testenv.Connected(t, testenv.SSHD)
	testenv.Sh(t, c, "tmux kill-server 2>/dev/null; true")
	t.Cleanup(func() { testenv.Sh(t, c, "tmux kill-server 2>/dev/null; rm -f ~/term-it.txt; true") })
	h := &term.Handler{SSH: c}
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
