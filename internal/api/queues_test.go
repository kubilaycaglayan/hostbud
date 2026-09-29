package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/queue"
	"hostbud/internal/store"
)

// fakeQueues records calls and returns a scripted error.
type fakeQueues struct {
	calls    []string
	err      error
	upd      store.QueueItemUpdate
	gates    []store.ItemGates
	order    []string
	parallel bool
	capacity *int
	delay    time.Duration
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
func (f *fakeQueues) CreateLinked(_ context.Context, projectID, name string, link store.QueueLink) (queue.View, error) {
	msg := "create " + projectID + " " + name
	if link.RunID != "" {
		msg += " after-run " + link.RunID
	}
	if link.Session != "" {
		msg += " after-session " + link.Session
	}
	f.rec(msg)
	return queue.View{Queue: store.Queue{ID: "queue_a", ProjectID: projectID, Name: name}}, f.err
}
func (f *fakeQueues) Rename(_ context.Context, id, name string) (queue.View, error) {
	f.rec("rename " + id + " " + name)
	return queue.View{}, f.err
}
func (f *fakeQueues) SetLoop(_ context.Context, id string, enabled bool, maxRuntime time.Duration) (queue.View, error) {
	f.rec(fmt.Sprintf("loop %s %t %s", id, enabled, maxRuntime))
	return queue.View{}, f.err
}
func (f *fakeQueues) Delete(_ context.Context, id string) error { f.rec("delete " + id); return f.err }
func (f *fakeQueues) AddItem(_ context.Context, queueID, agent, flags, instruction string, gates ...store.ItemGates) (queue.ItemView, error) {
	f.rec("add " + queueID + " " + agent + "|" + flags + "|" + instruction)
	f.gates = gates
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
func (f *fakeQueues) Start(_ context.Context, id string, delay ...time.Duration) (queue.View, error) {
	f.rec("start " + id)
	if len(delay) > 0 {
		f.delay = delay[0]
	}
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
func (f *fakeQueues) Approve(_ context.Context, id string, actor queue.Actor) (queue.View, error) {
	f.rec("approve " + id + " by " + actor.ID)
	return queue.View{}, f.err
}
func (f *fakeQueues) Reject(_ context.Context, id string, actor queue.Actor) (queue.View, error) {
	f.rec("reject " + id + " by " + actor.ID)
	return queue.View{}, f.err
}
func (f *fakeQueues) Reverify(_ context.Context, id string) (queue.View, error) {
	f.rec("reverify " + id)
	return queue.View{}, f.err
}
func (f *fakeQueues) Override(_ context.Context, id, action string) (queue.View, error) {
	f.rec(action + " " + id)
	return queue.View{}, f.err
}

func (f *fakeQueues) ParallelQueues() bool { return f.parallel }
func (f *fakeQueues) History(_ context.Context, limit, offset int) ([]store.QueueItemHistory, error) {
	f.rec(fmt.Sprintf("history %d %d", limit, offset))
	return []store.QueueItemHistory{}, f.err
}
func (f *fakeQueues) Capacity(context.Context) (*int, error) {
	f.rec("capacity")
	return f.capacity, f.err
}
func (f *fakeQueues) SetCapacity(_ context.Context, maxRuns *int) (*int, error) {
	if maxRuns == nil {
		f.rec("set-capacity null")
	} else {
		f.rec(fmt.Sprintf("set-capacity %d", *maxRuns))
		if *maxRuns < store.MinConcurrentRuns || *maxRuns > store.MaxConcurrentRuns {
			return nil, &queue.Error{Status: http.StatusBadRequest, Message: store.ErrCapacityRange.Error()}
		}
	}
	f.capacity = maxRuns
	return maxRuns, f.err
}

func (f *fakeQueues) SetParallel(_ context.Context, on bool) (bool, error) {
	f.rec(fmt.Sprintf("set-parallel %v", on))
	if f.err == nil {
		f.parallel = on
	}
	return f.parallel, f.err
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
		{"GET", "/api/queue-history?limit=25&offset=10", "", 200, "history 25 10"},
		{"POST", "/api/queues", `{"projectId":"project_a","name":"Milestones"}`, 201, "create project_a Milestones"},
		{"POST", "/api/queues", `{"projectId":"project_a","name":"Next","afterSession":"manual-work"}`, 201, "create project_a Next after-session manual-work"},
		{"GET", "/api/queues/queue_a", "", 200, "get queue_a"},
		{"PATCH", "/api/queues/queue_a", `{"name":"M"}`, 200, "rename queue_a M"},
		{"DELETE", "/api/queues/queue_a", "", 204, "delete queue_a"},
		{"POST", "/api/queues/queue_a/items", `{"agent":"claude","flags":"--yolo","instruction":"/goal m1"}`, 201, "add queue_a claude|--yolo|/goal m1"},
		{"PATCH", "/api/queue-items/item_a", `{"instruction":"/goal m2"}`, 200, "update item_a"},
		{"DELETE", "/api/queue-items/item_a", "", 204, "delete-item item_a"},
		{"PUT", "/api/queues/queue_a/order", `{"itemIds":["item_b","item_a"]}`, 200, "reorder queue_a"},
		{"PUT", "/api/queues/queue_a/loop", `{"enabled":true,"maxRuntime":"5h30m"}`, 200, "loop queue_a true 5h30m0s"},
		{"PUT", "/api/queues/queue_a/loop", `{"enabled":true}`, 200, "loop queue_a true 5h0m0s"},
		{"PUT", "/api/queues/queue_a/loop", `{"enabled":false}`, 200, "loop queue_a false 5h0m0s"},
		{"POST", "/api/queues/queue_a/start", "", 200, "start queue_a"},
		{"POST", "/api/queues/queue_a/start", `{"delay":"4h14m"}`, 200, "start queue_a"},
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
	if q.delay != 254*time.Minute {
		t.Errorf("start delay = %s", q.delay)
	}
	for _, value := range []string{"0s", "nonsense", "721h", "-1h"} {
		if rec := queueRequest(t, h, "PUT", "/api/queues/queue_a/loop", `{"enabled":true,"maxRuntime":"`+value+`"}`, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("loop maxRuntime %q status = %d", value, rec.Code)
		}
	}
	for _, value := range []string{"0s", "nonsense", "31d"} {
		if rec := queueRequest(t, h, "POST", "/api/queues/queue_a/start", `{"delay":"`+value+`"}`, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("delay %q status = %d", value, rec.Code)
		}
	}
	for _, path := range []string{"/api/queue-history?limit=0", "/api/queue-history?limit=201", "/api/queue-history?offset=-1"} {
		if rec := queueRequest(t, h, "GET", path, "", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("history query %q status = %d", path, rec.Code)
		}
	}
}

func TestQueueRoutesMapErrorsAndRefuseForeignOrigins(t *testing.T) {
	q := &fakeQueues{err: &queue.Error{Status: http.StatusConflict, Message: "a queue named \"n\" already exists in this project", Hint: "Pick another name."}}
	h := queueEnv(t, q)
	unauthenticated := httptest.NewRequestWithContext(t.Context(), "GET", "/api/queue-history", nil)
	unauthenticated.Header.Set("Origin", "https://attacker.example")
	unauthenticatedRec := httptest.NewRecorder()
	h.ServeHTTP(unauthenticatedRec, unauthenticated)
	if unauthenticatedRec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated queue history status = %d", unauthenticatedRec.Code)
	}
	rec := queueRequest(t, h, "POST", "/api/queues", `{"projectId":"p","name":"n"}`, nil)
	body := decodeBody[errorBody](t, rec)
	if rec.Code != http.StatusConflict || body.Error != `a queue named "n" already exists in this project` || body.Hint != "Pick another name." {
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

// V2-M2 T5: the capacity route validates its body, answers 404 for another
// machine, and GET /api/queues reports the parallel-queues switch.
func TestCapacityRouteAndParallelFlag(t *testing.T) {
	q := &fakeQueues{parallel: true}
	h := queueEnv(t, q)
	for _, c := range []struct {
		method, path, body string
		status             int
		call               string
		response           string
	}{
		{"GET", "/api/machines/host/capacity", "", 200, "capacity", `{"maxConcurrentRuns":null}`},
		{"PUT", "/api/machines/host/capacity", `{"maxConcurrentRuns":2}`, 200, "set-capacity 2", `{"maxConcurrentRuns":2}`},
		{"GET", "/api/machines/host/capacity", "", 200, "capacity", `{"maxConcurrentRuns":2}`},
		{"PUT", "/api/machines/host/capacity", `{"maxConcurrentRuns":null}`, 200, "set-capacity null", `{"maxConcurrentRuns":null}`},
		{"PUT", "/api/machines/host/capacity", `{"maxConcurrentRuns":0}`, 400, "set-capacity 0", ""},
		{"PUT", "/api/machines/host/capacity", `{"maxConcurrentRuns":33}`, 400, "set-capacity 33", ""},
		{"PUT", "/api/machines/host/capacity", `{"maxConcurrentRuns":1.5}`, 400, "", ""},
		{"PUT", "/api/machines/host/capacity", `{"maxConcurrentRuns":"2"}`, 400, "", ""},
		{"PUT", "/api/machines/host/capacity", `{"maxConcurrentRuns":true}`, 400, "", ""},
		{"PUT", "/api/machines/host/capacity", `{}`, 400, "", ""},
		{"PUT", "/api/machines/host/capacity", `{"maxConcurrentRuns":2,"extra":1}`, 400, "", ""},
		{"PUT", "/api/machines/server-a/capacity", `{"maxConcurrentRuns":2}`, 404, "", ""},
		{"GET", "/api/machines/server-a/capacity", "", 404, "", ""},
		{"PUT", "/api/machines/host/parallel-queues", `{"parallelQueues":false}`, 200, "set-parallel false", `{"parallelQueues":false}`},
		{"PUT", "/api/machines/host/parallel-queues", `{"parallelQueues":true}`, 200, "set-parallel true", `{"parallelQueues":true}`},
		{"PUT", "/api/machines/host/parallel-queues", `{}`, 400, "", ""},
		{"PUT", "/api/machines/host/parallel-queues", `{"parallelQueues":"true"}`, 400, "", ""},
		{"PUT", "/api/machines/host/parallel-queues", `{"parallelQueues":true,"extra":1}`, 400, "", ""},
		{"PUT", "/api/machines/server-a/parallel-queues", `{"parallelQueues":true}`, 404, "", ""},
	} {
		q.calls = nil
		rec := queueRequest(t, h, c.method, c.path, c.body, nil)
		wantCalls := 0
		if c.call != "" {
			wantCalls = 1
		}
		if rec.Code != c.status || len(q.calls) != wantCalls || (wantCalls == 1 && q.calls[0] != c.call) {
			t.Errorf("%s %s %s: %d %s, calls %v", c.method, c.path, c.body, rec.Code, rec.Body, q.calls)
		}
		if c.response != "" && strings.TrimSpace(rec.Body.String()) != c.response {
			t.Errorf("%s %s %s: body %s, want %s", c.method, c.path, c.body, rec.Body, c.response)
		}
	}
	rec := queueRequest(t, h, "GET", "/api/queues", "", nil)
	var list struct {
		Queues         []map[string]any `json:"queues"`
		ParallelQueues *bool            `json:"parallelQueues"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || list.ParallelQueues == nil || !*list.ParallelQueues || len(list.Queues) != 1 {
		t.Fatalf("GET /api/queues: %s, %v", rec.Body, err)
	}
	// A bad Origin never reaches the service.
	q.calls = nil
	rec = queueRequest(t, h, "PUT", "/api/machines/host/capacity", `{"maxConcurrentRuns":2}`, map[string]string{"Origin": "http://evil.example.com"})
	if rec.Code != http.StatusForbidden || len(q.calls) != 0 {
		t.Fatalf("bad origin: %d, calls %v", rec.Code, q.calls)
	}
}

// V2-M4 T1: items take the gate fields on create and edit; without them the
// service gets no gates (V2-M3 behavior).
func TestQueueItemGateFields(t *testing.T) {
	q := &fakeQueues{}
	h := queueEnv(t, q)
	if rec := queueRequest(t, h, "POST", "/api/queues/queue_a/items", `{"agent":"claude","instruction":"/goal m1"}`, nil); rec.Code != 201 || len(q.gates) != 0 {
		t.Fatalf("no gates: %d, %+v", rec.Code, q.gates)
	}
	rec := queueRequest(t, h, "POST", "/api/queues/queue_a/items", `{"agent":"claude","instruction":"/goal m1","verifyCommand":"make test","requiresApproval":true}`, nil)
	if rec.Code != 201 || len(q.gates) != 1 || q.gates[0] != (store.ItemGates{VerifyCommand: "make test", RequiresApproval: true}) {
		t.Fatalf("gates: %d, %+v", rec.Code, q.gates)
	}
	rec = queueRequest(t, h, "PATCH", "/api/queue-items/item_a", `{"verifyCommand":"","requiresApproval":false}`, nil)
	if rec.Code != 200 || q.upd.VerifyCommand == nil || *q.upd.VerifyCommand != "" || q.upd.RequiresApproval == nil || *q.upd.RequiresApproval || q.upd.Instruction != nil {
		t.Fatalf("gate edit: %d, %+v", rec.Code, q.upd)
	}
	var item store.QueueItem
	if err := json.Unmarshal([]byte(`{"verifyCommand":"make test","requiresApproval":true}`), &item); err != nil || item.VerifyCommand != "make test" || !item.RequiresApproval {
		t.Fatalf("item JSON carries the gates: %+v, %v", item, err)
	}
}

// V2-M4 T3: Approve and Reject pass the signed-in account; foreign Origins
// never reach the service.
func TestQueueApprovalRoutes(t *testing.T) {
	q := &fakeQueues{}
	h := queueEnv(t, q)
	for path, call := range map[string]string{
		"/api/queue-items/item_a/approve":  "approve item_a by u1",
		"/api/queue-items/item_a/reject":   "reject item_a by u1",
		"/api/queue-items/item_a/reverify": "reverify item_a",
	} {
		q.calls = nil
		if rec := queueRequest(t, h, "POST", path, "", nil); rec.Code != 200 || len(q.calls) != 1 || q.calls[0] != call {
			t.Errorf("%s: %d, calls %v", path, rec.Code, q.calls)
		}
		q.calls = nil
		if rec := queueRequest(t, h, "POST", path, "", map[string]string{"Origin": "http://evil.example.com"}); rec.Code != http.StatusForbidden || len(q.calls) != 0 {
			t.Errorf("%s foreign Origin: %d, calls %v", path, rec.Code, q.calls)
		}
	}
	q.err = &queue.Error{Status: http.StatusConflict, Message: "this item is done; Approve is only for items awaiting approval"}
	rec := queueRequest(t, h, "POST", "/api/queue-items/item_a/approve", "", nil)
	if body := decodeBody[errorBody](t, rec); rec.Code != http.StatusConflict || !strings.Contains(body.Error, "this item is done") {
		t.Fatalf("conflict: %d %+v", rec.Code, body)
	}
}
