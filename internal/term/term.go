// Package term bridges a browser WebSocket to `tmux attach-session` running
// under ssh in a PTY (docs/ARCHITECTURE.md §6). It holds no terminal state:
// closing the socket ends only the attach, never the tmux session.
package term

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"hostbud/internal/tmux"
)

const (
	readChunk    = 32 << 10
	maxInput     = 1 << 20 // one pasted message
	writeTimeout = 10 * time.Second
	// A client that doesn't answer a WebSocket ping within pingTimeout is
	// gone (e.g. its network dropped without a close): the attach ends.
	pingInterval = 25 * time.Second
	pingTimeout  = 10 * time.Second
	// outQueue bounds buffered output per client (× readChunk bytes); a
	// client that can't keep up is dropped and reconnects (tmux redraws).
	outQueue = 64
)

// SSH builds the ssh argv (sshx.Client).
type SSH interface {
	Args(machine string, sshOpts []string, args ...string) ([]string, error)
	Binary() string
}

// Handler serves /ws/term?machine=&session=&cols=&rows=. Mount it behind
// the Origin check (api.New does).
type Handler struct {
	SSH   SSH
	Log   *slog.Logger
	Start Starter // default StartPTY
	// Shutdown, when closed, ends every open terminal with "going away"
	// (a disconnect the client reconnects from), before ssh is torn down.
	Shutdown <-chan struct{}
	// PingInterval and PingTimeout override the WebSocket ping defaults
	// (25s, 10s); tests shorten them.
	PingInterval, PingTimeout time.Duration

	active atomic.Int64
}

// Active is the number of live attach processes.
func (h *Handler) Active() int { return int(h.active.Load()) }

// AttachArgv returns the full argv for attaching to a session.
func AttachArgv(ssh SSH, machine, session string) ([]string, error) {
	attach, err := tmux.AttachArgs(session)
	if err != nil {
		return nil, err
	}
	args, err := ssh.Args(machine, []string{"-tt"}, attach...)
	if err != nil {
		return nil, err
	}
	return append([]string{ssh.Binary()}, args...), nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := h.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	q := r.URL.Query()
	cols, rows := atoiOr(q.Get("cols"), 80), atoiOr(q.Get("rows"), 24)
	if err := validSize(cols, rows); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	argv, err := AttachArgv(h.SSH, q.Get("machine"), q.Get("session"))
	if err != nil {
		http.Error(w, "invalid machine or session: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Origin was checked by the api middleware.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()
	c.SetReadLimit(maxInput)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	start := h.Start
	if start == nil {
		start = StartPTY
	}
	proc, err := start(ctx, argv, cols, rows)
	if err != nil {
		log.Warn("can't start terminal", "err", err)
		_ = c.Close(websocket.StatusInternalError, "can't start ssh")
		return
	}
	h.active.Add(1)
	defer h.active.Add(-1)
	log.Info("terminal attached", "machine", q.Get("machine"), "session", q.Get("session"))
	h.bridge(ctx, cancel, c, proc, log)
	log.Info("terminal detached", "machine", q.Get("machine"), "session", q.Get("session"))
}

// bridge pumps bytes both ways until the process exits or the socket
// closes, then makes sure the process is gone.
func (h *Handler) bridge(ctx context.Context, cancel context.CancelFunc, c *websocket.Conn, proc Process, log *slog.Logger) {
	out := make(chan []byte, outQueue)
	var wg sync.WaitGroup

	// PTY → queue. A full queue means a stalled client: drop it.
	stalled := make(chan struct{})
	wg.Go(func() {
		defer close(out)
		for {
			buf := make([]byte, readChunk)
			n, err := proc.Read(buf)
			if n > 0 {
				select {
				case out <- buf[:n]:
				default:
					// The writer is stuck on a full socket: abort it now
					// (cancelling ctx fails the blocked write) and drop.
					close(stalled)
					cancel()
					return
				}
			}
			if err != nil {
				return
			}
		}
	})

	// Socket → PTY.
	go func() {
		defer cancel()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				if _, err := proc.Write(data); err != nil {
					return
				}
				continue
			}
			ctl, err := ParseControl(data)
			if err != nil {
				log.Debug("bad control frame", "err", err)
				continue
			}
			switch ctl.Type {
			case "resize":
				_ = proc.Resize(ctl.Cols, ctl.Rows)
			case "ping":
				_ = write(ctx, c, websocket.MessageText, PongFrame())
			}
		}
	}()

	interval, timeout := h.PingInterval, h.PingTimeout
	if interval <= 0 {
		interval = pingInterval
	}
	if timeout <= 0 {
		timeout = pingTimeout
	}
	ping := time.NewTicker(interval)
	defer ping.Stop()
	exited := false
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-stalled:
			_ = c.Close(websocket.StatusPolicyViolation, "client too slow; reconnect")
			break loop
		case <-h.Shutdown:
			_ = c.Close(websocket.StatusGoingAway, "hostbud is restarting; reconnect")
			break loop
		case b, ok := <-out:
			if !ok {
				exited = true
				break loop
			}
			if write(ctx, c, websocket.MessageBinary, b) != nil {
				break loop
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, timeout)
			err := c.Ping(pctx)
			pcancel()
			if err != nil {
				break loop
			}
		}
	}

	if !exited {
		_ = proc.Kill()
		_, _ = proc.Wait()
		cancel()
		wg.Wait()
		return
	}
	// The process ended (detach, or the session exited): tell the client
	// before cancelling ctx, which would close the socket.
	code, err := proc.Wait()
	if err != nil {
		code = -1
	}
	_ = write(ctx, c, websocket.MessageText, ExitFrame(code))
	_ = c.Close(websocket.StatusNormalClosure, "exited")
	cancel()
	wg.Wait()
}

func write(ctx context.Context, c *websocket.Conn, typ websocket.MessageType, b []byte) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return c.Write(ctx, typ, b)
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}
