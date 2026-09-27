package queue

import (
	"context"
	"slices"
	"sync"
	"time"

	"hostbud/internal/session"
	"hostbud/internal/store"
)

// memStore is an in-memory store for the queue package's unit tests.
type memStore struct {
	mu     sync.Mutex
	runs   map[string]store.Run
	order  []string
	events []store.RunEvent
	seq    int
}

func newMemStore() *memStore { return &memStore{runs: map[string]store.Run{}} }

func (m *memStore) CreateRun(_ context.Context, itemID string, hash []byte, startedAt time.Time) (store.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	id, _ := store.NewULID(startedAt)
	r := store.Run{ID: id, ItemID: itemID, MachineID: store.HostMachineID, TokenHash: hash, Status: store.RunStarting, StartedAt: startedAt}
	m.runs[id] = r
	m.order = append(m.order, id)
	return r, nil
}

func (m *memStore) Run(_ context.Context, id string) (store.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return r, store.ErrNotFound
	}
	return r, nil
}

func (m *memStore) UpdateRun(_ context.Context, id string, u store.RunUpdate) (store.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return r, store.ErrNotFound
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&r.SessionName, u.SessionName)
	set(&r.AgentSessionID, u.AgentSessionID)
	set(&r.TranscriptPath, u.TranscriptPath)
	set(&r.ClientVersion, u.ClientVersion)
	set(&r.Detail, u.Detail)
	if u.TranscriptOffset != nil {
		r.TranscriptOffset = *u.TranscriptOffset
	}
	if u.LastSignalAt != nil {
		t := *u.LastSignalAt
		r.LastSignalAt = &t
	}
	if u.EndedAt != nil {
		t := *u.EndedAt
		r.EndedAt = &t
	}
	m.runs[id] = r
	return r, nil
}

func (m *memStore) TransitionRun(_ context.Context, id string, from []string, to, detail string, endedAt *time.Time) (store.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return r, store.ErrNotFound
	}
	if !slices.Contains(from, r.Status) {
		return store.Run{}, store.ErrConflict
	}
	r.Status = to
	if detail != "" {
		r.Detail = detail
	}
	if endedAt != nil {
		t := *endedAt
		r.EndedAt = &t
	}
	m.runs[id] = r
	return r, nil
}

func (m *memStore) AppendRunEvent(_ context.Context, runID, source, kind string, payload []byte) (store.RunEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if payload == nil {
		payload = []byte("{}")
	}
	e := store.RunEvent{ID: int64(len(m.events) + 1), RunID: runID, Source: source, Kind: kind, Payload: append([]byte(nil), payload...)}
	m.events = append(m.events, e)
	return e, nil
}

func (m *memStore) eventKinds(runID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, e := range m.events {
		if e.RunID == runID {
			out = append(out, e.Source+":"+e.Kind)
		}
	}
	return out
}

// fakeSessions records Create calls.
type fakeSessions struct {
	mu    sync.Mutex
	specs []session.Spec
	at    []time.Time
	err   error
	taken map[string]bool
}

func (f *fakeSessions) Create(_ context.Context, spec session.Spec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.specs = append(f.specs, spec)
	f.at = append(f.at, time.Now())
	if f.err != nil {
		return "", f.err
	}
	name := spec.Name
	for n := 1; f.taken[name]; n++ {
		name = spec.Name + "-" + string(rune('0'+n))
	}
	if f.taken == nil {
		f.taken = map[string]bool{}
	}
	f.taken[name] = true
	return name, nil
}

// fakeAgent is a RunAgent with scripted results.
type fakeAgent struct {
	version    string
	versionErr error
	argv       []string
	buildErr   error
	built      []store.Run
}

func (a *fakeAgent) CheckVersion(context.Context, string) (string, error) {
	return a.version, a.versionErr
}

func (a *fakeAgent) BuildCommand(_ store.QueueItem, run store.Run) ([]string, error) {
	a.built = append(a.built, run)
	return a.argv, a.buildErr
}
