// Package term bridges a browser WebSocket to `tmux attach-session` running
// under ssh in a PTY (docs/ARCHITECTURE.md §6). It holds no terminal state:
// closing the socket ends only the attach, never the tmux session.
package term

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os/exec"
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

	slowInputWriteThreshold = 50 * time.Millisecond
	slowSocketPingThreshold = 100 * time.Millisecond
	slowDiagnosticLogEvery  = 10 * time.Second
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
	// MaxPerUser and MaxTotal cap concurrent attaches per account and
	// server-wide. MaxPerUser <= 0 means no per-account cap.
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

// longLivedSSH is implemented by sshx.Client: an attach stays open, so it
// uses a long-lived ControlMaster instead of the one short commands share.
type longLivedSSH interface {
	LongLived(machine string) (opts []string, release func())
}

// AttachArgv returns the full argv for attaching to a session; sshOpts go
// before the alias.
func AttachArgv(ssh SSH, machine, session string, version tmux.Version, sshOpts ...string) ([]string, error) {
	attach, err := tmux.AttachArgs(session, version)
	if err != nil {
		return nil, err
	}
	args, err := ssh.Args(machine, append([]string{"-tt"}, sshOpts...), attach...)
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
	var sshOpts []string
	if l, ok := h.SSH.(longLivedSSH); ok {
		opts, release := l.LongLived(q.Get("machine"))
		defer release()
		sshOpts = opts
	}
	argv, err := AttachArgv(h.SSH, q.Get("machine"), q.Get("session"), version, sshOpts...)
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
			"error": "Too many open terminals (" + strconv.Itoa(h.Limit(account)) + ")",
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
	attachedAt := time.Now()
	log.Info("terminal attached")
	attachTimeout := h.AttachTimeout
	if attachTimeout <= 0 {
		attachTimeout = 10 * time.Second
	}
	detach := h.bridge(ctx, cancel, c, proc, log, attachTimeout, q.Get("machine"))
	attrs := []any{"reason", detach.reason, "duration_ms", time.Since(attachedAt).Milliseconds()}
	if detach.closeCode > 0 {
		attrs = append(attrs, "close_code", detach.closeCode)
	}
	if detach.hasExitCode {
		attrs = append(attrs, "exit_code", detach.exitCode)
	}
	log.Info("terminal detached", attrs...)
}

