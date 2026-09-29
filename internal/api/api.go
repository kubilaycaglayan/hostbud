// Package api serves hostbud's HTTP and WebSocket endpoints
// (docs/ARCHITECTURE.md §9).
package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/fsbrowse"
	"hostbud/internal/inventory"
	"hostbud/internal/llm"
	"hostbud/internal/tmux"
)

// Snapshotter is one tracked machine (inventory.Inventory).
type Snapshotter interface {
	Snapshot() (inventory.Machine, []tmux.Session)
}

// Config wires the handler to the rest of hostbud.
type Config struct {
	Log  *slog.Logger
	Dist fs.FS // the built SPA (package web)
	// Origins are the allowed Origin header values for WebSocket upgrades and
	// state-changing requests (see AllowedOrigins).
	Origins    []string
	Bus        *events.Bus
	Machines   []Snapshotter // v1: the host only
	Sessions   SessionService
	Projects   ProjectService
	FileSystem FileBrowser  // authenticated SFTP-backed filesystem service
	Terminal   http.Handler // /ws/term (term.Handler)
	UIState    UIStateStore // /api/ui-state/{key}
	// Auth guards every /api and /ws route but health, register and login.
	// Nil fails closed (those routes answer 401).
	Auth           Authenticator
	TrustedProxies []netip.Prefix // peers whose X-Forwarded-* headers count (Caddy)
	// Heartbeat is how often /ws/events sends {"type":"heartbeat"}, so the
	// browser notices a hung connection (default 15s).
	Heartbeat             time.Duration
	ExecTimeout           time.Duration
	SFTPTimeout           time.Duration
	UploadTimeout         time.Duration
	RequestTimeout        time.Duration
	DBPing                func(context.Context) error
	ContentSecurityPolicy string
	AllowedTSUsers        string
	TSLogin               func(context.Context, string) (string, error)
	// Hooks receives v2 run hooks (POST /api/hooks/{run}/{event}).
	Hooks HookReceiver
	// Queues is the v2 queue service (/api/queues, /api/queue-items).
	Queues QueueService
	// Notifications is the V2-M3 notifier (/api/notifications/*).
	Notifications NotificationService
	Supervisor    llm.Status
}

// HookReceiver checks and records one run hook (queue.Hooks). It returns a
// *queue.HookError for a refused hook.
type HookReceiver interface {
	Receive(ctx context.Context, runID, event, authorization string, body io.Reader) error
}

