package auth

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hostbud/internal/store"
)

var fastParams = Params{Memory: 1024, Time: 1, Threads: 1}

var testLimits = Limits{LoginMax: 3, RegisterMax: 3, IPMax: 5,
	BlockBase: time.Second, BlockMax: 8 * time.Second, Multiplier: 2, Window: time.Hour}

func newSvc(t *testing.T) (*Service, *memRepo, *clock, *bytes.Buffer) {
	t.Helper()
	repo, clk, logs := newMem(), &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, &bytes.Buffer{}
	s, err := New(repo, Config{
		Limits: testLimits, Params: fastParams, Key: []byte("0123456789abcdef0123"),
		Log: slog.New(slog.NewTextHandler(logs, nil)), Now: clk.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, repo, clk, logs
}

const pw = "correct horse battery"

func TestNormalizeEmail(t *testing.T) {
	for in, want := range map[string]string{
		"  Person@Example.COM ":   "person@example.com",
		"a.b+tag@sub.example.org": "a.b+tag@sub.example.org",
	} {
		if got, err := NormalizeEmail(in); err != nil || got != want {
			t.Errorf("NormalizeEmail(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "person", "@example.com", "person@", "a@b@example.com", "a b@example.com",
		"person@localhost", "person@.example.com", "person@example.com.", "p\x00@example.com"} {
		if _, err := NormalizeEmail(bad); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("NormalizeEmail(%q) accepted", bad)
		}
	}
	if Redact("person@example.com") != "p***@example.com" || Redact("nope") != "***" {
		t.Error("Redact")
	}
}

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword(pw, fastParams)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=1024,t=1,p=1$") || strings.Contains(h, pw) {
		t.Fatalf("encoding %q", h)
	}
	if h2, _ := HashPassword(pw, fastParams); h2 == h {
		t.Fatal("no per-hash salt")
	}
	if ok, err := VerifyPassword(h, pw); !ok || err != nil {
		t.Fatal("correct password rejected")
	}
	if ok, _ := VerifyPassword(h, pw+"x"); ok {
		t.Fatal("wrong password accepted")
	}
	for _, bad := range []string{"", "$bcrypt$x", strings.Replace(h, "argon2id", "argon2i", 1), h[:len(h)-50]} {
		if ok, err := VerifyPassword(bad, pw); ok || err == nil {
			t.Errorf("bad hash %q accepted", bad)
		}
	}
	if CheckPassword("short") == nil || CheckPassword(strings.Repeat("x", 1025)) == nil || CheckPassword(pw) != nil {
		t.Fatal("CheckPassword")
	}
	if DefaultParams.Memory < 19*1024 || DefaultParams.Time < 2 {
		t.Fatal("production Argon2id parameters below the OWASP baseline")
	}
}

func TestULIDAndTokens(t *testing.T) {
	a := newULID(time.UnixMilli(1_700_000_000_000))
	b := newULID(time.UnixMilli(1_700_000_000_001))
	if len(a) != 26 || strings.Trim(a, crockford) != "" || a >= b {
		t.Fatalf("ULIDs %q %q", a, b)
	}
	t1, t2 := newToken(), newToken()
	if len(t1) < 43 || t1 == t2 || hashToken(t1) == t1 || len(hashToken(t1)) != 64 {
		t.Fatal("tokens")
	}
}

func TestClientIPTrustsOnlyTheProxy(t *testing.T) {
	proxies, err := ParsePrefixes("172.16.0.0/12, 127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		remote, xff, want string
	}{
		{"203.0.113.9:1234", "198.51.100.1", "203.0.113.9"},             // untrusted peer: header ignored
		{"172.18.0.5:1234", "198.51.100.1", "198.51.100.1"},             // Caddy
		{"172.18.0.5:1234", "6.6.6.6, 198.51.100.1", "198.51.100.1"},    // spoofed leftmost ignored
		{"172.18.0.5:1234", "198.51.100.1, 172.18.0.9", "198.51.100.1"}, // skip trusted hops
		{"172.18.0.5:1234", "", "172.18.0.5"},                           // no header
		{"172.18.0.5:1234", "garbage", "172.18.0.5"},
	}
	for _, c := range cases {
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := ClientIP(r, proxies); got != c.want {
			t.Errorf("remote %s xff %q: %s, want %s", c.remote, c.xff, got, c.want)
		}
	}
	r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
	r.RemoteAddr = "203.0.113.9:1"
	r.Header.Set("X-Forwarded-Proto", "https")
	if IsHTTPS(r, proxies) {
		t.Error("untrusted X-Forwarded-Proto honoured")
	}
	r.RemoteAddr = "172.18.0.5:1"
	if !IsHTTPS(r, proxies) {
		t.Error("Caddy's X-Forwarded-Proto ignored")
	}
	if _, err := ParsePrefixes("10.0.0.0/33"); err == nil {
		t.Error("bad CIDR accepted")
	}
}

func TestRegisterRequiresWhitelist(t *testing.T) {
	s, repo, _, _ := newSvc(t)
	ctx := context.Background()
	if err := s.Register(ctx, "person@example.com", pw, "198.51.100.1"); !errors.Is(err, ErrRegistrationFailed) {
		t.Fatalf("unlisted: %v", err)
	}
	repo.allow["person@example.com"] = true
	if err := s.Register(ctx, " Person@Example.com ", pw, "198.51.100.1"); err != nil {
		t.Fatalf("listed: %v", err)
	}
	u := repo.users["person@example.com"]
	if u.Email != "Person@Example.com" || !strings.HasPrefix(u.PasswordHash, "$argon2id$") || len(u.ID) != 26 {
		t.Fatalf("user %+v", u)
	}
	// Already registered: same generic error as unlisted.
	if err := s.Register(ctx, "person@example.com", pw, "198.51.100.1"); !errors.Is(err, ErrRegistrationFailed) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := s.Register(ctx, "x@example.com", "short", "ip"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak: %v", err)
	}
	if err := s.Register(ctx, "nope", pw, "ip"); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("invalid email: %v", err)
	}
}

