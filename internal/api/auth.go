package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hostbud/internal/auth"
	"hostbud/internal/store"
)

// SessionCookie holds the opaque session token (HttpOnly; only its hash is
// stored server-side).
const SessionCookie = "hostbud_session"

// Authenticator is the account service (auth.Service).
type Authenticator interface {
	Register(ctx context.Context, email, password, ip string) error
	Login(ctx context.Context, email, password, ip, userAgent, oldToken string) (string, time.Time, error)
	Logout(ctx context.Context, token string) error
	Authenticate(ctx context.Context, token string) (store.User, error)
}

// publicRoutes need no session (docs/ARCHITECTURE.md §9). The SPA's static
// files are public too: they hold no data and render the sign-in screen.
var publicRoutes = map[string]bool{
	"GET /api/health":         true,
	"POST /api/auth/register": true,
	"POST /api/auth/login":    true,
}

type userKey struct{}

// AuthenticatedUserID returns the authenticated account ID attached by
// requireAuth. It is used by the terminal handler for per-account limits.
func AuthenticatedUserID(r *http.Request) string {
	u, _ := r.Context().Value(userKey{}).(store.User)
	return u.ID
}

// requireAuth refuses /api/* and /ws/* requests without a valid session.
// It fails closed: with no Authenticator configured, nothing gets through.
func requireAuth(a Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protected := strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/")
		if !protected || publicRoutes[r.Method+" "+r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		if a == nil {
			writeError(w, http.StatusUnauthorized, auth.ErrUnauthenticated.Error(), "")
			return
		}
		var token string
		if c, err := r.Cookie(SessionCookie); err == nil {
			token = c.Value
		}
		u, err := a.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, auth.ErrUnauthenticated.Error(), "Your session ended; sign in again.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, u)))
	})
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *server) clientIP(r *http.Request) string { return auth.ClientIP(r, s.cfg.TrustedProxies) }

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if !decode(w, r, &req) {
		return
	}
	err := s.cfg.Auth.Register(r.Context(), req.Email, req.Password, s.clientIP(r))
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, map[string]string{})
	case errors.Is(err, auth.ErrInvalidEmail), errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, err.Error(), "")
	case errors.Is(err, auth.ErrRegistrationFailed):
		writeError(w, http.StatusBadRequest, err.Error(),
			"Registration needs an address the owner has approved. If you already have an account, sign in.")
	default:
		s.authError(w, err)
	}
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if !decode(w, r, &req) {
		return
	}
	var old string
	if c, err := r.Cookie(SessionCookie); err == nil {
		old = c.Value
	}
	token, expires, err := s.cfg.Auth.Login(r.Context(), req.Email, req.Password, s.clientIP(r), r.UserAgent(), old)
	switch {
	case err == nil:
		s.setSessionCookie(w, r, token, expires)
		writeJSON(w, http.StatusOK, map[string]string{})
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, err.Error(), "")
	default:
		s.authError(w, err)
	}
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		if err := s.cfg.Auth.Logout(r.Context(), c.Value); err != nil {
			s.authError(w, err)
			return
		}
	}
	s.setSessionCookie(w, r, "", time.Time{})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	u, _ := r.Context().Value(userKey{}).(store.User)
	writeJSON(w, http.StatusOK, map[string]string{"email": u.Email})
}

// setSessionCookie sets (or, with an empty token, clears) the session cookie.
func (s *server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	// Secure follows the connection: HTTPS on the domain, plain HTTP only on
	// the loopback port-forward path (localhost is a secure context).
	c := &http.Cookie{ //nolint:gosec // G124: Secure is set whenever the client connection is HTTPS
		Name: SessionCookie, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: auth.IsHTTPS(r, s.cfg.TrustedProxies),
	}
	if token == "" {
		c.MaxAge = -1
	} else {
		c.Expires = expires
		c.MaxAge = int(time.Until(expires).Seconds())
	}
	http.SetCookie(w, c)
}

func (s *server) authError(w http.ResponseWriter, err error) {
	var rl *auth.RateLimitedError
	if errors.As(err, &rl) {
		w.Header().Set("Retry-After", strconv.Itoa(auth.RetryAfterSeconds(rl.RetryAfter)))
		writeError(w, http.StatusTooManyRequests, "too many attempts", "Wait a moment before trying again.")
		return
	}
	s.cfg.Log.Error("auth request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error", "")
}
