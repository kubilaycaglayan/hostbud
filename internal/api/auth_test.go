package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"hostbud/internal/auth"
	"hostbud/internal/events"
	"hostbud/internal/store"
)

func authEnv(t *testing.T, a Authenticator) *env {
	t.Helper()
	proxies, _ := auth.ParsePrefixes("172.16.0.0/12")
	return &env{h: New(Config{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Dist: fstest.MapFS{"index.html": {Data: []byte("app")}},
		Origins: AllowedOrigins("", 9055), Bus: events.NewBus(), Machines: []Snapshotter{host("a")},
		Sessions: &fakeService{}, Auth: a, TrustedProxies: proxies,
		Terminal: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }),
	})}
}

var noCookie = map[string]string{"Cookie": ""}

func TestProtectedRoutesNeedSession(t *testing.T) {
	for name, a := range map[string]Authenticator{"with auth": &fakeAuth{}, "nil auth (fail closed)": nil} {
		t.Run(name, func(t *testing.T) {
			e := authEnv(t, a)
			for _, r := range []struct{ method, path, body string }{
				{"GET", "/api/machines", ""},
				{"GET", "/api/machines/host/sessions", ""},
				{"POST", "/api/machines/host/sessions", `{"name":"x"}`},
				{"PATCH", "/api/machines/host/sessions/a", `{"name":"b"}`},
				{"DELETE", "/api/machines/host/sessions/a", ""},
				{"GET", "/api/auth/me", ""},
				{"POST", "/api/auth/logout", ""},
				{"GET", "/ws/events", ""},
				{"GET", "/ws/term?machine=host&session=a", ""},
			} {
				for _, cookie := range []string{"", "forged-token"} {
					hdr := map[string]string{"Cookie": ""}
					if cookie != "" {
						hdr["Cookie"] = SessionCookie + "=" + cookie
					}
					rec := e.do(t, r.method, r.path, r.body, hdr)
					if rec.Code != http.StatusUnauthorized {
						t.Errorf("%s %s cookie=%q: %d", r.method, r.path, cookie, rec.Code)
					}
				}
			}
			// Public: health and the SPA shell (it renders the sign-in screen).
			if rec := e.do(t, "GET", "/api/health", "", noCookie); rec.Code != 200 {
				t.Errorf("health: %d", rec.Code)
			}
			if rec := e.do(t, "GET", "/", "", noCookie); rec.Code != 200 {
				t.Errorf("SPA: %d", rec.Code)
			}
		})
	}
}

type authOutage struct{ fakeAuth }

func (authOutage) Authenticate(context.Context, string) (store.User, error) {
	return store.User{}, context.DeadlineExceeded
}

func TestDatabaseOutageDuringAuthenticationIs503(t *testing.T) {
	e := authEnv(t, &authOutage{})
	rec := e.do(t, http.MethodGet, "/api/machines", "", map[string]string{"Cookie": SessionCookie + "=" + testToken})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "The database isn't answering" || body.Hint == "" {
		t.Fatalf("error body: %+v", body)
	}
}

