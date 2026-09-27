package tsauth

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAllowed(t *testing.T) {
	for _, tc := range []struct {
		list, login string
		want        bool
	}{
		{"Alice@example.com,tag:build", "alice@EXAMPLE.com", true},
		{"Alice@example.com", "tag:build", false},
		{"", "alice@example.com", false},
	} {
		if got := Allowed(tc.list, tc.login); got != tc.want {
			t.Fatalf("Allowed(%q,%q)=%v", tc.list, tc.login, got)
		}
	}
}

func TestClientWhoisShapeAndStatuses(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "tailscaled.sock")
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/localapi/v0/whois" || r.URL.Query().Get("addr") != "192.0.2.5" || r.Host != "local-tailscaled.sock" {
			t.Errorf("unexpected request: %s host=%s", r.URL, r.Host)
		}
		_, _ = w.Write([]byte(`{"UserProfile":{"LoginName":"User@Example.com"}}`))
	})}
	go func() { _ = server.Serve(ln) }()
	defer func() { _ = server.Close() }()
	login, err := New(socket).Login(context.Background(), "192.0.2.5")
	if err != nil || login != "user@example.com" {
		t.Fatalf("Login()=%q,%v", login, err)
	}
}

func TestClientUnknownNode(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "tailscaled.sock")
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })}
	go func() { _ = server.Serve(ln) }()
	defer func() { _ = server.Close() }()
	if _, err := New(socket).Login(context.Background(), "192.0.2.5"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("Login() error=%v, want ErrUnknown", err)
	}
}

func TestClientWhoisServerError(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "tailscaled.sock")
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })}
	go func() { _ = server.Serve(ln) }()
	defer func() { _ = server.Close() }()
	if _, err := New(socket).Login(context.Background(), "192.0.2.5"); err == nil {
		t.Fatal("expected server error")
	}
}

func TestClientReturnsTagForTaggedNode(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "tailscaled.sock")
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Node":{"Tags":["tag:Build"]}}`))
	})}
	go func() { _ = server.Serve(ln) }()
	defer func() { _ = server.Close() }()
	login, err := New(socket).Login(context.Background(), "192.0.2.5")
	if err != nil || login != "tag:build" {
		t.Fatalf("tag login=%q err=%v", login, err)
	}
}

func TestClientWhoisTimeout(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "tailscaled.sock")
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second)
		_, _ = w.Write([]byte(`{"UserProfile":{"LoginName":"late@example.com"}}`))
	})}
	go func() { _ = server.Serve(ln) }()
	defer func() { _ = server.Close() }()
	started := time.Now()
	if _, err := New(socket).Login(context.Background(), "192.0.2.5"); err == nil {
		t.Fatal("expected timeout")
	}
	if time.Since(started) > 2500*time.Millisecond {
		t.Fatal("whois exceeded its two second timeout")
	}
}

func TestCacheTTLAndCapacity(t *testing.T) {
	now := time.Now()
	calls := 0
	c := NewCache(func(context.Context, string) (string, error) { calls++; return "", nil })
	c.now = func() time.Time { return now }
	_, _ = c.Login(context.Background(), "one")
	_, _ = c.Login(context.Background(), "one")
	if calls != 1 {
		t.Fatalf("negative result not cached: calls=%d", calls)
	}
	now = now.Add(11 * time.Second)
	_, _ = c.Login(context.Background(), "one")
	if calls != 2 {
		t.Fatalf("negative TTL not expired: calls=%d", calls)
	}
	for i := 0; i < maxEntries+1; i++ {
		_, _ = c.Login(context.Background(), string(rune(i+1)))
	}
	if len(c.items) != maxEntries {
		t.Fatalf("cache size=%d", len(c.items))
	}
}

func TestCacheUsesShortTTLForRejectedIdentity(t *testing.T) {
	now := time.Now()
	calls := 0
	c := NewCache(func(context.Context, string) (string, error) { calls++; return "stranger@example.com", nil })
	c.now = func() time.Time { return now }
	_, _ = c.LoginFor(context.Background(), "one", "owner@example.com")
	now = now.Add(11 * time.Second)
	_, _ = c.LoginFor(context.Background(), "one", "owner@example.com")
	if calls != 2 {
		t.Fatalf("rejected identity TTL did not expire: calls=%d", calls)
	}
}

func TestMissingSocketFails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "absent")
	if _, err := New(p).Login(context.Background(), "192.0.2.5"); err == nil {
		t.Fatal("expected dial error")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("test socket unexpectedly exists")
	}
}
