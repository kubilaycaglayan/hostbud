package term

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Protocol (docs/ARCHITECTURE.md §6): binary frames carry terminal bytes in
// both directions; text frames carry JSON control messages.
//   client → server: {"type":"resize","cols":N,"rows":N} | {"type":"ping"}
//   server → client: {"type":"exit","code":N} | {"type":"pong"}

// Control is a JSON control frame.
type Control struct {
	Type       string  `json:"type"`
	Cols       int     `json:"cols,omitempty"`
	Rows       int     `json:"rows,omitempty"`
	Code       *int    `json:"code,omitempty"`
	ID         int64   `json:"id,omitempty"`
	WriteMs    float64 `json:"writeMs,omitempty"`
	DurationMs float64 `json:"durationMs,omitempty"`
	OK         *bool   `json:"ok,omitempty"`
}

const (
	maxCols = 1000
	maxRows = 500
)

// ParseControl decodes and validates a client control frame.
func ParseControl(b []byte) (Control, error) {
	var c Control
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("control frame: %w", err)
	}
	switch c.Type {
	case "ping", "inputProbe", "remoteProbe":
		return c, nil
	case "resize":
		if err := validSize(c.Cols, c.Rows); err != nil {
			return c, err
		}
		return c, nil
	default:
		return c, fmt.Errorf("control frame: unknown type %q", c.Type)
	}
}

func validSize(cols, rows int) error {
	if cols < 1 || cols > maxCols || rows < 1 || rows > maxRows {
		return errors.New("terminal size out of range")
	}
	return nil
}

// ExitFrame is sent when the attached process ends (detach, session exit).
func ExitFrame(code int) []byte {
	b, _ := json.Marshal(Control{Type: "exit", Code: &code})
	return b
}

// PongFrame answers a client ping.
func PongFrame() []byte { return []byte(`{"type":"pong"}`) }
