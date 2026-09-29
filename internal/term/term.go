// Package term bridges a browser WebSocket to `tmux attach-session` running
// under ssh in a PTY (docs/ARCHITECTURE.md §6). It holds no terminal state:
// closing the socket ends only the attach, never the tmux session.
package term

import (
	"context"
	"encoding/json"
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
	// maxQueuedOutput bounds buffered terminal bytes per client.
	maxQueuedOutput = 2 << 20
	outQueue        = 64
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
	// MaxPerUser and MaxTotal configure attach caps (enforcement is added in M7 T4).
	MaxPerUser, MaxTotal int
	AccountID            func(*http.Request) string
	// Shutdown, when closed, ends every open terminal with "going away"
	// (a disconnect the client reconnects from), before ssh is torn down.
	Shutdown <-chan struct{}
	// PingInterval and PingTimeout override the WebSocket ping defaults
	// (25s, 10s); tests shorten them.
	PingInterval, PingTimeout time.Duration
	AttachTimeout             time.Duration
	// TmuxVersion reports a machine's tmux version (zero when unknown), which
	// picks the attach flags (tmux.AttachArgs).
	TmuxVersion func(machine string) tmux.Version

	active  atomic.Int64
	limitMu sync.Mutex
	users   map[string]int
}

// Active is the number of live attach processes.
func (h *Handler) Active() int { return int(h.active.Load()) }

// AttachArgv returns the full argv for attaching to a session.
func AttachArgv(ssh SSH, machine, session string, version tmux.Version) ([]string, error) {
	attach, err := tmux.AttachArgs(session, version)
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
	if len(r.URL.RawQuery) > 2<<10 {
		http.Error(w, "query string too long", http.StatusBadRequest)
		return
	}
	cols, rows := atoiOr(q.Get("cols"), 80), atoiOr(q.Get("rows"), 24)
	if err := validSize(cols, rows); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var version tmux.Version
	if h.TmuxVersion != nil {
		version = h.TmuxVersion(q.Get("machine"))
	}
	argv, err := AttachArgv(h.SSH, q.Get("machine"), q.Get("session"), version)
	if err != nil {
		http.Error(w, "invalid machine or session: "+err.Error(), http.StatusBadRequest)
		return
	}
	account := "anonymous"
	if h.AccountID != nil {
		account = h.AccountID(r)
	}
	if !h.reserve(account) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "Too many open terminals (" + strconv.Itoa(h.PerUserLimit()) + ")",
			"hint":  "Close some tabs or panes; each open terminal keeps an ssh process on the host.",
		})
		return
	}
	released := true
	defer func() {
		if released {
			h.release(account)
		}
	}()

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
	released = false
	defer h.release(account)
	log.Info("terminal attached")
	attachTimeout := h.AttachTimeout
	if attachTimeout <= 0 {
		attachTimeout = 10 * time.Second
	}
	h.bridge(ctx, cancel, c, proc, log, attachTimeout)
	log.Info("terminal detached")
}

func (h *Handler) reserve(account string) bool {
	h.limitMu.Lock()
	defer h.limitMu.Unlock()
	if h.users == nil {
		h.users = make(map[string]int)
	}
	perUser, total := h.MaxPerUser, h.MaxTotal
	if perUser <= 0 {
		perUser = 32
	}
	if total <= 0 {
		total = 128
	}
	if h.users[account] >= perUser || int(h.active.Load()) >= total {
		return false
	}
	h.users[account]++
	h.active.Add(1) // reservations count before upgrade/start
	return true
}

func (h *Handler) release(account string) {
	h.limitMu.Lock()
	defer h.limitMu.Unlock()
	if h.users[account] <= 1 {
		delete(h.users, account)
	} else {
		h.users[account]--
	}
	h.active.Add(-1)
}

// PerUserLimit returns the effective per-account attach cap.
func (h *Handler) PerUserLimit() int {
	if h.MaxPerUser <= 0 {
		return 50
	}
	return h.MaxPerUser
}

// AtCapacity reports whether an account or the server has no attach slots.
func (h *Handler) AtCapacity(account string) bool {
	h.limitMu.Lock()
	defer h.limitMu.Unlock()
	perUser, total := h.PerUserLimit(), h.MaxTotal
	if total <= 0 {
		total = 128
	}
	return h.users[account] >= perUser || int(h.active.Load()) >= total
}

// bridge pumps bytes both ways until the process exits or the socket
// closes, then makes sure the process is gone.
func (h *Handler) bridge(ctx context.Context, cancel context.CancelFunc, c *websocket.Conn, proc Process, log *slog.Logger, attachTimeout time.Duration) {
	out := make(chan []byte, outQueue)
	var queued atomic.Int64
	var wg sync.WaitGroup
	watchdog := time.NewTimer(attachTimeout)
	defer watchdog.Stop()
	watchingFirstOutput := true

	// PTY → queue. A full queue means a stalled client: drop it.
	stalled := make(chan struct{})
	wg.Go(func() {
		defer close(out)
		for {
			buf := make([]byte, readChunk)
			n, err := proc.Read(buf)
			if n > 0 {
				if !reserveOutput(&queued, int64(n)) {
					close(stalled)
					cancel()
					return
				}
				select {
				case out <- buf[:n]:
				default:
					// The writer is stuck on a full socket: abort it now
					// (cancelling ctx fails the blocked write) and drop.
					close(stalled)
					queued.Add(-int64(n))
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
			_ = c.Close(websocket.StatusTryAgainLater, "client too slow; reconnect")
			break loop
		case <-watchdog.C:
			_ = c.Close(websocket.StatusCode(4408), "host didn't answer")
			break loop
		case <-h.Shutdown:
			_ = c.Close(websocket.StatusGoingAway, "hostbud is restarting; reconnect")
			break loop
		case b, ok := <-out:
			if !ok {
				exited = true
				break loop
			}
			if watchingFirstOutput {
				watchdog.Stop()
				watchingFirstOutput = false
			}
			err := write(ctx, c, websocket.MessageBinary, b)
			queued.Add(-int64(len(b)))
			if err != nil {
				if ctx.Err() == nil {
					_ = c.Close(websocket.StatusTryAgainLater, "client too slow; reconnect")
				}
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

func reserveOutput(queued *atomic.Int64, size int64) bool {
	for {
		current := queued.Load()
		if size <= 0 || current+size > maxQueuedOutput {
			return false
		}
		if queued.CompareAndSwap(current, current+size) {
			return true
		}
	}
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