func TestSessionReachesProtectedRoutes(t *testing.T) {
	e := authEnv(t, &fakeAuth{})
	if rec := e.do(t, "GET", "/api/machines", "", nil); rec.Code != 200 {
		t.Fatalf("machines with session: %d", rec.Code)
	}
	rec := e.do(t, "GET", "/api/auth/me", "", nil)
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"email":"Person@example.com"}` {
		t.Fatalf("me: %d %s", rec.Code, rec.Body)
	}
	hdr := map[string]string{"Upgrade": "websocket", "Connection": "Upgrade", "Origin": origin}
	if rec := e.do(t, "GET", "/ws/term?machine=host&session=a", "", hdr); rec.Code != http.StatusTeapot {
		t.Fatalf("term with session: %d", rec.Code)
	}
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookie {
			return c
		}
	}
	t.Fatalf("no session cookie in %v", rec.Header())
	return nil
}

func TestLoginSetsHardenedCookie(t *testing.T) {
	e := authEnv(t, &fakeAuth{})
	rec := e.do(t, "POST", "/api/auth/login", `{"email":"person@example.com","password":"good-password"}`, noCookie)
	if rec.Code != 200 {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	c := sessionCookie(t, rec)
	if c.Value != "new-token" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Secure ||
		c.MaxAge < 3500 || c.MaxAge > 3600 {
		t.Fatalf("cookie %+v", c)
	}
	if strings.Contains(rec.Body.String(), "new-token") {
		t.Fatal("token in the response body")
	}

	// HTTPS as reported by the trusted proxy (Caddy) ⇒ Secure.
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/auth/login",
		strings.NewReader(`{"email":"person@example.com","password":"good-password"}`))
	req.RemoteAddr = "172.20.0.3:4000"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Origin", origin)
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if c := sessionCookie(t, rec); !c.Secure {
		t.Fatal("cookie not Secure behind HTTPS")
	}
	// The same header from an untrusted peer is ignored.
	req = httptest.NewRequestWithContext(t.Context(), "POST", "/api/auth/login",
		strings.NewReader(`{"email":"person@example.com","password":"good-password"}`))
	req.RemoteAddr = "203.0.113.4:4000"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Origin", origin)
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if c := sessionCookie(t, rec); c.Secure {
		t.Fatal("untrusted X-Forwarded-Proto made the cookie Secure")
	}
}

func TestLoginErrors(t *testing.T) {
	e := authEnv(t, &fakeAuth{})
	rec := e.do(t, "POST", "/api/auth/login", `{"email":"person@example.com","password":"bad"}`, noCookie)
	body := decodeBody[errorBody](t, rec)
	if rec.Code != 401 || body.Error != "invalid email or password" || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("bad password: %d %+v", rec.Code, body)
	}
	e = authEnv(t, &fakeAuth{err: &auth.RateLimitedError{RetryAfter: 1500 * time.Millisecond}})
	rec = e.do(t, "POST", "/api/auth/login", `{"email":"person@example.com","password":"good-password"}`, noCookie)
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("rate limited: %d Retry-After=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := e.do(t, "POST", "/api/auth/login", `{"email":"x","password":"y"}`, map[string]string{"Cookie": "", "Origin": "http://evil.example.com"}); rec.Code != 403 {
		t.Fatalf("foreign origin login: %d", rec.Code)
	}
}

func TestRegisterMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		msg    string
	}{
		{nil, 201, ""},
		{auth.ErrInvalidEmail, 400, "enter a valid email address"},
		{auth.ErrWeakPassword, 400, auth.ErrWeakPassword.Error()},
		{auth.ErrRegistrationFailed, 400, auth.ErrRegistrationFailed.Error()},
		{&auth.RateLimitedError{RetryAfter: 3 * time.Second}, 429, "too many attempts"},
	}
	for _, c := range cases {
		fa := &fakeAuth{err: c.err}
		e := authEnv(t, fa)
		rec := e.do(t, "POST", "/api/auth/register", `{"email":"person@example.com","password":"long enough pw"}`, noCookie)
		if rec.Code != c.status {
			t.Errorf("%v: %d %s", c.err, rec.Code, rec.Body)
		}
		if c.msg != "" && decodeBody[errorBody](t, rec).Error != c.msg {
			t.Errorf("%v: body %s", c.err, rec.Body)
		}
		if len(fa.registered) != 1 || !strings.HasPrefix(fa.registered[0], "person@example.com ") {
			t.Errorf("service call %q", fa.registered)
		}
	}
}

func TestLogoutRevokesAndClears(t *testing.T) {
	fa := &fakeAuth{}
	e := authEnv(t, fa)
	rec := e.do(t, "POST", "/api/auth/logout", "", nil)
	if rec.Code != 204 || len(fa.loggedOut) != 1 || fa.loggedOut[0] != testToken {
		t.Fatalf("logout: %d %q", rec.Code, fa.loggedOut)
	}
	if c := sessionCookie(t, rec); c.MaxAge >= 0 || c.Value != "" {
		t.Fatalf("cookie not cleared: %+v", c)
	}
}

func TestClientIPUsesTrustedProxyHeader(t *testing.T) {
	fa := &fakeAuth{}
	e := authEnv(t, fa)
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/api/auth/register",
		strings.NewReader(`{"email":"a@example.com","password":"long enough pw"}`))
	req.RemoteAddr = "172.20.0.3:4000"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "198.51.100.23")
	req.Header.Set("Origin", origin)
	e.h.ServeHTTP(httptest.NewRecorder(), req)
	if len(fa.registered) != 1 || fa.registered[0] != "a@example.com 198.51.100.23" {
		t.Fatalf("ip %q", fa.registered)
	}
	_ = netip.Addr{}
}
