package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"hostbud/internal/config"
	"hostbud/internal/notify"
	"hostbud/internal/store"
)

// fakeNotifyStore keeps notification prefs per account in memory.
type fakeNotifyStore struct {
	prefs map[string]store.NotificationPrefs
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
	return notify.New(&fakeNotifyStore{}, push, "BPublicKey", nil)
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
