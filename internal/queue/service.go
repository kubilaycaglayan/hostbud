package queue

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/store"
)

// Error is an actionable, user-facing queue error with its HTTP status.
type Error struct {
	Status  int
	Message string
	Hint    string
}

func (e *Error) Error() string { return e.Message }

func invalid(msg, hint string) *Error  { return &Error{http.StatusBadRequest, msg, hint} }
func notFound(msg, hint string) *Error { return &Error{http.StatusNotFound, msg, hint} }
func conflict(msg, hint string) *Error { return &Error{http.StatusConflict, msg, hint} }

// Messages the owner sees (V2-M1 T8).
const (
	msgOneQueue    = "V2-M1 supports one queue; several queues arrive with V2-M2"
	msgParallelOff = "Parallel queues are off — pause queue %s and wait for its run to end, or set `HOSTBUD_PARALLEL_QUEUES=true`"
	msgDuplicate   = "a queue named %q already exists in this project"
	hintDuplicate  = "Pick another name; queue names are unique per project (case doesn't matter)."
	msgActiveRun   = "A run is still active in this queue — pause it and wait for the run to end, or mark its item done/skip it first"
	hintReload     = "Reload the queue and try again."
	hintOnlyQueued = "Only queued items can be edited, deleted or moved. A needs-attention item goes back to the queue with Retry."
)

// Store is what the queue service needs from the store.
type Store interface {
	Project(ctx context.Context, id string) (store.Project, error)
	Queues(ctx context.Context, machineID string) ([]store.Queue, error)
	Queue(ctx context.Context, id string) (store.Queue, error)
	CreateQueue(ctx context.Context, projectID, name string) (store.Queue, error)
	RenameQueue(ctx context.Context, id, name string) (store.Queue, error)
	DeleteQueue(ctx context.Context, id string) error
	TransitionQueue(ctx context.Context, id string, from []string, to string) (store.Queue, error)
	QueueItems(ctx context.Context, queueID string) ([]store.QueueItem, error)
	QueueItem(ctx context.Context, id string) (store.QueueItem, error)
	FirstQueuedItem(ctx context.Context, queueID string) (store.QueueItem, error)
	AddQueueItem(ctx context.Context, queueID, agent, flags, instruction string) (store.QueueItem, error)
	UpdateQueueItem(ctx context.Context, id string, u store.QueueItemUpdate) (store.QueueItem, error)
	DeleteQueueItem(ctx context.Context, id string) error
	ReorderQueueItems(ctx context.Context, queueID string, itemIDs []string) ([]store.QueueItem, error)
	TransitionQueueItem(ctx context.Context, id string, from []string, to string) (store.QueueItem, error)
	LatestRuns(ctx context.Context, queueID string) (map[string]store.Run, error)
	MachineCapacity(ctx context.Context, machineID string) (*int, error)
	SetMachineCapacity(ctx context.Context, machineID string, maxRuns *int) error
	CountActiveRuns(ctx context.Context, machineID string) (int, error)
}

// ItemValidator checks an item's agent, flags and instruction
// (agents.Registry).
type ItemValidator interface {
	ValidateItem(agent, flags, instruction string) error
}

// Control is what the service hands work to (the Dispatcher): Kick looks
// for the next item of a running queue; EndActiveRun cancels an item's
// active run before an owner override (its session stays open);
// CapacityChanged hands out slots after a cap change (V2-M2).
type Control interface {
	Kick(queueID string)
	EndActiveRun(ctx context.Context, item store.QueueItem, action string) error
	CapacityChanged()
}

// RunSummary is an item's latest run, as the panel shows it.
type RunSummary struct {
	ID            string     `json:"id"`
	Status        string     `json:"status"`
	SessionName   string     `json:"sessionName"`
	Detail        string     `json:"detail,omitempty"`
	ClientVersion string     `json:"clientVersion,omitempty"`
	StartedAt     time.Time  `json:"startedAt"`
	EndedAt       *time.Time `json:"endedAt,omitempty"`
}

// ItemView is an item with its latest run.
type ItemView struct {
	store.QueueItem
	Run *RunSummary `json:"run,omitempty"`
	// WaitingForSlot marks the head item of a running queue that has no
	// active run while the machine's cap is reached (V2-M2; derived).
	WaitingForSlot bool `json:"waitingForSlot,omitempty"`
}