func register(t *testing.T, s *Service, repo *memRepo, email string) {
	t.Helper()
	repo.allow[email] = true
	if err := s.Register(context.Background(), email, pw, "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
}

func TestLoginSessionsAndRotation(t *testing.T) {
	s, repo, clk, _ := newSvc(t)
	ctx := context.Background()
	register(t, s, repo, "person@example.com")

	tok, exp, err := s.Login(ctx, "PERSON@example.com", pw, "198.51.100.1", "test-agent", "")
	if err != nil {
		t.Fatal(err)
	}
	if !exp.Equal(clk.now().Add(s.SessionTTL())) {
		t.Fatalf("expiry %v", exp)
	}
	if _, stored := repo.sessions[tok]; stored {
		t.Fatal("raw token stored")
	}
	if u, err := s.Authenticate(ctx, tok); err != nil || u.EmailNormalized != "person@example.com" {
		t.Fatalf("authenticate: %+v %v", u, err)
	}

	// Signing in again rotates: the old token stops working.
	tok2, _, err := s.Login(ctx, "person@example.com", pw, "198.51.100.1", "", tok)
	if err != nil || tok2 == tok {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, tok); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("old token survived rotation")
	}

	if err := s.Logout(ctx, tok2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, tok2); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("token survived logout")
	}

	tok3, _, _ := s.Login(ctx, "person@example.com", pw, "198.51.100.1", "", "")
	clk.add(s.SessionTTL() + time.Second)
	if _, err := s.Authenticate(ctx, tok3); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("expired session accepted")
	}
	if _, err := s.Authenticate(ctx, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("empty token accepted")
	}
}

func TestLoginFailuresAreGeneric(t *testing.T) {
	s, repo, _, _ := newSvc(t)
	ctx := context.Background()
	register(t, s, repo, "person@example.com")
	register(t, s, repo, "off@example.com")
	repo.allow["off@example.com"] = false // owner disabled the address

	cases := map[string][2]string{
		"wrong password": {"person@example.com", pw + "!"},
		"unknown user":   {"ghost@example.com", pw},
		"disabled row":   {"off@example.com", pw},
		"invalid email":  {"not-an-email", pw},
	}
	for name, c := range cases {
		// Fresh IP each time so throttling doesn't interfere.
		_, _, err := s.Login(ctx, c[0], c[1], "198.51.100."+name[:1], "", "")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A disabled user is refused even with a whitelisted address.
	u := repo.users["person@example.com"]
	u.Disabled = true
	repo.users["person@example.com"] = u
	if _, _, err := s.Login(ctx, "person@example.com", pw, "203.0.113.50", "", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled user: %v", err)
	}
}

func retryAfter(err error) time.Duration {
	var rl *RateLimitedError
	if errors.As(err, &rl) {
		return rl.RetryAfter
	}
	return 0
}

func TestLoginBackoffGrowsToCeiling(t *testing.T) {
	s, repo, clk, _ := newSvc(t)
	ctx := context.Background()
	register(t, s, repo, "person@example.com")
	bad := func() error {
		_, _, err := s.Login(ctx, "person@example.com", "wrong-password", "198.51.100.1", "", "")
		return err
	}

	for i := 1; i <= 3; i++ {
		if err := bad(); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("failure %d: %v", i, err)
		}
	}
	var waits []time.Duration
	for range 4 {
		waits = append(waits, retryAfter(bad()))
	}
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("Retry-After sequence %v, want %v", waits, want)
		}
	}
	// Even the right password is refused while blocked (checked first).
	if _, _, err := s.Login(ctx, "person@example.com", pw, "198.51.100.1", "", ""); retryAfter(err) == 0 {
		t.Fatalf("correct password while blocked: %v", err)
	}
	clk.add(9 * time.Second)
	if _, _, err := s.Login(ctx, "person@example.com", pw, "198.51.100.1", "", ""); err != nil {
		t.Fatalf("after the block: %v", err)
	}
	if RetryAfterSeconds(1500*time.Millisecond) != 2 || RetryAfterSeconds(0) != 1 {
		t.Fatal("RetryAfterSeconds")
	}
}

