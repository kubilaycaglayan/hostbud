package notify

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hostbud/internal/config"
	"hostbud/internal/store"
)

// device is a browser's side of a push subscription: its key pair and auth
// secret (test-held, like the e2e scenarios' keys).
type device struct {
	key  *ecdh.PrivateKey
	auth []byte
}

func newDevice(t *testing.T) device {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return device{key: k, auth: auth}
}

func (d device) p256dh() string {
	return base64.RawURLEncoding.EncodeToString(d.key.PublicKey().Bytes())
}
func (d device) secret() string { return base64.RawURLEncoding.EncodeToString(d.auth) }

// decrypt opens an RFC 8291 aes128gcm message as the browser would.
func (d device) decrypt(t *testing.T, body []byte) []byte {
	t.Helper()
	if len(body) < 21 {
		t.Fatalf("body too short: %d", len(body))
	}
	salt, rs, idlen := body[:16], binary.BigEndian.Uint32(body[16:20]), int(body[20])
	serverPub := body[21 : 21+idlen]
	ciphertext := body[21+idlen:]
	if rs < 18 || idlen != 65 {
		t.Fatalf("header rs=%d idlen=%d", rs, idlen)
	}
	pub, err := ecdh.P256().NewPublicKey(serverPub)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := d.key.ECDH(pub)
	if err != nil {
		t.Fatal(err)
	}
	info := append(append([]byte("WebPush: info\x00"), d.key.PublicKey().Bytes()...), serverPub...)
	ikm, err := hkdf.Key(sha256.New, shared, d.auth, string(info), 32)
	if err != nil {
		t.Fatal(err)
	}
	cek, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	end := bytes.LastIndexByte(plain, 2)
	if end < 0 {
		t.Fatal("no padding delimiter")
	}
	return plain[:end]
}

func testVAPID(t *testing.T) VAPID {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	v := VAPID{
		PublicKey:  base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()),
		PrivateKey: base64.RawURLEncoding.EncodeToString(k.Bytes()),
		Subject:    "mailto:owner@example.com",
	}
	if p := config.CheckVAPID(v.PublicKey, v.PrivateKey, v.Subject); !p.Available {
		t.Fatalf("test VAPID invalid: %+v", p)
	}
	return v
}

// memDeliveries is the sender's store: deliveries by key, claimed once.
type memDeliveries struct {
	mu        sync.Mutex
	pending   []store.Delivery
	claimed   map[string]bool
	finished  map[string]string
	removed   []string
	pruned    []time.Time
	claimFail error
}

func (m *memDeliveries) ClaimDelivery(context.Context) (store.Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.claimFail != nil {
		return store.Delivery{}, m.claimFail
	}
	if len(m.pending) == 0 {
		return store.Delivery{}, store.ErrNotFound
	}
	d := m.pending[0]
	m.pending = m.pending[1:]
	if m.claimed == nil {
		m.claimed = map[string]bool{}
	}
	m.claimed[d.Key+"/"+d.SubscriptionID] = true
	return d, nil
}

func (m *memDeliveries) FinishDelivery(_ context.Context, outboxID int64, sub, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.finished == nil {
		m.finished = map[string]string{}
	}
	m.finished[sub] = status
	return nil
}

func (m *memDeliveries) DeletePushSubscriptionByID(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removed = append(m.removed, id)
	return nil
}

func (m *memDeliveries) PruneNotifications(_ context.Context, cutoff time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruned = append(m.pruned, cutoff)
	return 0, nil
}

// pushFake records POSTs and answers from a script per path.
type pushFake struct {
	mu      sync.Mutex
	srv     *httptest.Server
	answers map[string][]int // path → statuses, the last repeats
	headers map[string]http.Header
	got     []recorded
}

type recorded struct {
	path   string
	header http.Header
	body   []byte
}

