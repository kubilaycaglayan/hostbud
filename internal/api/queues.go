package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"hostbud/internal/queue"
	"hostbud/internal/store"
)

// QueueService is the v2 queue surface (queue.Service).
type QueueService interface {
	List(ctx context.Context) ([]queue.View, error)
	Get(ctx context.Context, id string) (queue.View, error)
	Create(ctx context.Context, projectID, name string, afterRunID ...string) (queue.View, error)
	Rename(ctx context.Context, id, name string) (queue.View, error)
	Delete(ctx context.Context, id string) error
	AddItem(ctx context.Context, queueID, agent, flags, instruction string, gates ...store.ItemGates) (queue.ItemView, error)
	UpdateItem(ctx context.Context, id string, u store.QueueItemUpdate) (queue.ItemView, error)
	DeleteItem(ctx context.Context, id string) error
	Reorder(ctx context.Context, queueID string, itemIDs []string) (queue.View, error)
	Start(ctx context.Context, id string) (queue.View, error)
	Pause(ctx context.Context, id string) (queue.View, error)
	Resume(ctx context.Context, id string) (queue.View, error)
	Override(ctx context.Context, itemID, action string) (queue.View, error)
	Approve(ctx context.Context, itemID string, actor queue.Actor) (queue.View, error)
	Reject(ctx context.Context, itemID string, actor queue.Actor) (queue.View, error)
	Reverify(ctx context.Context, itemID string) (queue.View, error)
	ParallelQueues() bool
	Capacity(ctx context.Context) (*int, error)
	SetCapacity(ctx context.Context, maxRuns *int) (*int, error)
	SetParallel(ctx context.Context, on bool) (bool, error)
}

func mountQueueRoutes(s *server, addFunc func(string, http.HandlerFunc)) {
	addFunc("GET /api/queues", s.listQueues)
	addFunc("POST /api/queues", s.createQueue)
	addFunc("GET /api/queues/{id}", s.getQueue)
	addFunc("PATCH /api/queues/{id}", s.renameQueue)
	addFunc("DELETE /api/queues/{id}", s.deleteQueue)
	addFunc("POST /api/queues/{id}/items", s.addQueueItem)
	addFunc("PUT /api/queues/{id}/order", s.reorderQueue)
	addFunc("PATCH /api/queue-items/{id}", s.updateQueueItem)
	addFunc("DELETE /api/queue-items/{id}", s.deleteQueueItem)
	for _, action := range []string{"start", "pause", "resume"} {
		addFunc("POST /api/queues/{id}/"+action, s.queueControl(action))
	}
	for _, action := range []string{queue.ActionRetry, queue.ActionSkip, queue.ActionMarkDone} {
		addFunc("POST /api/queue-items/{id}/"+action, s.queueOverride(action))
	}
	// V2-M4: the approval gate's owner actions.
	addFunc("POST /api/queue-items/{id}/approve", s.queueApproval(true))
	addFunc("POST /api/queue-items/{id}/reject", s.queueApproval(false))
	addFunc("POST /api/queue-items/{id}/reverify", s.queueReverify)
	// V2-M2: the per-machine cap on active runs (Settings).
	addFunc("GET /api/machines/{machine}/capacity", s.getCapacity)
	addFunc("PUT /api/machines/{machine}/capacity", s.putCapacity)
	// The parallel-queues switch (Queue panel), over HOSTBUD_PARALLEL_QUEUES.
	addFunc("PUT /api/machines/{machine}/parallel-queues", s.putParallelQueues)
}

func (s *server) queueError(w http.ResponseWriter, err error) {
	var qe *queue.Error
	switch {
	case errors.As(err, &qe):
		writeError(w, qe.Status, qe.Message, qe.Hint)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found", "Reload the Queue panel.")
	case store.IsUnavailable(err):
		writeDatabaseUnavailable(w)
	default:
		s.cfg.Log.Error("queue request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error", "")
	}
}

func (s *server) listQueues(w http.ResponseWriter, r *http.Request) {
	queues, err := s.cfg.Queues.List(r.Context())
	if err != nil {
		s.queueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, queueList{Queues: queues, ParallelQueues: s.cfg.Queues.ParallelQueues()})
}

// queueList is GET /api/queues: the queues and the V2-M2 switch.
type queueList struct {
	Queues         []queue.View `json:"queues"`
	ParallelQueues bool         `json:"parallelQueues"`
}

// capacityBody is the capacity route's body: a whole number 1–32, or null
// for no cap.
type capacityBody struct {
	MaxConcurrentRuns *int `json:"maxConcurrentRuns"`
}

func (s *server) capacityMachine(w http.ResponseWriter, r *http.Request) bool {
	if r.PathValue("machine") != store.HostMachineID {
		writeError(w, http.StatusNotFound, "unknown machine", "Reload hostbud.")
		return false
	}
	return true
}

func (s *server) getCapacity(w http.ResponseWriter, r *http.Request) {
	if !s.capacityMachine(w, r) {
		return
	}
	c, err := s.cfg.Queues.Capacity(r.Context())
	if err != nil {
		s.queueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, capacityBody{MaxConcurrentRuns: c})
}

func (s *server) putCapacity(w http.ResponseWriter, r *http.Request) {
	if !s.capacityMachine(w, r) {
		return
	}
	var req struct {
		MaxConcurrentRuns json.RawMessage `json:"maxConcurrentRuns"`
	}
	if !decode(w, r, &req) {
		return
	}
	var limit *int
	switch raw := strings.TrimSpace(string(req.MaxConcurrentRuns)); raw {
	case "":
		writeError(w, http.StatusBadRequest, "maxConcurrentRuns is required", "Send a whole number from 1 to 32, or null to restore the default of two.")
		return
	case "null":
	default:
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "maxConcurrentRuns must be a whole number from 1 to 32, or null", "Leave it empty to restore the default of two.")
			return
		}
		limit = &n
	}
	c, err := s.cfg.Queues.SetCapacity(r.Context(), limit)
	if err != nil {
		s.queueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, capacityBody{MaxConcurrentRuns: c})
}

