package api

import (
	"context"
	"errors"
	"net/http"

	"hostbud/internal/queue"
	"hostbud/internal/store"
)

// QueueService is the v2 queue surface (queue.Service).
type QueueService interface {
	List(ctx context.Context) ([]queue.View, error)
	Get(ctx context.Context, id string) (queue.View, error)
	Create(ctx context.Context, projectID, name string) (queue.View, error)
	Rename(ctx context.Context, id, name string) (queue.View, error)
	Delete(ctx context.Context, id string) error
	AddItem(ctx context.Context, queueID, agent, flags, instruction string) (queue.ItemView, error)
	UpdateItem(ctx context.Context, id string, u store.QueueItemUpdate) (queue.ItemView, error)
	DeleteItem(ctx context.Context, id string) error
	Reorder(ctx context.Context, queueID string, itemIDs []string) (queue.View, error)
	Start(ctx context.Context, id string) (queue.View, error)
	Pause(ctx context.Context, id string) (queue.View, error)
	Resume(ctx context.Context, id string) (queue.View, error)
	Override(ctx context.Context, itemID, action string) (queue.View, error)
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
	writeJSON(w, http.StatusOK, map[string][]queue.View{"queues": queues})
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
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
}

func (s *server) createQueue(w http.ResponseWriter, r *http.Request) {
	var req createQueueRequest
	if !decode(w, r, &req) {
		return
	}
	v, err := s.cfg.Queues.Create(r.Context(), req.ProjectID, req.Name)
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
	it, err := s.cfg.Queues.AddItem(r.Context(), r.PathValue("id"), value(req.Agent), value(req.Flags), value(req.Instruction))
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
	it, err := s.cfg.Queues.UpdateItem(r.Context(), r.PathValue("id"), store.QueueItemUpdate{Agent: req.Agent, Flags: req.Flags, Instruction: req.Instruction})
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
