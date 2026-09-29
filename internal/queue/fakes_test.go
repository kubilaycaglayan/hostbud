package queue

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
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
	mq     *memQueues
	// V2-M2: the machine cap (nil: none), and the most active runs ever
	// seen at once.
	capacity  *int
	maxActive int
	parallel  *bool
	// V2-M3: the notices queued with a transition that went through.
	outbox []store.Notice
}

func (m *memStore) TransitionQueueItemNotify(ctx context.Context, id string, from []string, to string, n *store.Notice) (store.QueueItem, error) {
	it, err := m.TransitionQueueItem(ctx, id, from, to)
	if err == nil && n != nil {
		m.mu.Lock()
		m.outbox = append(m.outbox, *n)
		m.mu.Unlock()
	}
	return it, err
}

func (m *memStore) TransitionQueueNotify(ctx context.Context, id string, from []string, to string, n *store.Notice) (store.Queue, error) {
	q, err := m.TransitionQueue(ctx, id, from, to)
	if err == nil && n != nil {
		m.mu.Lock()
		m.outbox = append(m.outbox, *n)
		m.mu.Unlock()
	}
	return q, err
}

func (m *memStore) outboxKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, n := range m.outbox {
		out = append(out, n.Key)
	}
	return out
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
	m.maxActive = max(m.maxActive, m.activeLocked())
	return r, nil
}

// CreateRunInSlot is CreateRun behind the cap, like the store's locked
// re-check.
func (m *memStore) CreateRunInSlot(ctx context.Context, itemID string, hash []byte, startedAt time.Time) (store.Run, error) {
	m.mu.Lock()
	if m.capacity != nil && m.activeLocked() >= *m.capacity {
		m.mu.Unlock()
		return store.Run{}, store.ErrNoSlot
	}
	m.mu.Unlock()
	return m.CreateRun(ctx, itemID, hash, startedAt)
}

// activeLocked counts the held run slots: active runs and (V2-M4)
// verifying items, like the store's slotHolders.
func (m *memStore) activeLocked() int {
	n := 0
	for _, r := range m.runs {
		if r.Active() {
			n++
		}
	}
	if m.mq != nil {
		for _, it := range m.mq.items {
			if it.Status == store.ItemVerifying {
				n++
			}
		}
	}
	return n
}

func (m *memStore) MachineCapacity(context.Context, string) (*int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.capacity == nil {
		return nil, nil
	}
	v := *m.capacity
	return &v, nil
}

func (m *memStore) SetMachineCapacity(_ context.Context, _ string, maxRuns *int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if maxRuns != nil && (*maxRuns < store.MinConcurrentRuns || *maxRuns > store.MaxConcurrentRuns) {
		return store.ErrCapacityRange
	}
	if maxRuns == nil {
		m.capacity = nil
		return nil
	}
	v := *maxRuns
	m.capacity = &v
	return nil
}

func (m *memStore) ParallelQueuesSetting(context.Context, string) (*bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.parallel, nil
}

func (m *memStore) SetParallelQueuesSetting(_ context.Context, _ string, on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.parallel = &on
	return nil
}

func (m *memStore) CountActiveRuns(context.Context, string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeLocked(), nil
}

