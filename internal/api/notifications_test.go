package api

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"hostbud/internal/config"
	"hostbud/internal/notify"
	"hostbud/internal/store"
)

// fakeNotifyStore keeps notification prefs and subscriptions per account
// in memory.
type fakeNotifyStore struct {
	prefs map[string]store.NotificationPrefs
	subs  map[string]string // endpoint → account
}

func (f *fakeNotifyStore) SavePushSubscription(_ context.Context, user, endpoint, _, _ string) (store.PushSubscription, error) {
	if f.subs == nil {
		f.subs = map[string]string{}
	}
	f.subs[endpoint] = user
	return store.PushSubscription{ID: "push_1", UserID: user, Endpoint: endpoint}, nil
}

func (f *fakeNotifyStore) DeletePushSubscription(_ context.Context, user, endpoint string) (bool, error) {
	if f.subs[endpoint] != user {
		return false, nil
	}
	delete(f.subs, endpoint)
	return true, nil
}

func (f *fakeNotifyStore) EnqueueTestNotification(_ context.Context, user, endpoint string, _ store.Notice) error {
	if f.subs[endpoint] != user {
		return store.ErrNotFound
	}
	return nil
}

func (f *fakeNotifyStore) NotificationPrefs(_ context.Context, user string) (store.NotificationPrefs, error) {
	if p, ok := f.prefs[user]; ok {
		return p, nil
	}
	return store.DefaultNotificationPrefs, nil
}

func (f *fakeNotifyStore) PutNotificationPrefs(_ context.Context, user string, p store.NotificationPrefs) error {
	if f.prefs == nil {
		f.prefs = map[string]store.NotificationPrefs{}
	}
	f.prefs[user] = p
	return nil
}

func (f *fakeNotifyStore) NotificationsEnabled(context.Context) (bool, error) {
	for _, p := range f.prefs {
		if p.Enabled {
			return true, nil
		}
	}
	return false, nil
}

func newNotifier(push config.Push) *notify.Service {
	return newNotifierWith(&fakeNotifyStore{}, push)
}

func newNotifierWith(st *fakeNotifyStore, push config.Push) *notify.Service {
	return notify.New(st, push, "BPublicKey", nil)
}

// testKeys are a valid p256dh (a P-256 point) and 16-byte auth secret.
func testKeys(t *testing.T) (string, string) {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(make([]byte, 16))
}

// V2-M3 T2: subscribing needs push available, an https/443/DNS endpoint and
// valid keys; unsubscribing touches only the caller's own subscription.
func TestPushSubscriptionRoutes(t *testing.T) {
	e := newEnv(t)
	p256dh, auth := testKeys(t)
	body := func(endpoint string) string {
		return `{"endpoint":"` + endpoint + `","expirationTime":null,"keys":{"p256dh":"` + p256dh + `","auth":"` + auth + `"}}`
	}
	if rec := e.do(t, http.MethodPost, "/api/notifications/subscriptions", body("https://push.example.com/x"), nil); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "make vapid-keys") {
		t.Fatalf("push off: %d %s", rec.Code, rec.Body)
	}
	st := &fakeNotifyStore{}
	e.notifier = newNotifierWith(st, config.Push{Available: true})
	e.rebuild()
	for _, bad := range []string{"http://push.example.com/x", "https://push.example.com:8443/x", "https://127.0.0.1/x", "https://[::1]/x", "https://localhost/x", "https://user:pw@push.example.com/x", "not a url"} {
		if rec := e.do(t, http.MethodPost, "/api/notifications/subscriptions", body(bad), nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}
	badKeys := `{"endpoint":"https://push.example.com/x","keys":{"p256dh":"AAAA","auth":"` + auth + `"}}`
	if rec := e.do(t, http.MethodPost, "/api/notifications/subscriptions", badKeys, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad keys: %d", rec.Code)
	}
	if rec := e.do(t, http.MethodPost, "/api/notifications/subscriptions", body("https://push.example.com/ok"), nil); rec.Code != http.StatusNoContent || st.subs["https://push.example.com/ok"] != "u1" {
		t.Fatalf("subscribe: %d %s %v", rec.Code, rec.Body, st.subs)
	}
	other := map[string]string{"Cookie": SessionCookie + "=" + otherToken}
	if rec := e.do(t, http.MethodDelete, "/api/notifications/subscriptions", `{"endpoint":"https://push.example.com/ok"}`, other); rec.Code != http.StatusNoContent || st.subs["https://push.example.com/ok"] != "u1" {
		t.Fatalf("another account removed it: %d %v", rec.Code, st.subs)
	}
	if rec := e.do(t, http.MethodDelete, "/api/notifications/subscriptions", `{"endpoint":"https://push.example.com/ok"}`, nil); rec.Code != http.StatusNoContent || len(st.subs) != 0 {
		t.Fatalf("unsubscribe: %d %v", rec.Code, st.subs)
	}
	if rec := e.do(t, http.MethodPost, "/api/notifications/subscriptions", body("https://push.example.com/ok"), map[string]string{"Origin": "https://evil.example"}); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign Origin: %d", rec.Code)
	}
}

