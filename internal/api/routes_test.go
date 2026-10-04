package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"hostbud/internal/config"
	"hostbud/internal/events"
)

type routeInfo struct {
	Method        string `json:"method"`
	Path          string `json:"path"`
	StateChanging bool   `json:"stateChanging"`
	AuthRequired  bool   `json:"authRequired"`
	WebSocket     bool   `json:"websocket"`
	JSONBody      bool   `json:"jsonBody"`
	TokenAuth     bool   `json:"token_auth"`
}

type routeOnlyTerminal struct{}

func (routeOnlyTerminal) ServeHTTP(http.ResponseWriter, *http.Request) {}
func (routeOnlyTerminal) AtCapacity(string) bool                       { return false }
func (routeOnlyTerminal) Limit(string) int                             { return 128 }

func TestRouteInventoryMatchesRouter(t *testing.T) {
	data, err := os.ReadFile("testdata/routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var routes []routeInfo
	if err := json.Unmarshal(data, &routes); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Dist: fstest.MapFS{}, Bus: events.NewBus(), Auth: &fakeAuth{}, Sessions: &fakeService{},
		Projects: &fakeProjects{}, FileSystem: &fakeFileBrowser{}, Terminal: routeOnlyTerminal{}, UIState: &fakeUIState{}, Hooks: &fakeHooks{}, Queues: &fakeQueues{},
		Notifications: newNotifier(config.Push{})}
	s := &server{cfg: cfg}
	mountRoutes(s, http.NewServeMux())
	want := make([]string, 0, len(routes))
	for _, route := range routes {
		want = append(want, route.Method+" "+route.Path)
	}
	got := append([]string(nil), s.routePatterns...)
	sort.Strings(want)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("router and route inventory differ\nrouter: %s\nfile:   %s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, route := range routes {
		if route.WebSocket != strings.HasPrefix(route.Path, "/ws/") {
			t.Errorf("%s websocket flag mismatch", route.Method+" "+route.Path)
		}
		protected := strings.HasPrefix(route.Path, "/api/") || strings.HasPrefix(route.Path, "/ws/")
		key := route.Method + " " + route.Path
		wantAuth := protected && !publicRoutes[key] && !tokenAuthRoutes[key]
		if route.TokenAuth != tokenAuthRoutes[key] {
			t.Errorf("%s token_auth flag mismatch", key)
		}
		// The exemption matcher accepts exactly the token-auth routes.
		sample := strings.NewReplacer("{machine}", "host", "{name}", "x", "{id}", "x", "{key}", "layout", "{run}", "01ARZ3NDEKTSV4RRFFQ69G5FAV", "{event}", "turn_end").Replace(route.Path)
		if got := tokenAuthRoute(httptest.NewRequestWithContext(t.Context(), route.Method, sample, nil)); got != route.TokenAuth {
			t.Errorf("%s: tokenAuthRoute = %v, want %v", key, got, route.TokenAuth)
		}
		if route.AuthRequired != wantAuth {
			t.Errorf("%s auth flag mismatch", route.Method+" "+route.Path)
		}
		wantChanging := isStateChanging(route.Method) && route.Path != "/api/machines/{machine}/sessions/{name}/windows"
		if route.StateChanging != wantChanging {
			t.Errorf("%s state-changing flag mismatch", route.Method+" "+route.Path)
		}
	}
	jsonBodies := map[string]bool{
		"POST /api/auth/register": true, "POST /api/auth/login": true,
		"POST /api/machines/{machine}/sessions/{name}/copy-mode": true,
		"POST /api/machines/{machine}/sessions/{name}/select":    true,
		"POST /api/machines/{machine}/sessions":                  true,
		"PATCH /api/machines/{machine}/sessions/{name}":          true,
		"POST /api/machines/{machine}/sessions/kill":             true,
		"PUT /api/ui-state/{key}":                                true,
		"POST /api/machines/{machine}/fs/mkdir":                  true,
		"POST /api/projects":                                     true, "PATCH /api/projects/{id}": true,
		"POST /api/projects/{id}/sessions": true,
		"POST /api/queues":                 true, "PATCH /api/queues/{id}": true, "POST /api/queues/{id}/items": true,
		"PUT /api/queues/{id}/order": true, "PUT /api/queues/{id}/loop": true, "PUT /api/queues/{id}/default-prompt": true, "PUT /api/queues/{id}/link": true, "PATCH /api/queue-items/{id}": true,
		"POST /api/machines/scan": true, "POST /api/machines": true, "PATCH /api/machines/{machine}": true,
		"PUT /api/machines/{machine}/capacity": true, "PUT /api/machines/{machine}/parallel-queues": true,
		"PUT /api/notifications/settings": true, "POST /api/notifications/subscriptions": true, "DELETE /api/notifications/subscriptions": true, "POST /api/notifications/test": true,
	}
	for _, route := range routes {
		key := route.Method + " " + route.Path
		if route.JSONBody != jsonBodies[key] {
			t.Errorf("%s JSON body flag mismatch", key)
		}
	}
}

