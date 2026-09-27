package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"hostbud/internal/events"
	"hostbud/internal/queue"
	"hostbud/internal/store"
)

// fakeQueues records calls and returns a scripted error.
type fakeQueues struct {
	calls []string
	err   error
	upd   store.QueueItemUpdate
	order []string
}

func (f *fakeQueues) rec(call string) { f.calls = append(f.calls, call) }

func (f *fakeQueues) List(context.Context) ([]queue.View, error) {
	f.rec("list")
	return []queue.View{{Queue: store.Queue{ID: "queue_a", Status: store.QueueIdle}, Items: []queue.ItemView{}}}, f.err
}
func (f *fakeQueues) Get(_ context.Context, id string) (queue.View, error) {
	f.rec("get " + id)
	return queue.View{Queue: store.Queue{ID: id}}, f.err
}
func (f *fakeQueues) Create(_ context.Context, projectID, name string) (queue.View, error) {
	f.rec("create " + projectID + " " + name)
	return queue.View{Queue: store.Queue{ID: "queue_a", ProjectID: projectID, Name: name}}, f.err
}
func (f *fakeQueues) Rename(_ context.Context, id, name string) (queue.View, error) {
	f.rec("rename " + id + " " + name)
	return queue.View{}, f.err
}
func (f *fakeQueues) Delete(_ context.Context, id string) error { f.rec("delete " + id); return f.err }
func (f *fakeQueues) AddItem(_ context.Context, queueID, agent, flags, instruction string) (queue.ItemView, error) {
	f.rec("add " + queueID + " " + agent + "|" + flags + "|" + instruction)
	return queue.ItemView{QueueItem: store.QueueItem{ID: "item_a", Agent: agent}}, f.err
}
func (f *fakeQueues) UpdateItem(_ context.Context, id string, u store.QueueItemUpdate) (queue.ItemView, error) {
	f.rec("update " + id)
	f.upd = u
	return queue.ItemView{}, f.err
}
func (f *fakeQueues) DeleteItem(_ context.Context, id string) error {
	f.rec("delete-item " + id)
	return f.err
}
func (f *fakeQueues) Reorder(_ context.Context, queueID string, ids []string) (queue.View, error) {
	f.rec("reorder " + queueID)
	f.order = ids
	return queue.View{}, f.err
}
func (f *fakeQueues) Start(_ context.Context, id string) (queue.View, error) {
	f.rec("start " + id)
	return queue.View{}, f.err
}
func (f *fakeQueues) Pause(_ context.Context, id string) (queue.View, error) {
	f.rec("pause " + id)
	return queue.View{}, f.err
}
func (f *fakeQueues) Resume(_ context.Context, id string) (queue.View, error) {
	f.rec("resume " + id)
	return queue.View{}, f.err
}
func (f *fakeQueues) Override(_ context.Context, id, action string) (queue.View, error) {
	f.rec(action + " " + id)
	return queue.View{}, f.err
}

func queueEnv(t *testing.T, q *fakeQueues) http.Handler {
	t.Helper()
	return New(Config{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Dist: fstest.MapFS{},
		Origins: AllowedOrigins("hostbud.example.com", 9055), Bus: events.NewBus(), Auth: &fakeAuth{}, Queues: q,
	})
}

func queueRequest(t *testing.T, h http.Handler, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", origin)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testToken})
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestQueueRoutesCallTheService(t *testing.T) {
	q := &fakeQueues{}
	h := queueEnv(t, q)
	for _, c := range []struct {
		method, path, body string
		status             int
		call               string
	}{
		{"GET", "/api/queues", "", 200, "list"},
		{"POST", "/api/queues", `{"projectId":"project_a","name":"Milestones"}`, 201, "create project_a Milestones"},
		{"GET", "/api/queues/queue_a", "", 200, "get queue_a"},
		{"PATCH", "/api/queues/queue_a", `{"name":"M"}`, 200, "rename queue_a M"},
		{"DELETE", "/api/queues/queue_a", "", 204, "delete queue_a"},
		{"POST", "/api/queues/queue_a/items", `{"agent":"claude","flags":"--yolo","instruction":"/goal m1"}`, 201, "add queue_a claude|--yolo|/goal m1"},
		{"PATCH", "/api/queue-items/item_a", `{"instruction":"/goal m2"}`, 200, "update item_a"},
		{"DELETE", "/api/queue-items/item_a", "", 204, "delete-item item_a"},
		{"PUT", "/api/queues/queue_a/order", `{"itemIds":["item_b","item_a"]}`, 200, "reorder queue_a"},
		{"POST", "/api/queues/queue_a/start", "", 200, "start queue_a"},
		{"POST", "/api/queues/queue_a/pause", "", 200, "pause queue_a"},
		{"POST", "/api/queues/queue_a/resume", "", 200, "resume queue_a"},
		{"POST", "/api/queue-items/item_a/retry", "", 200, "retry item_a"},
		{"POST", "/api/queue-items/item_a/skip", "", 200, "skip item_a"},
		{"POST", "/api/queue-items/item_a/mark-done", "", 200, "mark-done item_a"},
	} {
		q.calls = nil
		rec := queueRequest(t, h, c.method, c.path, c.body, nil)
		if rec.Code != c.status || len(q.calls) != 1 || q.calls[0] != c.call {
			t.Errorf("%s %s: %d %s, calls %v", c.method, c.path, rec.Code, rec.Body, q.calls)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s: not no-store", c.method, c.path)
		}
	}
	if q.upd.Instruction == nil || *q.upd.Instruction != "/goal m2" || q.upd.Agent != nil || q.upd.Flags != nil {
		t.Errorf("partial update %+v", q.upd)
	}
	if strings.Join(q.order, ",") != "item_b,item_a" {
		t.Errorf("order %v", q.order)
	}
}

func TestQueueRoutesMapErrorsAndRefuseForeignOrigins(t *testing.T) {
	q := &fakeQueues{err: &queue.Error{Status: http.StatusConflict, Message: "V2-M1 supports one queue; several queues arrive with V2-M2", Hint: "Add more items."}}
	h := queueEnv(t, q)
	rec := queueRequest(t, h, "POST", "/api/queues", `{"projectId":"p","name":"n"}`, nil)
	body := decodeBody[errorBody](t, rec)
	if rec.Code != http.StatusConflict || body.Error != "V2-M1 supports one queue; several queues arrive with V2-M2" || body.Hint != "Add more items." {
		t.Fatalf("mapped error: %d %+v", rec.Code, body)
	}
	q.err = store.ErrNotFound
	if rec := queueRequest(t, h, "GET", "/api/queues/nope", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("not found: %d", rec.Code)
	}
	q.err, q.calls = nil, nil
	for _, path := range []string{"/api/queues", "/api/queues/queue_a/start", "/api/queue-items/item_a/skip"} {
		if rec := queueRequest(t, h, "POST", path, `{"projectId":"p","name":"n"}`, map[string]string{"Origin": "http://evil.example.com"}); rec.Code != http.StatusForbidden {
			t.Errorf("foreign Origin %s: %d", path, rec.Code)
		}
	}
	if len(q.calls) != 0 {
		t.Fatalf("a foreign-Origin request reached the service: %v", q.calls)
	}
	if rec := queueRequest(t, h, "POST", "/api/queues", `{"projectId":"p","bogus":1}`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", rec.Code)
	}
}
