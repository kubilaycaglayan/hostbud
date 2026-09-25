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
	pingInterval = 25 * time.Second
	writeTimeout = 10 * time.Second
	eventBuffer  = 64
)

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
	// Origin was checked by checkOrigin; the library's own same-host check
	// would reject the domain behind Caddy.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()
	ctx := c.CloseRead(r.Context()) // we never read; this handles close frames

	// Subscribe before the snapshot so nothing falls between them.
	ch, unsubscribe := s.cfg.Bus.Subscribe(eventBuffer)
	defer unsubscribe()

	snap := snapshot{Type: "snapshot", Machines: []inventory.Machine{}, Sessions: map[string][]tmux.Session{}}
	for _, id := range s.order {
		m, sessions := s.machines[id].Snapshot()
		snap.Machines = append(snap.Machines, m)
		snap.Sessions[id] = sessions
	}
	if write(ctx, c, snap) != nil {
		return
	}

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				_ = c.Close(websocket.StatusTryAgainLater, "fell behind; reconnect to resync")
				return
			}
			if write(ctx, c, e) != nil {
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