func TestEveryJSONRouteGetsBodyAndContentTypeLimits(t *testing.T) {
	data, err := os.ReadFile("testdata/routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var routes []routeInfo
	if err := json.Unmarshal(data, &routes); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	for _, route := range routes {
		if !route.StateChanging || !route.JSONBody {
			continue
		}
		path := strings.NewReplacer("{machine}", "host", "{name}", "route-check", "{id}", "missing", "{key}", "layout").Replace(route.Path)
		for _, tc := range []struct {
			contentType string
			size        int
			status      int
		}{
			{"application/json", 65 << 10, http.StatusRequestEntityTooLarge},
			{"text/plain", 2, http.StatusUnsupportedMediaType},
		} {
			body := strings.Repeat("x", tc.size)
			req := httptest.NewRequestWithContext(t.Context(), route.Method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", tc.contentType)
			req.Header.Set("Origin", origin)
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testToken})
			w := httptest.NewRecorder()
			e.h.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Errorf("%s (%s): status=%d body=%s", route.Method+" "+path, tc.contentType, w.Code, w.Body)
			}
		}
		if route.Path == "/api/ui-state/{key}" {
			continue
		} // raw JSON state is intentionally not field-decoded
		req := httptest.NewRequestWithContext(t.Context(), route.Method, path, strings.NewReader(`{"unknownField":true}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testToken})
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s unknown field: status=%d body=%s", route.Method+" "+path, w.Code, w.Body)
		}
	}
}

func TestOriginPolicyCoversEveryChangingRouteAndWebSocket(t *testing.T) {
	data, err := os.ReadFile("testdata/routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var routes []routeInfo
	if err := json.Unmarshal(data, &routes); err != nil {
		t.Fatal(err)
	}
	allowed := []string{"http://localhost:9055", "https://hostbud.example.com"}
	for _, route := range routes {
		if !route.StateChanging && !route.WebSocket {
			continue
		}
		path := strings.NewReplacer("{machine}", "host", "{name}", "origin-check", "{id}", "missing", "{key}", "layout", "{run}", "01ARZ3NDEKTSV4RRFFQ69G5FAV", "{event}", "turn_end").Replace(route.Path)
		if route.TokenAuth {
			// Token-authenticated: any Origin, or none, reaches the handler.
			for _, origin := range []string{"http://evil.example.com", ""} {
				called := false
				req := httptest.NewRequestWithContext(t.Context(), route.Method, path, nil)
				req.Header.Set("Origin", origin)
				checkOrigin(allowed, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(httptest.NewRecorder(), req)
				if !called {
					t.Errorf("%s origin=%q: token-auth route blocked by the Origin check", route.Method+" "+route.Path, origin)
				}
			}
			continue
		}
		for _, origin := range []string{"http://evil.example.com", ""} {
			t.Run(route.Method+" "+path+" rejected origin="+origin, func(t *testing.T) {
				called := false
				next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
				req := httptest.NewRequestWithContext(t.Context(), route.Method, path, nil)
				req.Header.Set("Origin", origin)
				if route.WebSocket {
					req.Header.Set("Connection", "Upgrade")
					req.Header.Set("Upgrade", "websocket")
				}
				rec := httptest.NewRecorder()
				checkOrigin(allowed, next).ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden || called {
					t.Fatalf("status=%d downstream_called=%t; want 403 and no handler", rec.Code, called)
				}
			})
		}
		for _, origin := range allowed {
			t.Run(route.Method+" "+path+" allowed origin="+origin, func(t *testing.T) {
				called := false
				next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
				req := httptest.NewRequestWithContext(t.Context(), route.Method, path, nil)
				req.Header.Set("Origin", origin)
				if route.WebSocket {
					req.Header.Set("Connection", "Upgrade")
					req.Header.Set("Upgrade", "websocket")
				}
				rec := httptest.NewRecorder()
				checkOrigin(allowed, next).ServeHTTP(rec, req)
				if rec.Code == http.StatusForbidden || !called {
					t.Fatalf("status=%d downstream_called=%t; allowed Origin did not reach handler", rec.Code, called)
				}
			})
		}
	}
}