// parallelBody is the parallel-queues route's body and answer.
type parallelBody struct {
	ParallelQueues bool `json:"parallelQueues"`
}

func (s *server) putParallelQueues(w http.ResponseWriter, r *http.Request) {
	if !s.capacityMachine(w, r) {
		return
	}
	var req struct {
		ParallelQueues *bool `json:"parallelQueues"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.ParallelQueues == nil {
		writeError(w, http.StatusBadRequest, "parallelQueues is required", "Send true or false.")
		return
	}
	on, err := s.cfg.Queues.SetParallel(r.Context(), *req.ParallelQueues)
	if err != nil {
		s.queueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, parallelBody{ParallelQueues: on})
}

func (s *server) getQueue(w http.ResponseWriter, r *http.Request) {
	v, err := s.cfg.Queues.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.queueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type createQueueRequest struct {
	ProjectID  string `json:"projectId"`
	Name       string `json:"name"`
	AfterRunID string `json:"afterRunId"`
}

func (s *server) createQueue(w http.ResponseWriter, r *http.Request) {
	var req createQueueRequest
	if !decode(w, r, &req) {
		return
	}
	v, err := s.cfg.Queues.Create(r.Context(), req.ProjectID, req.Name, req.AfterRunID)
	if err != nil {
		s.queueError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

type renameQueueRequest struct {
	Name string `json:"name"`
}

func (s *server) renameQueue(w http.ResponseWriter, r *http.Request) {
	var req renameQueueRequest
	if !decode(w, r, &req) {
		return
	}
	v, err := s.cfg.Queues.Rename(r.Context(), r.PathValue("id"), req.Name)
	if err != nil {
		s.queueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *server) deleteQueue(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Queues.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.queueError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type queueItemRequest struct {
	Agent       *string `json:"agent"`
	Flags       *string `json:"flags"`
	Instruction *string `json:"instruction"`
	// V2-M4 completion gates ("" = no verify command).
	VerifyCommand    *string `json:"verifyCommand"`
	RequiresApproval *bool   `json:"requiresApproval"`
}

func (s *server) addQueueItem(w http.ResponseWriter, r *http.Request) {
	var req queueItemRequest
	if !decode(w, r, &req) {
		return
	}
	value := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	var gates []store.ItemGates
	if req.VerifyCommand != nil || req.RequiresApproval != nil {
		gates = append(gates, store.ItemGates{VerifyCommand: value(req.VerifyCommand), RequiresApproval: req.RequiresApproval != nil && *req.RequiresApproval})
	}
	it, err := s.cfg.Queues.AddItem(r.Context(), r.PathValue("id"), value(req.Agent), value(req.Flags), value(req.Instruction), gates...)
	if err != nil {
		s.queueError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, it)
}

func (s *server) updateQueueItem(w http.ResponseWriter, r *http.Request) {
	var req queueItemRequest
	if !decode(w, r, &req) {
		return
	}
	it, err := s.cfg.Queues.UpdateItem(r.Context(), r.PathValue("id"), store.QueueItemUpdate{
		Agent: req.Agent, Flags: req.Flags, Instruction: req.Instruction, VerifyCommand: req.VerifyCommand, RequiresApproval: req.RequiresApproval,
	})
	if err != nil {
		s.queueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *server) deleteQueueItem(w http.ResponseWriter, r *http.Request) {
	if err := s.cfg.Queues.DeleteItem(r.Context(), r.PathValue("id")); err != nil {
		s.queueError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type reorderQueueRequest struct {
	ItemIDs []string `json:"itemIds"`
}

func (s *server) reorderQueue(w http.ResponseWriter, r *http.Request) {
	var req reorderQueueRequest
	if !decode(w, r, &req) {
		return
	}
	if len(req.ItemIDs) > 1000 {
		writeError(w, http.StatusBadRequest, "too many items", "")
		return
	}
	v, err := s.cfg.Queues.Reorder(r.Context(), r.PathValue("id"), req.ItemIDs)
	if err != nil {
		s.queueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *server) queueControl(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var v queue.View
		var err error
		switch action {
		case "start":
			v, err = s.cfg.Queues.Start(r.Context(), id)
		case "pause":
			v, err = s.cfg.Queues.Pause(r.Context(), id)
		default:
			v, err = s.cfg.Queues.Resume(r.Context(), id)
		}
		if err != nil {
			s.queueError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

func (s *server) queueOverride(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := s.cfg.Queues.Override(r.Context(), r.PathValue("id"), action)
		if err != nil {
			s.queueError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

func (s *server) queueApproval(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := r.Context().Value(userKey{}).(store.User)
		if u.ID == "" {
			writeError(w, http.StatusUnauthorized, "sign in first", "")
			return
		}
		actor := queue.Actor{ID: u.ID, Email: u.Email}
		var v queue.View
		var err error
		if approve {
			v, err = s.cfg.Queues.Approve(r.Context(), r.PathValue("id"), actor)
		} else {
			v, err = s.cfg.Queues.Reject(r.Context(), r.PathValue("id"), actor)
		}
		if err != nil {
			s.queueError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

func (s *server) queueReverify(w http.ResponseWriter, r *http.Request) {
	v, err := s.cfg.Queues.Reverify(r.Context(), r.PathValue("id"))
	if err != nil {
		s.queueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