// View is a queue with its project and items.
type View struct {
	store.Queue
	ProjectName string     `json:"projectName"`
	ProjectPath string     `json:"projectPath"`
	Items       []ItemView `json:"items"`
	Warnings    []Warning  `json:"warnings,omitempty"`
}

// WarningSharedDirectory: another active queue runs in the same directory,
// so the agents may edit the same files (V2-M2; it never blocks).
const WarningSharedDirectory = "shared_directory"

// Warning is a non-blocking notice on a queue.
type Warning struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Queues  []WarningQueue `json:"queues"`
}

// WarningQueue names another queue a warning is about.
type WarningQueue struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ProjectName string `json:"projectName"`
}

// Changed is the queue.changed payload: the queue after the change, or
// nil when it was deleted.
type Changed struct {
	Action  string `json:"action"`
	QueueID string `json:"queueId"`
	Queue   *View  `json:"queue,omitempty"`
}

// Service is the queue CRUD and control surface (v2 §4). Every change
// publishes queue.changed; reads never do.
type Service struct {
	store     Store
	validator ItemValidator
	bus       *events.Bus
	dispatch  Control
	machine   string
	parallel  bool
}

// NewService returns the queue service for the host machine.
func NewService(st Store, validator ItemValidator, bus *events.Bus) *Service {
	return &Service{store: st, validator: validator, bus: bus, machine: store.HostMachineID}
}

// SetDispatcher connects the dispatcher (T9).
func (s *Service) SetDispatcher(d Control) { s.dispatch = d }

// SetParallelQueues sets the V2-M2 switch (HOSTBUD_PARALLEL_QUEUES): on,
// a project may have several queues and they run in parallel.
func (s *Service) SetParallelQueues(on bool) { s.parallel = on }

// ParallelQueues reports the V2-M2 switch.
func (s *Service) ParallelQueues() bool { return s.parallel }

// List returns the machine's queues with their items.
func (s *Service) List(ctx context.Context) ([]View, error) {
	queues, err := s.store.Queues(ctx, s.machine)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(queues))
	for _, q := range queues {
		v, err := s.view(ctx, q)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	shared := sharedDirectories(out)
	for i := range out {
		out[i].Warnings = shared[out[i].ID]
	}
	return out, nil
}

// Get returns one queue.
func (s *Service) Get(ctx context.Context, id string) (View, error) {
	q, err := s.queue(ctx, id)
	if err != nil {
		return View{}, err
	}
	v, err := s.view(ctx, q)
	if err != nil {
		return View{}, err
	}
	return v, s.addWarnings(ctx, &v)
}

// addWarnings sets v's warnings from the machine's other queues.
func (s *Service) addWarnings(ctx context.Context, v *View) error {
	queues, err := s.store.Queues(ctx, s.machine)
	if err != nil {
		return err
	}
	all := []View{*v}
	for _, q := range queues {
		if q.ID == v.ID {
			continue
		}
		other, err := s.view(ctx, q)
		if err != nil {
			return err
		}
		all = append(all, other)
	}
	v.Warnings = sharedDirectories(all)[v.ID]
	return nil
}

// busy reports whether a queue holds its directory: it is running, or
// paused while a run is still active.
func busy(v View) bool {
	if v.Status == store.QueueRunning {
		return true
	}
	if v.Status != store.QueuePaused {
		return false
	}
	for _, it := range v.Items {
		if it.Run != nil && slices.Contains(store.ActiveRunStatuses, it.Run.Status) {
			return true
		}
	}
	return false
}

// sharedDirectories returns the shared_directory warning of every busy
// queue whose project resolves to the same cleaned path as another busy
// queue's (the same project, or two projects with one path).
func sharedDirectories(views []View) map[string][]Warning {
	byPath := map[string][]View{}
	for _, v := range views {
		if busy(v) && v.ProjectPath != "" {
			p := path.Clean(v.ProjectPath)
			byPath[p] = append(byPath[p], v)
		}
	}
	out := map[string][]Warning{}
	for dir, group := range byPath {
		if len(group) < 2 {
			continue
		}
		for _, v := range group {
			w := Warning{Code: WarningSharedDirectory}
			var names []string
			for _, o := range group {
				if o.ID != v.ID {
					w.Queues = append(w.Queues, WarningQueue{ID: o.ID, Name: o.Name, ProjectName: o.ProjectName})
					names = append(names, o.Name)
				}
			}
			w.Message = fmt.Sprintf("Queue %s also runs in %s — the agents may edit the same files", strings.Join(names, ", "), dir)
			out[v.ID] = []Warning{w}
		}
	}
	return out
}