// V2-M3 T0: a fresh account reads off; a PUT changes only the caller's
// account; push unavailable carries the reason and no public key.
func TestNotificationSettingsPerAccount(t *testing.T) {
	e := newEnv(t)
	rec := e.do(t, http.MethodGet, "/api/notifications/settings", "", nil)
	got := decodeBody[notify.Settings](t, rec)
	if rec.Code != http.StatusOK || got.Enabled || !got.OnDone || !got.OnAttention || !got.OnFinished {
		t.Fatalf("fresh account: %d %+v", rec.Code, got)
	}
	if got.Push.Available || !strings.Contains(got.Push.Reason, "make vapid-keys") || got.VAPIDPublicKey != "" {
		t.Fatalf("push without keys: %+v", got)
	}
	rec = e.do(t, http.MethodPut, "/api/notifications/settings", `{"enabled":true,"onFinished":false}`, nil)
	got = decodeBody[notify.Settings](t, rec)
	if rec.Code != http.StatusOK || !got.Enabled || got.OnFinished || !got.OnDone {
		t.Fatalf("PUT: %d %+v", rec.Code, got)
	}
	other := e.do(t, http.MethodGet, "/api/notifications/settings", "", map[string]string{"Cookie": SessionCookie + "=" + otherToken})
	if o := decodeBody[notify.Settings](t, other); o.Enabled || !o.OnFinished {
		t.Fatalf("another account changed: %+v", o)
	}
	if rec := e.do(t, http.MethodPut, "/api/notifications/settings", `{"enabled":"yes"}`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body: %d", rec.Code)
	}
	if rec := e.do(t, http.MethodPut, "/api/notifications/settings", `{"enabled":true}`, map[string]string{"Origin": "https://evil.example"}); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign Origin: %d", rec.Code)
	}
	if rec := e.do(t, http.MethodGet, "/api/notifications/settings", "", map[string]string{"Cookie": "x=y"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("signed out: %d", rec.Code)
	}
}

func TestNotificationSettingsPushAvailable(t *testing.T) {
	e := newEnv(t)
	e.notifier = newNotifier(config.Push{Available: true})
	e.rebuild()
	got := decodeBody[notify.Settings](t, e.do(t, http.MethodGet, "/api/notifications/settings", "", nil))
	if !got.Push.Available || got.Push.Reason != "" || got.VAPIDPublicKey != "BPublicKey" {
		t.Fatalf("push available: %+v", got)
	}
}

// V2-M3 T4: the test route reaches only the caller's own device, and is
// rate-limited per account.
func TestTestNotificationRoute(t *testing.T) {
	e := newEnv(t)
	st := &fakeNotifyStore{subs: map[string]string{"https://push.example.com/mine": "u1", "https://push.example.com/theirs": "u2"}}
	e.notifier = newNotifierWith(st, config.Push{Available: true})
	e.rebuild()
	if rec := e.do(t, http.MethodPost, "/api/notifications/test", `{"endpoint":"https://push.example.com/theirs"}`, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("another account's device: %d %s", rec.Code, rec.Body)
	}
	for i := range 2 {
		if rec := e.do(t, http.MethodPost, "/api/notifications/test", `{"endpoint":"https://push.example.com/mine"}`, nil); rec.Code != http.StatusAccepted {
			t.Fatalf("test %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	rec := e.do(t, http.MethodPost, "/api/notifications/test", `{"endpoint":"https://push.example.com/mine"}`, nil)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("over the limit: %d %v", rec.Code, rec.Header())
	}
	// Another account has its own budget.
	other := map[string]string{"Cookie": SessionCookie + "=" + otherToken}
	if rec := e.do(t, http.MethodPost, "/api/notifications/test", `{"endpoint":"https://push.example.com/theirs"}`, other); rec.Code != http.StatusAccepted {
		t.Fatalf("other account: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(t, http.MethodPost, "/api/notifications/test", `{"endpoint":"https://push.example.com/mine"}`, map[string]string{"Origin": "https://evil.example"}); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign Origin: %d", rec.Code)
	}
	if rec := e.do(t, http.MethodPost, "/api/notifications/test", `{}`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("no endpoint: %d", rec.Code)
	}
}
