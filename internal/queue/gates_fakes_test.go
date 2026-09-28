package queue

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"

	"hostbud/internal/session"
	"hostbud/internal/store"
)

// V2-M4 gate methods of the in-memory store, like the store's guarded
// transactions.

func (m *memStore) StartVerify(_ context.Context, itemID, runID string, from []string) (int, store.QueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.q().items[itemID]
	if !ok {
		return 0, it, store.ErrNotFound
	}
	if !slices.Contains(from, it.Status) {
		return 0, store.QueueItem{}, store.ErrConflict
	}
	it.Status = store.ItemVerifying
	m.q().items[itemID] = it
	attempt := 1
	for _, e := range m.events {
		if e.RunID == runID && e.Source == store.SourceVerify && e.Kind == store.KindVerifyStarted {
			attempt++
		}
	}
	payload, _ := json.Marshal(map[string]int{"attempt": attempt})
	m.events = append(m.events, store.RunEvent{ID: int64(len(m.events) + 1), RunID: runID, Source: store.SourceVerify, Kind: store.KindVerifyStarted, Payload: payload})
	m.maxActive = max(m.maxActive, m.activeLocked())
	return attempt, it, nil
}

func (m *memStore) FinishVerify(_ context.Context, itemID, runID string, result []byte, to string, n *store.Notice) (store.QueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.q().items[itemID]
	if !ok {
		return it, store.ErrNotFound
	}
	if it.Status != store.ItemVerifying {
		return store.QueueItem{}, store.ErrConflict
	}
	if len(result) > store.MaxRunEventPayload || !json.Valid(result) {
		return store.QueueItem{}, store.ErrPayloadTooLarge
	}
	it.Status = to
	m.q().items[itemID] = it
	m.events = append(m.events, store.RunEvent{ID: int64(len(m.events) + 1), RunID: runID, Source: store.SourceVerify, Kind: store.KindVerifyResult, Payload: append([]byte(nil), result...)})
	if n != nil {
		m.outbox = append(m.outbox, *n)
	}
	return it, nil
}

func (m *memStore) VerifyEvents(_ context.Context, runID string) ([]store.RunEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.RunEvent
	for _, e := range m.events {
		if e.RunID == runID && e.Source == store.SourceVerify {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *memStore) ItemsWithStatus(_ context.Context, machine, status string) ([]store.QueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.QueueItem
	for _, it := range m.q().items {
		if it.MachineID == machine && it.Status == status {
			out = append(out, it)
		}
	}
	slices.SortFunc(out, func(a, b store.QueueItem) int { return a.Position - b.Position })
	return out, nil
}

func (m *memStore) LatestRunForItem(_ context.Context, itemID string) (store.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.order) - 1; i >= 0; i-- {
		if r := m.runs[m.order[i]]; r.ItemID == itemID {
			return r, nil
		}
	}
	return store.Run{}, store.ErrNotFound
}

func (m *memStore) verifyResults(runID string) []VerifyResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []VerifyResult
	for _, e := range m.events {
		if e.RunID == runID && e.Kind == store.KindVerifyResult {
			var r VerifyResult
			_ = json.Unmarshal(e.Payload, &r)
			out = append(out, r)
		}
	}
	return out
}

// fakeVerifier returns scripted results. With hold set, each call blocks
// until release (or ctx ends: ok=false, like a shutdown).
type fakeVerifier struct {
	mu      sync.Mutex
	results []VerifyResult // popped per call; default passed
	calls   []string       // machine|dir|command
	hold    bool
	release chan struct{}
	started chan struct{}
}

func newFakeVerifier() *fakeVerifier {
	return &fakeVerifier{release: make(chan struct{}, 16), started: make(chan struct{}, 16)}
}

func (v *fakeVerifier) Timeout() time.Duration { return 10 * time.Minute }

func (v *fakeVerifier) Run(ctx context.Context, machine, dir, command string) (VerifyResult, bool) {
	v.mu.Lock()
	v.calls = append(v.calls, machine+"|"+dir+"|"+command)
	res := VerifyResult{Outcome: VerifyPassed, ExitCode: new(int)}
	if len(v.results) > 0 {
		res, v.results = v.results[0], v.results[1:]
	}
	hold := v.hold
	v.mu.Unlock()
	v.started <- struct{}{}
	if hold {
		select {
		case <-v.release:
		case <-ctx.Done():
			return VerifyResult{}, false
		}
	}
	return res, true
}

func (v *fakeVerifier) script(results ...VerifyResult) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.results = append(v.results, results...)
}

func (v *fakeVerifier) callCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.calls)
}

func (m *memStore) ResolveApproval(_ context.Context, itemID, runID, to, kind string, payload []byte, n *store.Notice) (store.QueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.q().items[itemID]
	if !ok {
		return it, store.ErrNotFound
	}
	if it.Status != store.ItemAwaitingApproval {
		return store.QueueItem{}, store.ErrConflict
	}
	it.Status = to
	m.q().items[itemID] = it
	m.events = append(m.events, store.RunEvent{ID: int64(len(m.events) + 1), RunID: runID, Source: store.SourceUser, Kind: kind, Payload: append([]byte(nil), payload...)})
	if n != nil {
		m.outbox = append(m.outbox, *n)
	}
	return it, nil
}

// created returns the session specs created so far.
func (f *fakeSessions) created() []session.Spec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.specs)
}