// peers lists the other queues whose project has the same cleaned path.
func (s *Service) peers(ctx context.Context, queueID, projectPath string) []string {
	if projectPath == "" {
		return nil
	}
	queues, err := s.store.Queues(ctx, s.machine)
	if err != nil {
		return nil
	}
	var out []string
	for _, q := range queues {
		if q.ID == queueID {
			continue
		}
		if p, err := s.store.Project(ctx, q.ProjectID); err == nil && path.Clean(p.Path) == path.Clean(projectPath) {
			out = append(out, q.ID)
		}
	}
	return out
}

func (s *Service) queue(ctx context.Context, id string) (store.Queue, error) {
	q, err := s.store.Queue(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return q, notFound("queue not found", "Reload the Queue panel.")
	}
	return q, err
}

func (s *Service) item(ctx context.Context, id string) (store.QueueItem, error) {
	it, err := s.store.QueueItem(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return it, notFound("queue item not found", "Reload the Queue panel.")
	}
	return it, err
}

func (s *Service) view(ctx context.Context, q store.Queue) (View, error) {
	v := View{Queue: q, Items: []ItemView{}}
	if p, err := s.store.Project(ctx, q.ProjectID); err == nil {
		v.ProjectName, v.ProjectPath = p.Name, p.Path
	} else if !errors.Is(err, store.ErrNotFound) {
		return v, err
	}
	items, err := s.store.QueueItems(ctx, q.ID)
	if err != nil {
		return v, err
	}
	runs, err := s.store.LatestRuns(ctx, q.ID)
	if err != nil {
		return v, err
	}
	for _, it := range items {
		iv := ItemView{QueueItem: it}
		if r, ok := runs[it.ID]; ok {
			iv.Run = &RunSummary{ID: r.ID, Status: r.Status, SessionName: r.SessionName, Detail: r.Detail, ClientVersion: r.ClientVersion, StartedAt: r.StartedAt, EndedAt: r.EndedAt}
		}
		v.Items = append(v.Items, iv)
	}
	head, err := s.waitingFor(ctx, q, items, runs)
	if err != nil {
		return v, err
	}
	for i := range v.Items {
		v.Items[i].WaitingForSlot = v.Items[i].ID == head
	}
	return v, nil
}

// waitingForSlot returns the id of q's head item when it waits for a free
// slot, else "".
func (s *Service) waitingForSlot(ctx context.Context, q store.Queue) (string, error) {
	if !s.parallel || q.Status != store.QueueRunning {
		return "", nil
	}
	items, err := s.store.QueueItems(ctx, q.ID)
	if err != nil {
		return "", err
	}
	runs, err := s.store.LatestRuns(ctx, q.ID)
	if err != nil {
		return "", err
	}
	return s.waitingFor(ctx, q, items, runs)
}

// waitingFor: the head (first queued) item of a running queue with no
// active run waits for a slot while the machine's active runs reach its
// cap. Derived on every read, never stored.
func (s *Service) waitingFor(ctx context.Context, q store.Queue, items []store.QueueItem, runs map[string]store.Run) (string, error) {
	if !s.parallel || q.Status != store.QueueRunning {
		return "", nil
	}
	head := ""
	for _, it := range items {
		if r, ok := runs[it.ID]; ok && r.Active() {
			return "", nil
		}
		if head == "" && it.Status == store.ItemQueued {
			head = it.ID
		}
	}
	if head == "" {
		return "", nil
	}
	limit, err := s.store.MachineCapacity(ctx, q.MachineID)
	if err != nil || limit == nil {
		return "", err
	}
	active, err := s.store.CountActiveRuns(ctx, q.MachineID)
	if err != nil || active < *limit {
		return "", err
	}
	return head, nil
}

// Capacity returns the machine's cap on active runs (nil: no cap).
func (s *Service) Capacity(ctx context.Context) (*int, error) {
	return s.store.MachineCapacity(ctx, s.machine)
}

