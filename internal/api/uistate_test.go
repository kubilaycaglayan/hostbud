package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"hostbud/internal/store"
)

// fakeUIState stores values by "<user>/<key>".
type fakeUIState struct{ m map[string]json.RawMessage }

func (f *fakeUIState) UIStateForUser(_ context.Context, user, key string) (json.RawMessage, error) {
	v, ok := f.m[user+"/"+key]
	if !ok {
		return nil, store.ErrNotFound
	}
	return v, nil
}

func (f *fakeUIState) PutUIStateForUser(_ context.Context, user, key string, v json.RawMessage) error {
	if f.m == nil {
		f.m = map[string]json.RawMessage{}
	}
	f.m[user+"/"+key] = v
	return nil
}

func TestUIStateRoundTripPerAccount(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(t, http.MethodGet, "/api/ui-state/layout", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("before the first PUT: %d", rec.Code)
	}
	if rec := e.do(t, http.MethodPut, "/api/ui-state/layout", `{"version":1,"tabs":[]}`, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	rec := e.do(t, http.MethodGet, "/api/ui-state/layout", "", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"version":1,"tabs":[]}` ||
		rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("GET: %d %q %s", rec.Code, rec.Body, rec.Header())
	}
	if _, ok := e.ui.m["u1/layout"]; !ok {
		t.Fatalf("stored under %v, want the signed-in account u1", e.ui.m)
	}
	if rec := e.do(t, http.MethodPut, "/api/ui-state/tree", `{"version":1,"projects":[],"sessions":{}}`, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("PUT tree = %d %s", rec.Code, rec.Body)
	}
	// Another account doesn't see it.
	other := map[string]string{"Cookie": SessionCookie + "=" + otherToken}
	if rec := e.do(t, http.MethodGet, "/api/ui-state/layout", "", other); rec.Code != http.StatusNotFound {
		t.Fatalf("other account: %d %s", rec.Code, rec.Body)
	}
}

func TestUIStateRefusals(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name, method, path, body string
		hdr                      map[string]string
		want                     int
	}{
		{"unknown key GET", http.MethodGet, "/api/ui-state/theme", "", nil, http.StatusNotFound},
		{"unknown key PUT", http.MethodPut, "/api/ui-state/other", `{}`, nil, http.StatusNotFound},
		{"invalid JSON", http.MethodPut, "/api/ui-state/layout", `{"tabs":`, nil, http.StatusBadRequest},
		{"too big", http.MethodPut, "/api/ui-state/layout", `"` + strings.Repeat("x", 64<<10) + `"`, nil, http.StatusRequestEntityTooLarge},
		{"signed out GET", http.MethodGet, "/api/ui-state/layout", "", map[string]string{"Cookie": SessionCookie + "=bad"}, http.StatusUnauthorized},
		{"signed out PUT", http.MethodPut, "/api/ui-state/layout", `{}`, map[string]string{"Cookie": SessionCookie + "=bad"}, http.StatusUnauthorized},
		{"foreign Origin", http.MethodPut, "/api/ui-state/layout", `{}`, map[string]string{"Origin": "http://evil.example.com"}, http.StatusForbidden},
	}
	for _, c := range cases {
		if rec := e.do(t, c.method, c.path, c.body, c.hdr); rec.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, rec.Code, rec.Body, c.want)
		}
	}
	if len(e.ui.m) != 0 {
		t.Fatalf("a refused request stored something: %v", e.ui.m)
	}
	// Exactly 64 KiB is fine.
	body := `"` + strings.Repeat("x", 64<<10-2) + `"`
	if rec := e.do(t, http.MethodPut, "/api/ui-state/layout", body, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("64 KiB: %d", rec.Code)
	}
}
