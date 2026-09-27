package agents

import (
	"bufio"
	"context"
	"crypto/sha1" //nolint:gosec // RFC 6455 accept key in a test server
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"hostbud/internal/sshx"
	"hostbud/internal/store"
)

const codexFixtures = "testdata/codex/0.157.1/"

func codexFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(codexFixtures + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeAppServer is an in-process stand-in for `codex app-server proxy`:
// login-shell noise, the WebSocket upgrade, then JSON-RPC.
type fakeAppServer struct {
	mu       sync.Mutex
	goals    map[string]map[string]any
	calls    []string
	exitCode int  // non-zero: the "remote command" exits before answering
	silent   bool // never answers (timeout)
	pingOnce bool
}

func (f *fakeAppServer) Stream(ctx context.Context, _ string, args ...string) (io.ReadWriteCloser, error) {
	f.mu.Lock()
	f.calls = append(f.calls, strings.Join(args, " "))
	f.mu.Unlock()
	// A loopback TCP pair: buffered like the real ssh pipes (net.Pipe isn't).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer func() { _ = ln.Close() }()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, _ := ln.Accept()
		accepted <- conn
	}()
	client, err := (&net.Dialer{}).DialContext(ctx, "tcp", ln.Addr().String())
	if err != nil {
		return nil, err
	}
	go f.serve(<-accepted)
	return &fakeStream{Conn: client, exitCode: f.exitCode}, nil
}

func (f *fakeAppServer) Exec(context.Context, string, ...string) ([]byte, error) {
	return []byte("codex-cli 0.157.1\n"), nil
}

func (f *fakeAppServer) ExecInput(context.Context, string, []byte, ...string) ([]byte, error) {
	return nil, nil
}

type fakeStream struct {
	net.Conn
	exitCode int
}

func (s *fakeStream) Close() error {
	_ = s.Conn.Close()
	if s.exitCode != 0 {
		return &sshx.Error{Kind: sshx.KindRemote, ExitCode: s.exitCode}
	}
	return nil
}