// SetCapacity sets or clears (nil) the machine's cap. Lowering it stops no
// run; raising or clearing it hands out slots at once. Every queue gets a
// queue.changed, since their waiting state may change (V2-M2).
func (s *Service) SetCapacity(ctx context.Context, maxRuns *int) (*int, error) {
	if err := s.store.SetMachineCapacity(ctx, s.machine, maxRuns); errors.Is(err, store.ErrCapacityRange) {
		return nil, invalid(err.Error(), "Leave it empty for no cap.")
	} else if err != nil {
		return nil, err
	}
	if s.dispatch != nil {
		s.dispatch.CapacityChanged()
	}
	if queues, err := s.store.Queues(ctx, s.machine); err == nil && s.bus != nil {
		for _, q := range queues {
			s.publishOne(ctx, "capacity_changed", q.ID)
		}
	}
	return s.store.MachineCapacity(ctx, s.machine)
}

// Publish sends queue.changed with the queue's current view (the
// dispatcher uses it too). Queues sharing its directory get one as well,
// since their shared-directory warning may have changed with it.
func (s *Service) Publish(ctx context.Context, action, queueID string) {
	if s.bus == nil {
		return
	}
	v := s.publishOne(ctx, action, queueID)
	if v != nil {
		for _, id := range s.peers(ctx, queueID, v.ProjectPath) {
			s.publishOne(ctx, "peer_changed", id)
		}
	}
}

func (s *Service) publishOne(ctx context.Context, action, queueID string) *View {
	payload := Changed{Action: action, QueueID: queueID}
	if q, err := s.store.Queue(ctx, queueID); err == nil {
		if v, err := s.view(ctx, q); err == nil {
			_ = s.addWarnings(ctx, &v)
			payload.Queue = &v
		}
	}
	s.bus.Publish(events.Event{Type: events.QueueChanged, Machine: s.machine, Payload: payload})
	return payload.Queue
}

func (s *Service) changed(ctx context.Context, action, queueID string) (View, error) {
	s.Publish(ctx, action, queueID)
	return s.Get(ctx, queueID)
}

// Create creates a queue for a saved project. With the parallel-queues
// switch off, V2-M1's limit of one queue holds.
func (s *Service) Create(ctx context.Context, projectID, name string) (View, error) {
	queues, err := s.store.Queues(ctx, s.machine)
	if err != nil {
		return View{}, err
	}
	if len(queues) > 0 && !s.parallel {
		return View{}, conflict(msgOneQueue, "Add more items to the existing queue.")
	}
	named := name != ""
	if !named {
		name = "Queue"
	}
	q, err := s.store.CreateQueue(ctx, projectID, name)
	// An unnamed queue takes the first free "Queue n".
	for n := 2; errors.Is(err, store.ErrDuplicate) && !named && n <= 100; n++ {
		q, err = s.store.CreateQueue(ctx, projectID, fmt.Sprintf("Queue %d", n))
	}
	switch {
	case errors.Is(err, store.ErrDuplicate):
		return View{}, conflict(fmt.Sprintf(msgDuplicate, strings.TrimSpace(name)), hintDuplicate)
	case errors.Is(err, store.ErrNotFound):
		return View{}, invalid("project not found", "Pick a saved project; save a folder as a project first.")
	case err != nil && !store.IsUnavailable(err):
		return View{}, invalid(err.Error(), "")
	case err != nil:
		return View{}, err
	}
	return s.changed(ctx, "created", q.ID)
}

// Rename renames a queue.
func (s *Service) Rename(ctx context.Context, id, name string) (View, error) {
	if _, err := s.queue(ctx, id); err != nil {
		return View{}, err
	}
	if _, err := s.store.RenameQueue(ctx, id, name); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			return View{}, conflict(fmt.Sprintf(msgDuplicate, strings.TrimSpace(name)), hintDuplicate)
		}
		if store.IsUnavailable(err) {
			return View{}, err
		}
		return View{}, invalid(err.Error(), "")
	}
	return s.changed(ctx, "renamed", id)
}

// Delete deletes a queue with its items and runs; their sessions stay open.
// It is refused while a run is active.
func (s *Service) Delete(ctx context.Context, id string) error {
	q, err := s.queue(ctx, id)
	if err != nil {
		return err
	}
	var peers []string
	if p, err := s.store.Project(ctx, q.ProjectID); err == nil {
		peers = s.peers(ctx, id, p.Path)
	}
	err = s.store.DeleteQueue(ctx, id)
	if errors.Is(err, store.ErrConflict) {
		return conflict(msgActiveRun, "")
	}
	if err != nil {
		return err
	}
	if s.bus != nil {
		s.bus.Publish(events.Event{Type: events.QueueChanged, Machine: s.machine, Payload: Changed{Action: "deleted", QueueID: id}})
		for _, peer := range peers {
			s.publishOne(ctx, "peer_changed", peer)
		}
	}
	return nil
}