func TestIPBucketStopsEmailRotation(t *testing.T) {
	s, repo, _, _ := newSvc(t)
	ctx := context.Background()
	register(t, s, repo, "person@example.com")
	var last error
	for i := range 5 { // IPMax = 5; each email only fails once
		_, _, last = s.Login(ctx, "user"+string(rune('a'+i))+"@example.com", "wrong-password", "198.51.100.7", "", "")
	}
	if !errors.Is(last, ErrInvalidCredentials) {
		t.Fatalf("5th failure: %v", last)
	}
	_, _, err := s.Login(ctx, "fresh@example.com", "wrong-password", "198.51.100.7", "", "")
	if retryAfter(err) == 0 {
		t.Fatalf("new email from a blocked IP: %v", err)
	}
	// Another IP is unaffected.
	if _, _, err := s.Login(ctx, "person@example.com", pw, "198.51.100.8", "", ""); err != nil {
		t.Fatalf("other IP: %v", err)
	}
}

func TestSuccessClearsOnlyEmailBucket(t *testing.T) {
	s, repo, clk, _ := newSvc(t)
	ctx := context.Background()
	register(t, s, repo, "person@example.com")
	ip := "198.51.100.9"
	fail := func() { _, _, _ = s.Login(ctx, "person@example.com", "wrong-password", ip, "", "") }
	fail()
	fail()
	if _, _, err := s.Login(ctx, "person@example.com", pw, ip, "", ""); err != nil {
		t.Fatal(err)
	}
	fail()
	fail() // 2 failures since the success: not blocked (LoginMax = 3)
	if _, _, err := s.Login(ctx, "person@example.com", pw, ip, "", ""); err != nil {
		t.Fatalf("email bucket not cleared by success: %v", err)
	}
	// The IP bucket kept counting (5 failures ⇒ blocked) and success didn't clear it.
	fail()
	if _, _, err := s.Login(ctx, "person@example.com", pw, ip, "", ""); retryAfter(err) == 0 {
		t.Fatalf("IP block cleared by success: %v", err)
	}
	// Failures older than the window are forgotten.
	clk.add(2 * time.Hour)
	if _, _, err := s.Login(ctx, "person@example.com", pw, ip, "", ""); err != nil {
		t.Fatalf("after the window: %v", err)
	}
}

func TestRegisterThrottled(t *testing.T) {
	s, _, _, _ := newSvc(t)
	ctx := context.Background()
	for range 3 {
		_ = s.Register(ctx, "person@example.com", pw, "198.51.100.3")
	}
	if err := s.Register(ctx, "person@example.com", pw, "198.51.100.3"); retryAfter(err) == 0 {
		t.Fatalf("4th registration attempt: %v", err)
	}
}

func TestLogsHoldNoSecrets(t *testing.T) {
	s, repo, _, logs := newSvc(t)
	ctx := context.Background()
	register(t, s, repo, "person@example.com")
	tok, _, _ := s.Login(ctx, "person@example.com", pw, "198.51.100.1", "", "")
	_, _, _ = s.Login(ctx, "person@example.com", "wrong-password", "198.51.100.1", "", "")
	_ = s.Register(ctx, "ghost@example.com", pw, "198.51.100.1")
	out := logs.String()
	for _, secret := range []string{pw, "wrong-password", tok, hashToken(tok), "person@example.com", "ghost@example.com",
		repo.users["person@example.com"].PasswordHash} {
		if strings.Contains(out, secret) {
			t.Fatalf("logs contain %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "p***@example.com") {
		t.Fatalf("expected redacted email in logs:\n%s", out)
	}
}

func TestNewValidatesKey(t *testing.T) {
	if _, err := New(newMem(), Config{Key: []byte("short")}); err == nil {
		t.Fatal("short key accepted")
	}
	_ = store.User{}
}