func (h *Handler) reserve(account string) bool {
	h.limitMu.Lock()
	defer h.limitMu.Unlock()
	if h.users == nil {
		h.users = make(map[string]int)
	}
	if h.full(account) {
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

func (h *Handler) total() int {
	if h.MaxTotal <= 0 {
		return 128
	}
	return h.MaxTotal
}

// full reports whether account can't take another slot. Callers hold limitMu.
func (h *Handler) full(account string) bool {
	if h.MaxPerUser > 0 && h.users[account] >= h.MaxPerUser {
		return true
	}
	return int(h.active.Load()) >= h.total()
}

// Limit returns the cap account runs into: its own when set and reached,
// otherwise the server-wide one.
func (h *Handler) Limit(account string) int {
	h.limitMu.Lock()
	defer h.limitMu.Unlock()
	if h.MaxPerUser > 0 && h.users[account] >= h.MaxPerUser {
		return h.MaxPerUser
	}
	return h.total()
}

// AtCapacity reports whether an account or the server has no attach slots.
func (h *Handler) AtCapacity(account string) bool {
	h.limitMu.Lock()
	defer h.limitMu.Unlock()
	return h.full(account)
}

type detachInfo struct {
	reason      string
	closeCode   int
	exitCode    int
	hasExitCode bool
}

// bridge pumps bytes both ways until the process exits or the socket
// closes, then makes sure the process is gone.
func (h *Handler) bridge(ctx context.Context, cancel context.CancelFunc, c *websocket.Conn, proc Process, log *slog.Logger, attachTimeout time.Duration, machine string) detachInfo {
	detach := detachInfo{reason: "context_cancelled"}
	out := make(chan []byte, outQueue)
	var queued atomic.Int64
	var wg sync.WaitGroup
	watchdog := time.NewTimer(attachTimeout)
	defer watchdog.Stop()
	watchingFirstOutput := true

	// PTY → queue. A full queue means a stalled client: drop it.
	stalled := make(chan struct{})
	clientClose := make(chan int, 1)
	var remoteProbeRunning atomic.Bool
	var lastSlowPingLog time.Time
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
		var probeID int64
		var lastSlowInputLog time.Time
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				clientClose <- int(websocket.CloseStatus(err))
				return
			}
			if typ == websocket.MessageBinary {
				started := time.Now()
				if _, err := proc.Write(data); err != nil {
					return
				}
				writeDuration := time.Since(started)
				if len(data) <= 64 && writeDuration >= slowInputWriteThreshold && (lastSlowInputLog.IsZero() || time.Since(lastSlowInputLog) >= slowDiagnosticLogEvery) {
					log.Warn("terminal PTY input write slow", "duration_ms", float64(writeDuration)/float64(time.Millisecond))
					lastSlowInputLog = time.Now()
				}
				if probeID > 0 {
					ack, _ := json.Marshal(Control{Type: "inputAck", ID: probeID, WriteMs: float64(writeDuration) / float64(time.Millisecond)})
					_ = write(ctx, c, websocket.MessageText, ack)
					probeID = 0
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
			case "inputProbe":
				probeID = ctl.ID
			case "remoteProbe":
				if probeSSH, ok := h.SSH.(longLivedSSH); ok && remoteProbeRunning.CompareAndSwap(false, true) {
					go func(id int64) {
						defer remoteProbeRunning.Store(false)
						probeCtx, probeCancel := context.WithTimeout(ctx, 5*time.Second)
						defer probeCancel()
						started := time.Now()
						sshOpts, release := probeSSH.LongLived(machine)
						defer release()
						argv, err := h.SSH.Args(machine, sshOpts, "true")
						if err == nil {
							cmd := exec.CommandContext(probeCtx, h.SSH.Binary(), argv...) //nolint:gosec // generated sshx argv; the remote command is fixed
							cmd.WaitDelay = time.Second
							err = cmd.Run()
						}
						ok := err == nil
						durationMs := float64(time.Since(started)) / float64(time.Millisecond)
						log.Info("terminal SSH probe completed", "duration_ms", durationMs, "ok", ok)
						ack, _ := json.Marshal(Control{Type: "remoteProbeAck", ID: id, DurationMs: durationMs, OK: &ok})
						_ = write(ctx, c, websocket.MessageText, ack)
					}(ctl.ID)
				}
			case "ping":
				pong := Control{Type: "pong", ID: ctl.ID}
				b, _ := json.Marshal(pong)
				_ = write(ctx, c, websocket.MessageText, b)
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
			select {
			case <-stalled:
				detach.reason = "output_queue_full"
			default:
				select {
				case code := <-clientClose:
					detach.reason = "client_disconnected"
					if code == int(websocket.StatusNormalClosure) {
						detach.reason = "client_closed"
					} else if code < 0 || code == int(websocket.StatusNoStatusRcvd) || code == int(websocket.StatusAbnormalClosure) {
						detach.reason = "abrupt_websocket_disconnect"
					}
					detach.closeCode = code
				default:
					detach.reason = "context_cancelled"
				}
			}
			break loop
		case <-stalled:
			detach.reason = "output_queue_full"
			detach.closeCode = int(websocket.StatusTryAgainLater)
			_ = c.Close(websocket.StatusTryAgainLater, "client too slow; reconnect")
			break loop
		case <-watchdog.C:
			detach.reason = "first_output_timeout"
			detach.closeCode = 4408
			_ = c.Close(websocket.StatusCode(4408), "host didn't answer")
			break loop
		case <-h.Shutdown:
			detach.reason = "hostbud_shutdown"
			detach.closeCode = int(websocket.StatusGoingAway)
			_ = c.Close(websocket.StatusGoingAway, "hostbud is restarting; reconnect")
			break loop
		case b, ok := <-out:
			if !ok {
				exited = true
				detach.reason = "process_exited"
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
					detach.reason = "output_write_failed"
					detach.closeCode = int(websocket.StatusTryAgainLater)
					_ = c.Close(websocket.StatusTryAgainLater, "client too slow; reconnect")
				}
				break loop
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, timeout)
			pingStarted := time.Now()
			err := c.Ping(pctx)
			pingDuration := time.Since(pingStarted)
			pcancel()
			if err != nil {
				detach.reason = "websocket_ping_failed"
				break loop
			}
			if pingDuration >= slowSocketPingThreshold && (lastSlowPingLog.IsZero() || time.Since(lastSlowPingLog) >= slowDiagnosticLogEvery) {
				log.Warn("terminal WebSocket ping slow", "duration_ms", float64(pingDuration)/float64(time.Millisecond))
				lastSlowPingLog = time.Now()
			}
		}
	}

	if !exited {
		_ = proc.Kill()
		_, _ = proc.Wait()
		cancel()
		wg.Wait()
		return detach
	}
	// The process ended (detach, or the session exited): tell the client
	// before cancelling ctx, which would close the socket.
	code, err := proc.Wait()
	if err != nil {
		code = -1
	}
	detach.reason = "process_exited"
	detach.exitCode = code
	detach.hasExitCode = true
	_ = write(ctx, c, websocket.MessageText, ExitFrame(code))
	_ = c.Close(websocket.StatusNormalClosure, "exited")
	cancel()
	wg.Wait()
	return detach
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