func (s *Service) validate(agent, flags, instruction string) error {
	if err := s.validator.ValidateItem(agent, flags, instruction); err != nil {
		return invalid(err.Error(), "Pick claude or codex, balance any quotes in the flags, and start the instruction with /goal.")
	}
	return nil
}

// AddItem appends a queued item.
func (s *Service) AddItem(ctx context.Context, queueID, agent, flags, instruction string) (ItemView, error) {
	if _, err := s.queue(ctx, queueID); err != nil {
		return ItemView{}, err
	}
	if err := s.validate(agent, flags, instruction); err != nil {
		return ItemView{}, err
	}
	it, err := s.store.AddQueueItem(ctx, queueID, agent, flags, instruction)
	if err != nil {
		if store.IsUnavailable(err) || errors.Is(err, store.ErrNotFound) {
			return ItemView{}, err
		}
		return ItemView{}, invalid(err.Error(), "")
	}
	s.Publish(ctx, "item_added", queueID)
	return ItemView{QueueItem: it}, nil
}

// UpdateItem edits a queued item.
func (s *Service) UpdateItem(ctx context.Context, id string, u store.QueueItemUpdate) (ItemView, error) {
	cur, err := s.item(ctx, id)
	if err != nil {
		return ItemView{}, err
	}
	agent, flags, instruction := cur.Agent, cur.Flags, cur.Instruction
	if u.Agent != nil {
		agent = *u.Agent
	}
	if u.Flags != nil {
		flags = *u.Flags
	}
	if u.Instruction != nil {
		instruction = *u.Instruction
	}
	if cur.Status != store.ItemQueued {
		return ItemView{}, conflict("this item is "+statusWords(cur.Status)+" and can't be edited", hintOnlyQueued)
	}
	if err := s.validate(agent, flags, instruction); err != nil {
		return ItemView{}, err
	}
	it, err := s.store.UpdateQueueItem(ctx, id, u)
	if errors.Is(err, store.ErrConflict) {
		return ItemView{}, conflict("this item has started and can't be edited", hintOnlyQueued)
	}
	if err != nil {
		return ItemView{}, err
	}
	s.Publish(ctx, "item_updated", it.QueueID)
	return ItemView{QueueItem: it}, nil
}

// DeleteItem deletes a queued item.
func (s *Service) DeleteItem(ctx context.Context, id string) error {
	it, err := s.item(ctx, id)
	if err != nil {
		return err
	}
	if it.Status != store.ItemQueued {
		return conflict("this item is "+statusWords(it.Status)+" and can't be deleted", hintOnlyQueued)
	}
	if err := s.store.DeleteQueueItem(ctx, id); errors.Is(err, store.ErrConflict) {
		return conflict("this item has started and can't be deleted", hintOnlyQueued)
	} else if err != nil {
		return err
	}
	s.Publish(ctx, "item_deleted", it.QueueID)
	return nil
}

// Reorder puts the queued items in a new order; itemIDs must list exactly
// the queued items.
func (s *Service) Reorder(ctx context.Context, queueID string, itemIDs []string) (View, error) {
	if _, err := s.queue(ctx, queueID); err != nil {
		return View{}, err
	}
	_, err := s.store.ReorderQueueItems(ctx, queueID, itemIDs)
	if errors.Is(err, store.ErrInvalidOrder) {
		return View{}, conflict("the new order must list exactly the queued items", "Reload the queue: an item may have started meanwhile.")
	}
	if err != nil {
		return View{}, err
	}
	return s.changed(ctx, "reordered", queueID)
}

// Start starts an idle queue, or a finished one that has queued items again.
func (s *Service) Start(ctx context.Context, id string) (View, error) {
	q, err := s.queue(ctx, id)
	if err != nil {
		return View{}, err
	}
	if q.Status == store.QueuePaused {
		return View{}, conflict("the queue is paused; use Resume", "")
	}
	if _, err := s.store.FirstQueuedItem(ctx, id); errors.Is(err, store.ErrNotFound) {
		return View{}, conflict("nothing is queued", "Add an item first.")
	} else if err != nil {
		return View{}, err
	}
	return s.transition(ctx, q, []string{store.QueueIdle, store.QueueFinished}, store.QueueRunning, "started", true)
}