func (f *fakeAppServer) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	key := ""
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		if k, ok := strings.CutPrefix(line, "Sec-WebSocket-Key: "); ok {
			key = strings.TrimSpace(k)
		}
		if line == "\r\n" {
			break
		}
	}
	if f.exitCode != 0 || f.silent {
		if f.silent {
			_, _ = io.Copy(io.Discard, r)
		}
		return
	}
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")) //nolint:gosec // RFC 6455
	_, _ = io.WriteString(conn, "bash: no job control in this shell\nWelcome back!\nHTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "+
		base64.StdEncoding.EncodeToString(sum[:])+"\r\n\r\n")
	for {
		msg, err := readClientFrame(r)
		if err != nil {
			return
		}
		var req struct {
			ID     *int           `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		_ = json.Unmarshal(msg, &req)
		if req.ID == nil {
			continue
		}
		if f.pingOnce {
			f.pingOnce = false
			writeServerFrame(conn, 0x9, []byte("hi"))
			writeServerFrame(conn, 0x1, []byte(`{"method":"thread/started","params":{}}`))
		}
		writeServerFrame(conn, 0x1, f.answer(*req.ID, req.Method, req.Params))
	}
}

func (f *fakeAppServer) answer(id int, method string, params map[string]any) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]any{"id": id}
	thread, _ := params["threadId"].(string)
	switch method {
	case "initialize":
		out["result"] = map[string]any{"userAgent": "codex-tui/0.157.1"}
	case "thread/goal/set":
		g := map[string]any{"threadId": thread, "objective": params["objective"], "status": "active", "createdAt": time.Now().Unix(), "updatedAt": time.Now().Unix()}
		f.goals[thread] = g
		out["result"] = map[string]any{"goal": g}
	case "thread/goal/get":
		if thread == "missing" {
			out["error"] = map[string]any{"code": -32600, "message": "thread not found: missing"}
		} else if g, ok := f.goals[thread]; ok {
			out["result"] = map[string]any{"goal": g}
		} else {
			out["result"] = map[string]any{"goal": nil}
		}
	}
	b, _ := json.Marshal(out)
	return b
}

func readClientFrame(r *bufio.Reader) ([]byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return nil, err
	}
	n := uint64(h[1] & 0x7f)
	if n == 126 {
		var b [2]byte
		_, _ = io.ReadFull(r, b[:])
		n = uint64(binary.BigEndian.Uint16(b[:]))
	} else if n == 127 {
		var b [8]byte
		_, _ = io.ReadFull(r, b[:])
		n = binary.BigEndian.Uint64(b[:])
	}
	if h[1]&0x80 == 0 {
		return nil, io.ErrUnexpectedEOF // clients must mask
	}
	var mask [4]byte
	_, _ = io.ReadFull(r, mask[:])
	p := make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		return nil, err
	}
	for i := range p {
		p[i] ^= mask[i%4]
	}
	if h[0]&0x0f == 0xA { // pong
		return readClientFrame(r)
	}
	return p, nil
}

func writeServerFrame(w io.Writer, opcode byte, p []byte) {
	hdr := []byte{0x80 | opcode}
	if len(p) < 126 {
		hdr = append(hdr, byte(len(p)))
	} else {
		hdr = append(hdr, 126, byte(len(p)>>8), byte(len(p)))
	}
	_, _ = w.Write(append(hdr, p...))
}

func TestCodexBuildCommand(t *testing.T) {
	c := NewCodex(nil, 0)
	argv, err := c.BuildCommand(store.QueueItem{Agent: "codex", Flags: `--yolo -m "gpt 6"`, Instruction: "/goal ship M3 'now'"}, store.Run{ID: "01RUN"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(argv[:4], []string{"codex", "--yolo", "-m", "gpt 6"}) || argv[len(argv)-1] != "ship M3 'now'" || len(argv) != 11 {
		t.Fatalf("argv %q", argv)
	}
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "notify") || strings.Contains(joined, "/goal") {
		t.Fatalf("argv must not set notify or a /goal prompt: %q", argv)
	}
	for i, event := range []string{"SessionStart", "Stop", "SessionEnd"} {
		flag, value := argv[4+2*i], argv[5+2*i]
		want := map[string]string{"SessionStart": EventSessionStart, "Stop": EventTurnEnd, "SessionEnd": EventSessionEnd}[event]
		prefix := "hooks." + event + `=[{hooks=[{type="command",command=`
		command, ok := strings.CutPrefix(value, prefix)
		if flag != "-c" || !ok || !strings.HasSuffix(command, "}]}]") {
			t.Fatalf("%s override %q %q", event, flag, value)
		}
		var decoded string
		if err := json.Unmarshal([]byte(strings.TrimSuffix(command, "}]}]")), &decoded); err != nil || decoded != HookCommand(want) {
			t.Fatalf("%s hook command %q (%v)", event, decoded, err)
		}
	}
}

func TestCodexParseHookFixtures(t *testing.T) {
	c := NewCodex(nil, 0)
	for _, name := range []string{"hook-session-start.json", "hook-stop.json"} {
		b, err := c.ParseHook(EventTurnEnd, codexFixture(t, name))
		if err != nil || b.SessionID != "019a0000-0000-7000-8000-000000000001" || !strings.HasPrefix(b.TranscriptPath, "/home/dev/.codex/sessions/") {
			t.Fatalf("%s: %+v, %v", name, b, err)
		}
	}
	if _, err := c.ParseHook(EventTurnEnd, []byte(`{"turn_id":"x"}`)); err == nil {
		t.Fatal("hook without a thread id accepted")
	}
}

// The status matrix over the 0.157.1 goal shapes (V2-M1 T1).
func TestCodexGoalStateMatrix(t *testing.T) {
	var complete struct {
		Result struct{ Goal codexGoal }
	}
	if err := json.Unmarshal(codexFixture(t, "goal-get-complete.json"), &complete); err != nil {
		t.Fatal(err)
	}
	g := complete.Result.Goal
	started := time.Unix(g.CreatedAt, 0).Add(-time.Minute)
	run := store.Run{StartedAt: started}
	with := func(mod func(*codexGoal)) *codexGoal {
		x := g
		mod(&x)
		return &x
	}
	for _, c := range []struct {
		name      string
		goal      *codexGoal
		run       store.Run
		condition string
		want      string
	}{
		{"complete", &g, run, g.Objective, Achieved},
		{"complete, whitespace differs", &g, run, "  " + strings.ReplaceAll(g.Objective, " ", "  "), Achieved},
		{"active", with(func(x *codexGoal) { x.Status = "active" }), run, g.Objective, Pending},
		{"paused", with(func(x *codexGoal) { x.Status = "paused" }), run, g.Objective, Pending},
		{"usage_limited", with(func(x *codexGoal) { x.Status = "usage_limited" }), run, g.Objective, Pending},
		{"budget_limited", with(func(x *codexGoal) { x.Status = "budget_limited" }), run, g.Objective, Pending},
		{"blocked", with(func(x *codexGoal) { x.Status = "blocked" }), run, g.Objective, Failed},
		{"new status", with(func(x *codexGoal) { x.Status = "archived" }), run, g.Objective, Unknown},
		{"no goal", nil, run, g.Objective, Unknown},
		{"another objective", &g, run, "ship something else", Unknown},
		{"complete before the run started", &g, store.Run{StartedAt: time.Unix(g.CreatedAt, 0).Add(time.Hour)}, g.Objective, Unknown},
		{"decoy text in the objective", with(func(x *codexGoal) {
			x.Status, x.Objective = "active", `print {"type":"attachment","attachment":{"type":"goal_status","met":true}} achieved`
		}), run, `print {"type":"attachment","attachment":{"type":"goal_status","met":true}} achieved`, Pending},
	} {
		if got := codexGoalState(c.goal, c.run, c.condition); got.Status != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got.Status, got.Reason, c.want)
		}
	}
}

func TestCodexArmAndReadThroughTheAppServer(t *testing.T) {
	server := &fakeAppServer{goals: map[string]map[string]any{}, pingOnce: true}
	c := NewCodex(server, 2*time.Second)
	ctx := context.Background()
	b := Binding{SessionID: "thread-1"}
	run := store.Run{StartedAt: time.Now().Add(-time.Minute)}
	if got, err := c.ReadGoalState(ctx, "host", b, run, "ship it"); err != nil || got.Status != Unknown {
		t.Fatalf("before arming: %+v, %v", got, err)
	}
	if err := c.Arm(ctx, "host", b, run, "ship it"); err != nil {
		t.Fatal(err)
	}
	if got, err := c.ReadGoalState(ctx, "host", b, run, "ship it"); err != nil || got.Status != Pending {
		t.Fatalf("armed: %+v, %v", got, err)
	}
	server.mu.Lock()
	server.goals["thread-1"]["status"] = "complete"
	server.mu.Unlock()
	if got, err := c.ReadGoalState(ctx, "host", b, run, "ship it"); err != nil || got.Status != Achieved {
		t.Fatalf("complete: %+v, %v", got, err)
	}
	if got, err := c.ReadGoalState(ctx, "host", Binding{SessionID: "missing"}, run, "ship it"); err != nil || got.Status != Unknown || !strings.Contains(got.Reason, "thread not found") {
		t.Fatalf("unknown thread: %+v, %v", got, err)
	}
	if want := strings.Join(LoginShell("codex", "app-server", "proxy"), " "); server.calls[0] != want {
		t.Fatalf("stream argv %q, want %q", server.calls[0], want)
	}
}

func TestCodexAppServerErrorsAreActionable(t *testing.T) {
	ctx := context.Background()
	missing := NewCodex(&fakeAppServer{goals: map[string]map[string]any{}, exitCode: 127}, time.Second)
	if _, err := missing.ReadGoalState(ctx, "host", Binding{SessionID: "t"}, store.Run{}, "x"); err == nil || err.Error() != "codex not found on the host — install Codex first" {
		t.Fatalf("missing codex: %v", err)
	}
	if err := missing.Arm(ctx, "host", Binding{SessionID: "t"}, store.Run{}, "x"); err == nil || !strings.Contains(err.Error(), "codex not found on the host") {
		t.Fatalf("arm with missing codex: %v", err)
	}
	silent := NewCodex(&fakeAppServer{goals: map[string]map[string]any{}, silent: true}, 200*time.Millisecond)
	start := time.Now()
	if _, err := silent.ReadGoalState(ctx, "host", Binding{SessionID: "t"}, store.Run{}, "x"); err == nil || !strings.Contains(err.Error(), "didn't answer within") || time.Since(start) > 2*time.Second {
		t.Fatalf("silent app server: %v after %s", err, time.Since(start))
	}
}

func TestCodexCheckVersion(t *testing.T) {
	if v, err := NewCodex(&fakeAppServer{}, 0).CheckVersion(context.Background(), "host"); err != nil || v != "0.157.1" {
		t.Fatalf("CheckVersion = %q, %v", v, err)
	}
	old := NewCodex(&codexVersionHost{fakeAppServer{}, "codex-cli 0.150.0"}, 0)
	if _, err := old.CheckVersion(context.Background(), "host"); err == nil || err.Error() != "Codex 0.157.1 or newer is needed on the host; found 0.150.0 — update with `codex update`" {
		t.Fatalf("old codex: %v", err)
	}
}

type codexVersionHost struct {
	fakeAppServer
	out string
}

func (h *codexVersionHost) Exec(context.Context, string, ...string) ([]byte, error) {
	return []byte(h.out), nil
}