var inlineScriptRE = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script\s*>`)
var scriptSourceRE = regexp.MustCompile(`(?i)\bsrc\s*=`)
var bootCapabilityRE = regexp.MustCompile(`(?i)\bfetch\s*\(|\bXMLHttpRequest\b|\bWebSocket\b`)

// BuildContentSecurityPolicy hashes the single approved inline theme boot script.
// Any other inline script must be moved into a same-origin asset before startup.
func BuildContentSecurityPolicy(dist fs.FS, origins []string) (string, error) {
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		return "", fmt.Errorf("read embedded index.html for CSP: %w", err)
	}
	allScripts := inlineScriptRE.FindAllSubmatch(index, -1)
	scripts := make([][]byte, 0, len(allScripts))
	for _, script := range allScripts {
		if !scriptSourceRE.Match(script[1]) {
			scripts = append(scripts, script[2])
		}
	}
	if len(scripts) != 1 {
		return "", fmt.Errorf("CSP requires exactly one inline script in index.html; found %d", len(scripts))
	}
	if !strings.Contains(string(scripts[0]), "hostbud.theme") || !strings.Contains(string(scripts[0]), "dataset.theme") {
		return "", errors.New("CSP inline script is not the approved hostbud theme boot script")
	}
	boot := string(scripts[0])
	if len(boot) >= 1024 || bootCapabilityRE.MatchString(boot) ||
		strings.Count(boot, "document.documentElement.") != strings.Count(boot, "document.documentElement.dataset.theme") {
		return "", errors.New("CSP inline script exceeds the theme boot script rules")
	}
	hash := sha256.Sum256(scripts[0])
	connect := []string{"'self'"}
	for _, origin := range origins {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" {
			return "", fmt.Errorf("invalid CSP origin %q", origin)
		}
		switch u.Scheme {
		case "http":
			u.Scheme = "ws"
		case "https":
			u.Scheme = "wss"
		default:
			return "", fmt.Errorf("invalid CSP origin scheme in %q", origin)
		}
		connect = append(connect, u.Scheme+"://"+u.Host)
	}
	return "default-src 'self'; script-src 'self' 'sha256-" + base64.StdEncoding.EncodeToString(hash[:]) + "'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src " + strings.Join(connect, " ") + "; worker-src 'self'; manifest-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'", nil
}

// New returns the root HTTP handler.
func New(cfg Config) http.Handler {
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = defaultHeartbeat
	}
	if cfg.ExecTimeout <= 0 {
		cfg.ExecTimeout = 10 * time.Second
	}
	if cfg.SFTPTimeout <= 0 {
		cfg.SFTPTimeout = 10 * time.Second
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 30 * time.Second
	}
	if cfg.UploadTimeout <= 0 {
		cfg.UploadTimeout = 5 * time.Minute
	}
	s := &server{cfg: cfg, machines: map[string]Snapshotter{}, eventUsers: map[string]int{}}
	for _, m := range cfg.Machines {
		info, _ := m.Snapshot()
		s.machines[info.ID] = m
		s.order = append(s.order, info.ID)
	}

	mux := http.NewServeMux()
	mountRoutes(s, mux)
	inner := tailscaleIdentity(cfg, checkOrigin(cfg.Origins, requestLimits(cfg, requireAuth(cfg.Auth, mux))))
	return securityHeaders(cfg.ContentSecurityPolicy, inner)
}

func securityHeaders(csp string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		if csp != "" {
			h.Set("Content-Security-Policy", csp)
		}
		next.ServeHTTP(w, r)
	})
}

func mountRoutes(s *server, mux *http.ServeMux) {
	cfg := s.cfg
	add := func(pattern string, h http.Handler) {
		mux.Handle(pattern, h)
		s.routePatterns = append(s.routePatterns, pattern)
	}
	addFunc := func(pattern string, h http.HandlerFunc) { add(pattern, h) }
	addFunc("GET /api/health", s.health)
	addFunc("GET /api/runtime/limits", s.runtimeLimits)
	addFunc("GET /api/supervisor", s.supervisorStatus)
	if cfg.Auth != nil {
		addFunc("POST /api/auth/register", s.register)
		addFunc("POST /api/auth/login", s.login)
		addFunc("POST /api/auth/logout", s.logout)
		addFunc("GET /api/auth/me", s.me)
	}
	if cfg.Sessions != nil {
		addFunc("GET /api/machines/{machine}/sessions/{name}/output", s.sessionOutput)
		addFunc("POST /api/machines/{machine}/sessions/{name}/copy-mode", s.copyMode)
		addFunc("GET /api/machines/{machine}/sessions/{name}/windows", s.listWindows)
		addFunc("POST /api/machines/{machine}/sessions/{name}/windows", s.listWindows)
		addFunc("POST /api/machines/{machine}/sessions/{name}/select", s.selectWindow)
		addFunc("GET /api/machines/{machine}/sessions/{name}/select", s.selectWindow)
	}
	addFunc("GET /api/machines", s.listMachines)
	addFunc("GET /api/machines/{machine}/sessions", s.listSessions)
	addFunc("POST /api/machines/{machine}/sessions", s.createSession)
	addFunc("PATCH /api/machines/{machine}/sessions/{name}", s.renameSession)
	addFunc("DELETE /api/machines/{machine}/sessions/{name}", s.killSession)
	addFunc("POST /api/machines/{machine}/sessions/kill", s.killSessions)
	addFunc("GET /ws/events", s.eventsSocket)
	if cfg.Terminal != nil {
		add("GET /ws/term", cfg.Terminal)
		addFunc("GET /api/runtime/terminal-slots", s.terminalSlots)
	}
	if cfg.UIState != nil {
		addFunc("GET /api/ui-state/{key}", s.getUIState)
		addFunc("PUT /api/ui-state/{key}", s.putUIState)
	}
	if cfg.FileSystem != nil {
		addFunc("GET /api/machines/{machine}/fs/home", s.fsHome)
		addFunc("GET /api/machines/{machine}/fs", s.fsList)
		addFunc("GET /api/machines/{machine}/fs/stat", s.fsStat)
		addFunc("POST /api/machines/{machine}/fs/mkdir", s.fsMkdir)
		addFunc("PUT /api/machines/{machine}/fs/upload", s.fsUpload)
	}
	if cfg.Projects != nil {
		addFunc("GET /api/projects", s.listProjects)
		addFunc("POST /api/projects", s.createProject)
		addFunc("GET /api/projects/{id}", s.getProject)
		addFunc("GET /api/projects/{id}/recent-commands", s.listRecentCommands)
		addFunc("PATCH /api/projects/{id}", s.renameProject)
		addFunc("DELETE /api/projects/{id}", s.deleteProject)
		addFunc("POST /api/projects/{id}/sessions", s.createProjectSession)
	}
	if cfg.Hooks != nil {
		addFunc(hookRoute, s.runHook)
	}
	if cfg.Queues != nil {
		mountQueueRoutes(s, addFunc)
	}
	if cfg.Notifications != nil {
		mountNotificationRoutes(s, addFunc)
	}
	add("GET /", spaHandler(cfg.Dist))
}

func requestLimits(cfg Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		isUpload := r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/fs/upload")
		if isUpload {
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/octet-stream" {
				writeError(w, http.StatusUnsupportedMediaType, "content type must be application/octet-stream", "Send the original photo bytes directly.")
				return
			}
			if r.ContentLength < 0 || r.ContentLength > fsbrowse.MaxUploadBytes {
				writeError(w, http.StatusRequestEntityTooLarge, "photo exceeds the 100 MiB upload limit", "Choose a smaller original photo.")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, fsbrowse.MaxUploadBytes)
		} else if isStateChanging(r.Method) && hasRequestBody(r) && !tokenAuthRoute(r) {
			// (Run hooks cap their body themselves, after the token check,
			// so their responses keep the order in v2 §8.)
			// The media type first: a non-JSON body is refused whatever its size.
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json", "Send a JSON request body.")
				return
			}
			if r.ContentLength > 64<<10 {
				writeError(w, http.StatusRequestEntityTooLarge, "request body is limited to 64 KiB", "Send a smaller JSON request.")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			deadline := cfg.RequestTimeout
			if isUpload {
				deadline = cfg.UploadTimeout
			}
			ctx, cancel := context.WithTimeout(r.Context(), deadline)
			defer cancel()
			controller := http.NewResponseController(w)
			_ = controller.SetWriteDeadline(time.Now().Add(deadline))
			if isUpload {
				_ = controller.SetReadDeadline(time.Now().Add(deadline))
			}
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

func isStateChanging(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func hasRequestBody(r *http.Request) bool {
	return r.ContentLength > 0 || len(r.TransferEncoding) > 0 || (r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0)
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	if s.cfg.DBPing == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := s.cfg.DBPing(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "db": "unreachable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type server struct {
	cfg           Config
	machines      map[string]Snapshotter
	order         []string
	eventMu       sync.Mutex
	eventUsers    map[string]int
	routePatterns []string
}

type terminalCapacity interface {
	AtCapacity(string) bool
	Limit(string) int
}

func (s *server) terminalSlots(w http.ResponseWriter, r *http.Request) {
	account := AuthenticatedUserID(r)
	capacity, ok := s.cfg.Terminal.(terminalCapacity)
	if account == "" || !ok || !capacity.AtCapacity(account) {
		writeJSON(w, http.StatusOK, map[string]bool{"available": true})
		return
	}
	writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Too many open terminals (%d)", capacity.Limit(account)), "Close some tabs or panes; each open terminal keeps an ssh process on the host.")
}

func (s *server) runtimeLimits(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]int64{
		"execTimeoutMs":   s.cfg.ExecTimeout.Milliseconds(),
		"sftpTimeoutMs":   s.cfg.SFTPTimeout.Milliseconds(),
		"uploadTimeoutMs": s.cfg.UploadTimeout.Milliseconds(),
	})
}

func (s *server) supervisorStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg.Supervisor)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorBody is the JSON error shape: {error, hint?}.
type errorBody struct {
	Error string `json:"error"`
	Hint  string `json:"hint,omitempty"`
}

func writeError(w http.ResponseWriter, status int, msg, hint string) {
	writeJSON(w, status, errorBody{Error: msg, Hint: hint})
}

func writeDatabaseUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "The database isn't answering", "Check `docker compose ps hostbud-postgres`; hostbud recovers when it's back.")
}
