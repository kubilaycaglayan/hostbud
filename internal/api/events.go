package api

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"hostbud/internal/inventory"
	"hostbud/internal/tmux"
)

const (
	defaultHeartbeat = 15 * time.Second
	pingInterval     = 25 * time.Second
	writeTimeout     = 10 * time.Second
	eventBuffer      = 64
)

// heartbeat is sent every Config.Heartbeat: a text frame the browser sees
// (WebSocket pings are invisible to page scripts), so it can tell a hung
// connection from a quiet one.
var heartbeat = struct {
	Type string `json:"type"`
}{Type: "heartbeat"}

// snapshot is the first /ws/events message; bus events follow as
// {type, machine, payload}.
type snapshot struct {
	Type     string                    `json:"type"` // "snapshot"
	Machines []inventory.Machine       `json:"machines"`
	Sessions map[string][]tmux.Session `json:"sessions"`
}

// eventsSocket streams state to the browser: a snapshot, then every bus
// event. A client that falls behind is disconnected and resyncs on
// reconnect (the next snapshot).
func (s *server) eventsSocket(w http.ResponseWriter, r *http.Request) {
	account := AuthenticatedUserID(r)
	if account == "" {
		account = "anonymous"
	}
	s.eventMu.Lock()
	if s.eventUsers[account] >= 16 {
		s.eventMu.Unlock()
		writeError(w, http.StatusTooManyRequests, "Too many open event sockets (16)", "Close another tab or device and try again.")
		return
	}
	s.eventUsers[account]++
	s.eventMu.Unlock()
	defer func() {
		s.eventMu.Lock()
		if s.eventUsers[account] <= 1 {
			delete(s.eventUsers, account)
		} else {
			s.eventUsers[account]--
		}
		s.eventMu.Unlock()
	}()
	// Origin was checked by checkOrigin; the library's own same-host check
	// would reject the domain behind Caddy.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c.SetReadLimit(1 << 20)
	input := make(chan error, 1)
	go func() {
		_, _, err := c.Read(ctx)
		input <- err
	}()

	// Subscribe before the snapshot so nothing falls between them.
	ch, unsubscribe := s.cfg.Bus.Subscribe(eventBuffer)
	defer unsubscribe()

	snap := snapshot{Type: "snapshot", Machines: []inventory.Machine{}, Sessions: map[string][]tmux.Session{}}
	for _, t := range s.machineList() {
		m, sessions := t.Snapshot()
		snap.Machines = append(snap.Machines, m)
		snap.Sessions[m.ID] = s.withProjectPlacement(r.Context(), m.ID, sessions)
	}
	if write(ctx, c, snap) != nil {
		return
	}

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	beat := time.NewTicker(s.cfg.Heartbeat)
	defer beat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-input:
			if err == nil {
				_ = c.Close(websocket.StatusUnsupportedData, "events socket is read-only")
			}
			return
		case e, ok := <-ch:
			if !ok {
				_ = c.Close(websocket.StatusTryAgainLater, "fell behind; reconnect to resync")
				return
			}
			if change, ok := e.Payload.(inventory.SessionsChanged); ok {
				change.Sessions = s.withProjectPlacement(ctx, e.Machine, change.Sessions)
				e.Payload = change
			}
			if write(ctx, c, e) != nil {
				return
			}
		case <-beat.C:
			if write(ctx, c, heartbeat) != nil {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func write(ctx context.Context, c *websocket.Conn, v any) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return wsjson.Write(ctx, c, v)
}
