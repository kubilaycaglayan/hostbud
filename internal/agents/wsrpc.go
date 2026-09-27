package agents

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6455 accept key, not a security hash
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// A minimal WebSocket client for JSON-RPC over a byte stream (Codex's
// `codex app-server proxy` relays stdio to the daemon's control socket,
// which speaks WebSocket). Text frames only; client frames are masked.

const maxRPCMessage = 1 << 20

type rpcConn struct {
	w      io.Writer
	r      *bufio.Reader
	nextID int
}

// RPCError is a JSON-RPC error response.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("%s (code %d)", e.Message, e.Code) }

var errNoHandshake = errors.New("no WebSocket handshake from the Codex app server")

// dialRPC upgrades the stream. Lines before the HTTP status line (login
// shell noise) are skipped.
func dialRPC(rw io.ReadWriter) (*rpcConn, error) {
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, err
	}
	k := base64.StdEncoding.EncodeToString(key[:])
	req := "GET / HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: " + k + "\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(rw, req); err != nil {
		return nil, err
	}
	r := bufio.NewReaderSize(rw, 64<<10)
	status := ""
	for range 200 {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, errNoHandshake
		}
		if strings.HasPrefix(line, "HTTP/1.1 ") {
			status = strings.TrimSpace(line)
			break
		}
	}
	if !strings.HasPrefix(status, "HTTP/1.1 101") {
		return nil, fmt.Errorf("%w: %q", errNoHandshake, status)
	}
	sum := sha1.Sum([]byte(k + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")) //nolint:gosec // RFC 6455
	want := base64.StdEncoding.EncodeToString(sum[:])
	accepted := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, errNoHandshake
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Sec-WebSocket-Accept") && strings.TrimSpace(value) == want {
			accepted = true
		}
	}
	if !accepted {
		return nil, fmt.Errorf("%w: bad Sec-WebSocket-Accept", errNoHandshake)
	}
	return &rpcConn{w: rw, r: r}, nil
}

func (c *rpcConn) writeFrame(opcode byte, payload []byte) error {
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	hdr := []byte{0x80 | opcode}
	switch n := len(payload); {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n <= 0xffff:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 0x80|127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	hdr = append(hdr, mask[:]...)
	out := make([]byte, len(payload))
	for i, b := range payload {
		out[i] = b ^ mask[i%4]
	}
	_, err := c.w.Write(append(hdr, out...))
	return err
}

// readMessage returns the next text message, answering pings.
func (c *rpcConn) readMessage() ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(c.r, h[:]); err != nil {
			return nil, err
		}
		fin, opcode, masked := h[0]&0x80 != 0, h[0]&0x0f, h[1]&0x80 != 0
		n := uint64(h[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(c.r, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(c.r, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > maxRPCMessage || uint64(len(msg))+n > maxRPCMessage {
			return nil, errors.New("Codex app server message too large")
		}
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(c.r, mask[:]); err != nil {
				return nil, err
			}
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(c.r, payload); err != nil {
			return nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}
		switch opcode {
		case 0x8:
			return nil, errors.New("the Codex app server closed the connection")
		case 0x9:
			if err := c.writeFrame(0xA, payload); err != nil {
				return nil, err
			}
			continue
		case 0xA:
			continue
		}
		msg = append(msg, payload...)
		if fin {
			return msg, nil
		}
	}
}

// notify sends a JSON-RPC notification.
func (c *rpcConn) notify(method string) error {
	b, _ := json.Marshal(map[string]any{"method": method})
	return c.writeFrame(0x1, b)
}

// call sends a request and waits for its response, skipping notifications.
func (c *rpcConn) call(method string, params, result any) error {
	c.nextID++
	id := c.nextID
	b, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	if err := c.writeFrame(0x1, b); err != nil {
		return err
	}
	for {
		msg, err := c.readMessage()
		if err != nil {
			return err
		}
		var resp struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *RPCError       `json:"error"`
		}
		if err := json.Unmarshal(msg, &resp); err != nil || resp.ID == nil || *resp.ID != id {
			continue // a notification or another message
		}
		if resp.Error != nil {
			return resp.Error
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(resp.Result, result)
	}
}