func (m *memStore) WaitingQueues(_ context.Context, machine string) ([]store.Queue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Queue
	for _, q := range m.q().queues {
		if q.MachineID == machine && q.Status == store.QueueRunning && q.WaitingSince != nil {
			out = append(out, q)
		}
	}
	slices.SortFunc(out, func(a, b store.Queue) int {
		if c := a.WaitingSince.Compare(*b.WaitingSince); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

func (m *memStore) SetQueueWaiting(_ context.Context, id string, since *time.Time) (store.Queue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, ok := m.q().queues[id]
	if !ok {
		return q, store.ErrNotFound
	}
	if since == nil {
		q.WaitingSince = nil
	} else {
		t := *since
		q.WaitingSince = &t
	}
	m.q().queues[id] = q
	return q, nil
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
	mu         sync.Mutex
	built      []store.Run
}

func (a *fakeAgent) CheckVersion(context.Context, string) (string, error) {
	return a.version, a.versionErr
}

func (a *fakeAgent) BuildCommand(_ store.QueueItem, run store.Run) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.built = append(a.built, run)
	return a.argv, a.buildErr
}

// ---- queues and items (same rules as the real store) ----

type memQueues struct {
	projects map[string]store.Project
	queues   map[string]store.Queue
	items    map[string]store.QueueItem
	seq      int
}

func (m *memStore) q() *memQueues {
	if m.mq == nil {
		m.mq = &memQueues{projects: map[string]store.Project{}, queues: map[string]store.Queue{}, items: map[string]store.QueueItem{}}
	}
	return m.mq
}

func (m *memStore) addProject(p store.Project) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.q().projects[p.ID] = p
}

func (m *memStore) Project(_ context.Context, id string) (store.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.q().projects[id]
	if !ok {
		return p, store.ErrNotFound
	}
	return p, nil
}

func (m *memStore) Queues(_ context.Context, machine string) ([]store.Queue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Queue
	for _, q := range m.q().queues {
		if q.MachineID == machine {
			out = append(out, q)
		}
	}
	slices.SortFunc(out, func(a, b store.Queue) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func (m *memStore) Queue(_ context.Context, id string) (store.Queue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, ok := m.q().queues[id]
	if !ok {
		return q, store.ErrNotFound
	}
	return q, nil
}

func (m *memStore) CreateQueue(_ context.Context, projectID, name string, afterRunIDs ...string) (store.Queue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.q().projects[projectID]
	if !ok {
		return store.Queue{}, store.ErrNotFound
	}
	if m.nameTaken(projectID, "", name) {
		return store.Queue{}, store.ErrDuplicate
	}
	m.q().seq++
	q := store.Queue{ID: fmt.Sprintf("queue_%02d", m.q().seq), MachineID: p.MachineID, ProjectID: p.ID, Name: name, Status: store.QueueIdle}
	if len(afterRunIDs) > 0 && afterRunIDs[0] != "" {
		q.AfterRunID = &afterRunIDs[0]
	}
	m.q().queues[q.ID] = q
	return q, nil
}

// nameTaken mirrors the queues_project_name index (case-insensitive).
func (m *memStore) nameTaken(projectID, exceptID, name string) bool {
	for _, q := range m.q().queues {
		if q.ProjectID == projectID && q.ID != exceptID && strings.EqualFold(strings.TrimSpace(q.Name), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

func (m *memStore) RenameQueue(_ context.Context, id, name string) (store.Queue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, ok := m.q().queues[id]
	if !ok {
		return q, store.ErrNotFound
	}
	if strings.TrimSpace(name) == "" {
		return q, errors.New("queue name must be 1–255 bytes")
	}
	if m.nameTaken(q.ProjectID, id, name) {
		return q, store.ErrDuplicate
	}
	q.Name = name
	m.q().queues[id] = q
	return q, nil
}

func (m *memStore) DeleteQueue(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.q().queues[id]; !ok {
		return store.ErrNotFound
	}
	for _, r := range m.runs {
		if it, ok := m.q().items[r.ItemID]; ok && it.QueueID == id && r.Active() {
			return store.ErrConflict
		}
	}
	for _, it := range m.q().items {
		if it.QueueID == id && it.Status == store.ItemVerifying {
			return store.ErrConflict
		}
	}
	for iid, it := range m.q().items {
		if it.QueueID == id {
			delete(m.q().items, iid)
			for rid, r := range m.runs {
				if r.ItemID == iid {
					delete(m.runs, rid)
				}
			}
		}
	}
	delete(m.q().queues, id)
	return nil
}

func (m *memStore) TransitionQueue(_ context.Context, id string, from []string, to string) (store.Queue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, ok := m.q().queues[id]
	if !ok {
		return q, store.ErrNotFound
	}
	if !slices.Contains(from, q.Status) {
		return store.Queue{}, store.ErrConflict
	}
	q.Status = to
	m.q().queues[id] = q
	return q, nil
}

func (m *memStore) itemsOf(queueID string) []store.QueueItem {
	var out []store.QueueItem
	for _, it := range m.q().items {
		if it.QueueID == queueID {
			out = append(out, it)
		}
	}
	slices.SortFunc(out, func(a, b store.QueueItem) int { return a.Position - b.Position })
	return out
}

func (m *memStore) QueueItems(_ context.Context, queueID string) ([]store.QueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.itemsOf(queueID), nil
}

func (m *memStore) QueueItem(_ context.Context, id string) (store.QueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.q().items[id]
	if !ok {
		return it, store.ErrNotFound
	}
	return it, nil
}

func (m *memStore) FirstQueuedItem(_ context.Context, queueID string) (store.QueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, it := range m.itemsOf(queueID) {
		if it.Status == store.ItemQueued {
			return it, nil
		}
	}
	return store.QueueItem{}, store.ErrNotFound
}

func (m *memStore) AddQueueItem(_ context.Context, queueID, agent, flags, instruction string, gates ...store.ItemGates) (store.QueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q, ok := m.q().queues[queueID]
	if !ok {
		return store.QueueItem{}, store.ErrNotFound
	}
	m.q().seq++
	it := store.QueueItem{ID: fmt.Sprintf("item_%02d", m.q().seq), QueueID: queueID, MachineID: q.MachineID, Position: len(m.itemsOf(queueID)) + 1,
		Agent: agent, Flags: flags, Instruction: instruction, Status: store.ItemQueued}
	if len(gates) > 0 {
		verify, err := store.NormalizeVerifyCommand(gates[0].VerifyCommand)
		if err != nil {
			return store.QueueItem{}, err
		}
		it.VerifyCommand, it.RequiresApproval = verify, gates[0].RequiresApproval
	}
	m.q().items[it.ID] = it
	return it, nil
}

func (m *memStore) UpdateQueueItem(_ context.Context, id string, u store.QueueItemUpdate) (store.QueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.q().items[id]
	if !ok {
		return it, store.ErrNotFound
	}
	next, err := store.ApplyItemUpdate(it, u)
	if err != nil {
		return store.QueueItem{}, err
	}
	if !slices.Contains(store.EditableStatuses(u), it.Status) {
		return store.QueueItem{}, store.ErrConflict
	}
	m.q().items[id] = next
	return next, nil
}

func (m *memStore) DeleteQueueItem(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.q().items[id]
	if !ok {
		return store.ErrNotFound
	}
	if it.Status != store.ItemQueued {
		return store.ErrConflict
	}
	delete(m.q().items, id)
	for i, rest := range m.itemsOf(it.QueueID) {
		rest.Position = i + 1
		m.q().items[rest.ID] = rest
	}
	return nil
}

func (m *memStore) ReorderQueueItems(_ context.Context, queueID string, ids []string) ([]store.QueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var queued []store.QueueItem
	for _, it := range m.itemsOf(queueID) {
		if it.Status == store.ItemQueued {
			queued = append(queued, it)
		}
	}
	have := make([]string, 0, len(queued))
	for _, it := range queued {
		have = append(have, it.ID)
	}
	want := slices.Clone(ids)
	slices.Sort(want)
	slices.Sort(have)
	if !slices.Equal(want, have) {
		return nil, store.ErrInvalidOrder
	}
	for i, id := range ids {
		it := m.q().items[id]
		it.Position = queued[i].Position
		m.q().items[id] = it
	}
	return m.itemsOf(queueID), nil
}

func (m *memStore) TransitionQueueItem(_ context.Context, id string, from []string, to string) (store.QueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.q().items[id]
	if !ok {
		return it, store.ErrNotFound
	}
	if !slices.Contains(from, it.Status) {
		return store.QueueItem{}, store.ErrConflict
	}
	it.Status = to
	m.q().items[id] = it
	return it, nil
}

func (m *memStore) LatestRuns(_ context.Context, queueID string) (map[string]store.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]store.Run{}
	for _, id := range m.order {
		r := m.runs[id]
		if it, ok := m.q().items[r.ItemID]; ok && it.QueueID == queueID {
			out[r.ItemID] = r
		}
	}
	return out, nil
}

func (m *memStore) ActiveRunForItem(_ context.Context, itemID string) (store.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.order) - 1; i >= 0; i-- {
		if r := m.runs[m.order[i]]; r.ItemID == itemID && r.Active() {
			return r, nil
		}
	}
	return store.Run{}, store.ErrNotFound
}

func (m *memStore) ActiveRuns(_ context.Context) ([]store.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Run
	for _, id := range m.order {
		if r := m.runs[id]; r.Active() {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memStore) RunByTokenHash(_ context.Context, hash []byte) (store.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		if string(r.TokenHash) == string(hash) {
			return r, nil
		}
	}
	return store.Run{}, store.ErrNotFound
}

func (m *memStore) RunEvents(_ context.Context, runID string, limit int) ([]store.RunEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.RunEvent
	for i := len(m.events) - 1; i >= 0 && len(out) < limit; i-- {
		if m.events[i].RunID == runID {
			out = append(out, m.events[i])
		}
	}
	return out, nil
}
