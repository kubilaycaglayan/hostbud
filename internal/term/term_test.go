package term

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"hostbud/internal/tmux"
)

func TestParseControl(t *testing.T) {
	c, err := ParseControl([]byte(`{"type":"resize","cols":120,"rows":40}`))
	if err != nil || c.Type != "resize" || c.Cols != 120 || c.Rows != 40 {
		t.Fatalf("resize: %+v %v", c, err)
	}
	if c, err := ParseControl([]byte(`{"type":"ping"}`)); err != nil || c.Type != "ping" {
		t.Fatalf("ping: %+v %v", c, err)
	}
	for _, bad := range []string{`{"type":"resize","cols":0,"rows":10}`, `{"type":"resize","cols":10,"rows":9999}`,
		`{"type":"exec"}`, `not json`} {
		if _, err := ParseControl([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestServerFrames(t *testing.T) {
	if got := string(ExitFrame(0)); got != `{"type":"exit","code":0}` {
		t.Fatalf("exit frame %s", got)
	}
	if got := string(ExitFrame(130)); got != `{"type":"exit","code":130}` {
		t.Fatalf("exit frame %s", got)
	}
	if string(PongFrame()) != `{"type":"pong"}` {
		t.Fatal("pong frame")
	}
}

type fakeSSH struct{}

func (fakeSSH) Binary() string { return "ssh" }
func (fakeSSH) Args(machine string, opts []string, args ...string) ([]string, error) {
	return append(append(append([]string{"-F", "cfg"}, opts...), machine, "--"), args...), nil
}

func TestAttachArgv(t *testing.T) {
	got, err := AttachArgv(fakeSSH{}, "host", "my-session", tmux.Version{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ssh", "-F", "cfg", "-tt", "host", "--", "tmux", "attach-session", "-t", "=my-session"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
	// tmux 3.2+: synchronized redraws for this client (M8 T6).
	got, err = AttachArgv(fakeSSH{}, "host", "my-session", tmux.Version{Major: 3, Minor: 4})
	want = []string{"ssh", "-F", "cfg", "-tt", "host", "--", "tmux", "-T", "sync", "attach-session", "-t", "=my-session"}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := AttachArgv(fakeSSH{}, "host", "a.b", tmux.Version{}); err == nil {
		t.Fatal("invalid session name accepted")
	}
}

// fakeProc is a scripted Process.
type fakeProc struct {
	mu      sync.Mutex
	input   []byte
	resizes [][2]int
	killed  bool
	output  chan []byte // nil entry = EOF
	done    chan struct{}
	code    int
	endless bool
}

func newFake() *fakeProc {
	return &fakeProc{output: make(chan []byte, 16), done: make(chan struct{})}
}

func (p *fakeProc) Read(b []byte) (int, error) {
	if p.endless {
		select {
		case <-p.done:
			return 0, io.EOF
		default:
			return copy(b, strings.Repeat("x", len(b))), nil
		}
	}
	select {
	case chunk := <-p.output:
		if chunk == nil {
			return 0, io.EOF
		}
		return copy(b, chunk), nil
	case <-p.done:
		return 0, io.EOF
	}
}

func (p *fakeProc) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.input = append(p.input, b...)
	return len(b), nil
}

func (p *fakeProc) Resize(cols, rows int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resizes = append(p.resizes, [2]int{cols, rows})
	return nil
}

func (p *fakeProc) Wait() (int, error) { <-p.done; return p.code, nil }

func (p *fakeProc) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.killed {
		p.killed = true
		close(p.done)
	}
	return nil
}

func (p *fakeProc) exit(code int) {
	p.code = code
	p.output <- nil
	close(p.done)
}

func (p *fakeProc) snapshot() ([]byte, [][2]int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte{}, p.input...), append([][2]int{}, p.resizes...), p.killed
}

type started struct {
	argv       []string
	cols, rows int
}

func serve(t *testing.T, proc *fakeProc) (*Handler, string, chan started) {
	t.Helper()
	ch := make(chan started, 1)
	h := &Handler{SSH: fakeSSH{}, Start: func(_ context.Context, argv []string, cols, rows int) (Process, error) {
		ch <- started{argv, cols, rows}
		return proc, nil
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return h, "ws" + strings.TrimPrefix(srv.URL, "http"), ch
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	c, resp, err := websocket.Dial(t.Context(), url, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHandlerAttachesWithTheMachinesTmuxVersion(t *testing.T) {
	proc := newFake()
	h, url, startedCh := serve(t, proc)
	var asked string
	h.TmuxVersion = func(machine string) tmux.Version { asked = machine; return tmux.Version{Major: 3, Minor: 5} }
	_ = dial(t, url+"?machine=host&session=s1&cols=100&rows=30")
	st := <-startedCh
	if asked != "host" || !slices.Equal(st.argv[len(st.argv)-5:], []string{"-T", "sync", "attach-session", "-t", "=s1"}) {
		t.Fatalf("asked %q, started %q", asked, st.argv)
	}
}

func TestBridgeIOResizePingAndExit(t *testing.T) {
	proc := newFake()
	_, url, startedCh := serve(t, proc)
	c := dial(t, url+"?machine=host&session=s1&cols=100&rows=30")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	st := <-startedCh
	if st.cols != 100 || st.rows != 30 || !slices.Contains(st.argv, "=s1") {
		t.Fatalf("started %+v", st)
	}

	proc.output <- []byte("hello")
	typ, data, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || string(data) != "hello" {
		t.Fatalf("output: %v %q %v", typ, data, err)
	}

	if err := c.Write(ctx, websocket.MessageBinary, []byte("ls\r")); err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":90,"rows":20}`)); err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	typ, data, err = c.Read(ctx)
	if err != nil || typ != websocket.MessageText || string(data) != `{"type":"pong"}` {
		t.Fatalf("pong: %v %s %v", typ, data, err)
	}
	eventually(t, "input and resize", func() bool {
		in, rs, _ := proc.snapshot()
		return string(in) == "ls\r" && len(rs) == 1 && rs[0] == [2]int{90, 20}
	})

	proc.exit(3)
	typ, data, err = c.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		t.Fatalf("exit frame: %v %s %v", typ, data, err)
	}
	var ctl Control
	if json.Unmarshal(data, &ctl) != nil || ctl.Type != "exit" || ctl.Code == nil || *ctl.Code != 3 {
		t.Fatalf("exit frame: %s", data)
	}
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("close after exit: %v", err)
	}
}

func TestAttachCapRejectsBeforeStartAndReleasesOnExit(t *testing.T) {
	var starts atomic.Int32
	h := &Handler{SSH: fakeSSH{}, MaxPerUser: 1, MaxTotal: 2, AccountID: func(*http.Request) string { return "u1" }, Start: func(_ context.Context, _ []string, _, _ int) (Process, error) {
		starts.Add(1)
		return newFake(), nil
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "?machine=host&session=s1"
	c := dial(t, url)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http"+strings.TrimPrefix(srv.URL, "http")+"?machine=host&session=s1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusTooManyRequests || starts.Load() != 1 {
		t.Fatalf("second attach status=%d starts=%d", resp.StatusCode, starts.Load())
	}
	_ = c.Close(websocket.StatusNormalClosure, "close")
	eventually(t, "released terminal slot", func() bool { return h.Active() == 0 })
	third := dial(t, url)
	_ = third.Close(websocket.StatusNormalClosure, "close")
	eventually(t, "third attach starts", func() bool { return starts.Load() == 2 })
}

func TestOversizeQueryRejected(t *testing.T) {
	started := false
	h := &Handler{SSH: fakeSSH{}, Start: func(context.Context, []string, int, int) (Process, error) { started = true; return newFake(), nil }}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ws/term?x="+strings.Repeat("a", 2049), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || started {
		t.Fatalf("status=%d started=%v", w.Code, started)
	}
}

func TestOutputQueueUsesByteLimit(t *testing.T) {
	var queued atomic.Int64
	if !reserveOutput(&queued, maxQueuedOutput-1) {
		t.Fatal("queue rejected bytes below cap")
	}
	if reserveOutput(&queued, 2) {
		t.Fatal("queue accepted bytes over cap")
	}
	if got := queued.Load(); got != maxQueuedOutput-1 {
		t.Fatalf("queued=%d after rejection", got)
	}
	queued.Store(maxQueuedOutput - readChunk)
	if !reserveOutput(&queued, readChunk) || queued.Load() != maxQueuedOutput {
		t.Fatal("queue rejected exact cap")
	}
}

func TestConcurrentTerminalReservationsNeverOvershoot(t *testing.T) {
	h := &Handler{MaxPerUser: 64, MaxTotal: 20}
	var wg sync.WaitGroup
	var acquired atomic.Int32
	for i := 0; i < 64; i++ {
		wg.Go(func() {
			if h.reserve("u1") {
				acquired.Add(1)
			}
		})
	}
	wg.Wait()
	if got := acquired.Load(); got != 20 {
		t.Fatalf("reserved %d slots, want 20", got)
	}
	for i := int32(0); i < acquired.Load(); i++ {
		h.release("u1")
	}
	if h.Active() != 0 {
		t.Fatalf("active after release = %d", h.Active())
	}
}

func TestSocketCloseKillsProcess(t *testing.T) {
	proc := newFake()
	h, url, _ := serve(t, proc)
	c := dial(t, url+"?machine=host&session=s1")
	eventually(t, "attached", func() bool { return h.Active() == 1 })
	_ = c.Close(websocket.StatusNormalClosure, "tab closed")
	eventually(t, "process killed", func() bool { _, _, killed := proc.snapshot(); return killed })
	eventually(t, "handler done", func() bool { return h.Active() == 0 })
}

func TestStalledClientIsDropped(t *testing.T) {
	proc := newFake()
	proc.endless = true
	h, url, _ := serve(t, proc)
	c := dial(t, url+"?machine=host&session=s1")
	_ = c // never read: the client stalls
	eventually(t, "stalled client dropped", func() bool { _, _, killed := proc.snapshot(); return killed })
	eventually(t, "handler done", func() bool { return h.Active() == 0 })
}

func TestAttachWatchdogClosesSilentHostWith4408(t *testing.T) {
	proc := newFake()
	h, url, _ := serve(t, proc)
	h.AttachTimeout = 80 * time.Millisecond
	c := dial(t, url+"?machine=host&session=s1")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if websocket.CloseStatus(err) != 4408 || !strings.Contains(err.Error(), "host didn't answer") {
		t.Fatalf("watchdog close = %v, status %d", err, websocket.CloseStatus(err))
	}
	eventually(t, "silent attach killed", func() bool { _, _, killed := proc.snapshot(); return killed })
}

func TestAttachWatchdogStopsAfterFirstOutput(t *testing.T) {
	proc := newFake()
	h, url, _ := serve(t, proc)
	h.AttachTimeout = 80 * time.Millisecond
	c := dial(t, url+"?machine=host&session=s1")
	proc.output <- []byte("ready")
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	typ, data, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || string(data) != "ready" {
		t.Fatalf("initial output: type=%v data=%q err=%v", typ, data, err)
	}
	time.Sleep(120 * time.Millisecond)
	_, _, killed := proc.snapshot()
	if killed {
		t.Fatal("attach watchdog killed process after output")
	}
	_ = c.Close(websocket.StatusNormalClosure, "done")
}

func TestBadRequests(t *testing.T) {
	_, url, _ := serve(t, newFake())
	httpURL := "http" + strings.TrimPrefix(url, "ws")
	for _, q := range []string{"?machine=host&session=a.b", "?machine=host&session=", "?machine=host&session=s&cols=0",
		"?machine=host&session=s&rows=abc"} {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, httpURL+q, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d", q, resp.StatusCode)
		}
	}
}

func TestShutdownClosesWithGoingAway(t *testing.T) {
	proc := newFake()
	shutdown := make(chan struct{})
	h, url, _ := serve(t, proc)
	h.Shutdown = shutdown
	c := dial(t, url+"?machine=host&session=s1")
	eventually(t, "attached", func() bool { return h.Active() == 1 })
	close(shutdown)
	_, _, err := c.Read(context.Background())
	if websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Fatalf("close status: %v", err)
	}
	eventually(t, "process killed", func() bool { _, _, killed := proc.snapshot(); return killed })
}