func newPushFake(t *testing.T) *pushFake {
	f := &pushFake{answers: map[string][]int{}, headers: map[string]http.Header{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.got = append(f.got, recorded{r.URL.Path, r.Header.Clone(), body})
		status := http.StatusCreated
		if a := f.answers[r.URL.Path]; len(a) > 0 {
			status = a[0]
			if len(a) > 1 {
				f.answers[r.URL.Path] = a[1:]
			}
		}
		for k, v := range f.headers[r.URL.Path] {
			w.Header()[k] = v
		}
		f.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *pushFake) posts(path string) []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recorded
	for _, r := range f.got {
		if r.path == path {
			out = append(out, r)
		}
	}
	return out
}

type sleeps struct {
	mu  sync.Mutex
	got []time.Duration
}

func (s *sleeps) sleep(_ context.Context, d time.Duration) error {
	s.mu.Lock()
	s.got = append(s.got, d)
	s.mu.Unlock()
	return nil
}

func newTestSender(t *testing.T, st SenderStore, fake *pushFake, logs io.Writer) (*Sender, *sleeps) {
	t.Helper()
	if logs == nil {
		logs = io.Discard
	}
	s := NewSender(st, testVAPID(t), fake.srv.URL+"/", slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	sl := &sleeps{}
	s.sleep = sl.sleep
	return s, sl
}

func delivery(fake *pushFake, dev device, path, key, kind string) store.Delivery {
	p, _ := Build(Event{Kind: kind, RunID: "01RUN", QueueID: "queue_a", ItemID: "item_a", Project: "app", Position: 1, Outcome: "failed"})
	p.Key = key
	b, _ := p.JSON()
	return store.Delivery{OutboxID: 1, SubscriptionID: "sub_" + strings.TrimPrefix(path, "/"), Endpoint: fake.srv.URL + path, P256dh: dev.p256dh(), Auth: dev.secret(), Key: key, Payload: b}
}

// V2-M3 T2: one delivery is one POST the device decrypts to the payload,
// with the VAPID JWT, TTL 24 h, the hashed key as Topic and Urgency high
// for needs attention.
func TestSenderEncryptsForTheDevice(t *testing.T) {
	fake := newPushFake(t)
	dev := newDevice(t)
	st := &memDeliveries{pending: []store.Delivery{
		delivery(fake, dev, "/a", "run:01RUN:done", KindDone),
		delivery(fake, dev, "/b", "run:01RUN:attention", KindAttention),
	}}
	s, _ := newTestSender(t, st, fake, nil)
	s.Drain(t.Context())
	for _, c := range []struct{ path, key, urgency string }{{"/a", "run:01RUN:done", "normal"}, {"/b", "run:01RUN:attention", "high"}} {
		posts := fake.posts(c.path)
		if len(posts) != 1 {
			t.Fatalf("%s: %d POSTs", c.path, len(posts))
		}
		h := posts[0].header
		if h.Get("Content-Encoding") != "aes128gcm" || h.Get("TTL") != "86400" || h.Get("Urgency") != c.urgency || h.Get("Topic") != Topic(c.key) || len(h.Get("Topic")) != 32 {
			t.Fatalf("%s headers %v", c.path, h)
		}
		var p Payload
		if err := json.Unmarshal(dev.decrypt(t, posts[0].body), &p); err != nil || p.Key != c.key || p.Project != "app" {
			t.Fatalf("%s decrypted %+v, %v", c.path, p, err)
		}
		auth := h.Get("Authorization")
		if !strings.HasPrefix(auth, "vapid t=") || !strings.Contains(auth, ", k="+s.vapid.PublicKey) {
			t.Fatalf("Authorization %q", auth)
		}
		jwt := strings.TrimPrefix(strings.Split(auth, ",")[0], "vapid t=")
		claims, _ := base64.RawURLEncoding.DecodeString(strings.Split(jwt, ".")[1])
		var c2 struct{ Aud, Sub string }
		_ = json.Unmarshal(claims, &c2)
		if c2.Sub != "mailto:owner@example.com" || c2.Aud != fake.srv.URL {
			t.Fatalf("JWT claims %s", claims)
		}
	}
	if st.finished["sub_a"] != store.DeliverySent || st.finished["sub_b"] != store.DeliverySent {
		t.Fatalf("finished %v", st.finished)
	}
}

// Redirects are never followed.
func TestSenderFollowsNoRedirects(t *testing.T) {
	fake := newPushFake(t)
	fake.answers["/r"] = []int{http.StatusFound}
	fake.headers["/r"] = http.Header{"Location": {fake.srv.URL + "/elsewhere"}}
	dev := newDevice(t)
	st := &memDeliveries{pending: []store.Delivery{delivery(fake, dev, "/r", "k", KindDone)}}
	s, _ := newTestSender(t, st, fake, nil)
	s.Drain(t.Context())
	if len(fake.posts("/elsewhere")) != 0 || st.finished["sub_r"] != store.DeliveryFailed {
		t.Fatalf("redirect followed or kept: %v", st.finished)
	}
}

// V2-M3 T3: 500 then 201 is one notification after one retry; 429 honors
// Retry-After (capped at 60 s); at most three retries; 404/410 remove the
// subscription at once; other 4xx drop it; no log line has the endpoint.
func TestSenderRetriesAndExpiry(t *testing.T) {
	fake := newPushFake(t)
	fake.answers["/flaky"] = []int{500, 201}
	fake.answers["/limited"] = []int{429, 201}
	fake.headers["/limited"] = http.Header{"Retry-After": {"120"}}
	fake.answers["/down"] = []int{503}
	fake.answers["/gone"] = []int{410}
	fake.answers["/missing"] = []int{404}
	fake.answers["/bad"] = []int{400}
	dev := newDevice(t)
	paths := []string{"/flaky", "/limited", "/down", "/gone", "/missing", "/bad"}
	st := &memDeliveries{}
	for _, p := range paths {
		st.pending = append(st.pending, delivery(fake, dev, p, "key"+p, KindDone))
	}
	var logs bytes.Buffer
	s, sl := newTestSender(t, st, fake, &logs)
	// One at a time, so the sleeps come in a known order.
	for range paths {
		d, _ := st.ClaimDelivery(t.Context())
		s.deliver(t.Context(), d)
	}
	want := map[string]int{"/flaky": 2, "/limited": 2, "/down": 4, "/gone": 1, "/missing": 1, "/bad": 1}
	for p, n := range want {
		if got := len(fake.posts(p)); got != n {
			t.Errorf("%s: %d POSTs, want %d", p, got, n)
		}
	}
	wantSleeps := []time.Duration{time.Second, 60 * time.Second, time.Second, 5 * time.Second, 25 * time.Second}
	if len(sl.got) != len(wantSleeps) {
		t.Fatalf("sleeps %v, want %v", sl.got, wantSleeps)
	}
	for i := range wantSleeps {
		if sl.got[i] != wantSleeps[i] {
			t.Fatalf("sleeps %v, want %v", sl.got, wantSleeps)
		}
	}
	wantStatus := map[string]string{"sub_flaky": "sent", "sub_limited": "sent", "sub_down": "failed", "sub_bad": "failed"}
	for k, v := range wantStatus {
		if st.finished[k] != v {
			t.Errorf("%s = %q, want %q", k, st.finished[k], v)
		}
	}
	if strings.Join(st.removed, ",") != "sub_gone,sub_missing" {
		t.Fatalf("removed %v", st.removed)
	}
	if strings.Contains(logs.String(), fake.srv.URL) || strings.Contains(logs.String(), "/flaky") {
		t.Fatalf("logs carry an endpoint:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "subscription=sub_gone") {
		t.Fatalf("logs lack the subscription id:\n%s", logs.String())
	}
}

// A network error is retried like a 5xx and logged without the URL.
func TestSenderNetworkErrorsAreRedacted(t *testing.T) {
	fake := newPushFake(t)
	dev := newDevice(t)
	d := delivery(fake, dev, "/x", "k", KindDone)
	d.Endpoint = fake.srv.URL + "/x"
	fake.srv.Close() // connection refused
	st := &memDeliveries{pending: []store.Delivery{d}}
	var logs bytes.Buffer
	s, sl := newTestSender(t, st, fake, &logs)
	s.Drain(t.Context())
	if len(sl.got) != 3 || st.finished["sub_x"] != store.DeliveryFailed {
		t.Fatalf("sleeps %v finished %v", sl.got, st.finished)
	}
	if strings.Contains(logs.String(), "/x") || strings.Contains(logs.String(), "127.0.0.1") {
		t.Fatalf("logs carry the endpoint:\n%s", logs.String())
	}
}

// Claim-before-send: Drain only ever sends what it claimed, and a store
// error stops the round without sending.
func TestSenderSendsOnlyClaimedDeliveries(t *testing.T) {
	fake := newPushFake(t)
	st := &memDeliveries{claimFail: errors.New("db down"), pending: []store.Delivery{delivery(fake, newDevice(t), "/a", "k", KindDone)}}
	s, _ := newTestSender(t, st, fake, nil)
	s.Drain(t.Context())
	if len(fake.posts("/a")) != 0 {
		t.Fatal("sent without a claim")
	}
}

func TestEndpointRules(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://fcm.googleapis.com/fcm/send/abc":       true,
		"https://web.push.apple.com:443/x":              true,
		"http://fcm.googleapis.com/x":                   false,
		"https://fcm.googleapis.com:8443/x":             false,
		"https://10.0.0.1/x":                            false,
		"https://[fd00::1]/x":                           false,
		"https://hostbud-postgres/x":                    false,
		"https://a:b@push.example.com/x":                false,
		"https://push.example.com./x":                   false,
		"https://" + strings.Repeat("a", 2050) + ".com": false,
	} {
		if got := CheckEndpoint(raw, "") == nil; got != ok {
			t.Errorf("%.60s: ok=%v, want %v", raw, got, ok)
		}
	}
	if CheckEndpoint("http://hostbud-e2e-pushfake:8080/push/a", "http://hostbud-e2e-pushfake:8080/") != nil {
		t.Error("the test prefix is refused")
	}
	for _, addr := range []string{"127.0.0.1:443", "10.1.2.3:443", "172.18.0.2:443", "192.168.1.1:443", "100.100.1.1:443", "[::1]:443", "[fe80::1]:443", "169.254.1.1:443"} {
		if publicAddress(addr) == nil {
			t.Errorf("%s allowed", addr)
		}
	}
	if err := publicAddress("142.250.1.1:443"); err != nil {
		t.Errorf("public address refused: %v", err)
	}
}