// Pause stops starting new items. The current run continues and is still
// tracked; its session is never touched.
func (s *Service) Pause(ctx context.Context, id string) (View, error) {
	q, err := s.queue(ctx, id)
	if err != nil {
		return View{}, err
	}
	return s.transition(ctx, q, []string{store.QueueRunning}, store.QueuePaused, "paused", false)
}

// Resume continues a paused queue.
func (s *Service) Resume(ctx context.Context, id string) (View, error) {
	q, err := s.queue(ctx, id)
	if err != nil {
		return View{}, err
	}
	return s.transition(ctx, q, []string{store.QueuePaused}, store.QueueRunning, "resumed", true)
}

func (s *Service) transition(ctx context.Context, q store.Queue, from []string, to, action string, kick bool) (View, error) {
	if !slices.Contains(from, q.Status) {
		return View{}, conflict(fmt.Sprintf("the queue is %s; it can't be %s", q.Status, action), hintReload)
	}
	if to == store.QueueRunning && !s.parallel {
		if err := s.checkOnlyActive(ctx, q.ID); err != nil {
			return View{}, err
		}
	}
	if _, err := s.store.TransitionQueue(ctx, q.ID, from, to); errors.Is(err, store.ErrConflict) {
		return View{}, conflict("the queue changed meanwhile; it can't be "+action, hintReload)
	} else if err != nil {
		return View{}, err
	}
	if kick && s.dispatch != nil {
		s.dispatch.Kick(q.ID)
	}
	return s.changed(ctx, action, q.ID)
}

// checkOnlyActive refuses to start or resume a queue, with the switch off,
// while another queue is running or has an active run (queues left over
// from switch-on stay usable one at a time; no run is ever cancelled).
func (s *Service) checkOnlyActive(ctx context.Context, queueID string) error {
	queues, err := s.store.Queues(ctx, s.machine)
	if err != nil {
		return err
	}
	for _, q := range queues {
		if q.ID == queueID {
			continue
		}
		active := q.Status == store.QueueRunning
		if !active {
			runs, err := s.store.LatestRuns(ctx, q.ID)
			if err != nil {
				return err
			}
			for _, r := range runs {
				active = active || r.Active()
			}
		}
		if active {
			return conflict(fmt.Sprintf(msgParallelOff, q.Name), "")
		}
	}
	return nil
}

// Owner overrides on a needs-attention item (V2-M1 T8/T9).
const (
	ActionRetry    = "retry"
	ActionSkip     = "skip"
	ActionMarkDone = "mark-done"
)

// Override applies an owner action to a needs-attention item: retry
// (queued again; the next run gets a new session, the old one stays open),
// skip, or mark done (overrides the evaluator). An active run of the item
// ends as cancelled first. The queue stays paused until Resume.
func (s *Service) Override(ctx context.Context, itemID, action string) (View, error) {
	it, err := s.item(ctx, itemID)
	if err != nil {
		return View{}, err
	}
	to := map[string]string{ActionRetry: store.ItemQueued, ActionSkip: store.ItemSkipped, ActionMarkDone: store.ItemDone}[action]
	if to == "" {
		return View{}, notFound("unknown action", "")
	}
	if it.Status != store.ItemNeedsAttention {
		return View{}, conflict("this item is "+statusWords(it.Status)+"; "+actionWords(action)+" is only for items that need attention", hintReload)
	}
	if s.dispatch != nil {
		if err := s.dispatch.EndActiveRun(ctx, it, action); err != nil {
			return View{}, err
		}
	}
	if _, err := s.store.TransitionQueueItem(ctx, itemID, []string{store.ItemNeedsAttention}, to); errors.Is(err, store.ErrConflict) {
		return View{}, conflict("the item changed meanwhile", hintReload)
	} else if err != nil {
		return View{}, err
	}
	return s.changed(ctx, "item_"+action, it.QueueID)
}

func statusWords(status string) string {
	switch status {
	case store.ItemNeedsAttention:
		return "waiting for your attention"
	case store.ItemRunning:
		return "running"
	default:
		return status
	}
}

func actionWords(action string) string {
	switch action {
	case ActionMarkDone:
		return "Mark done"
	case ActionSkip:
		return "Skip"
	default:
		return "Retry"
	}
}
