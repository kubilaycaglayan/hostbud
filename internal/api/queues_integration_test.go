//go:build integration

package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"hostbud/internal/agents"
	"hostbud/internal/events"
	"hostbud/internal/queue"
	"hostbud/internal/store"
)

// V2-M1 T8: the queue routes against the real store: CRUD, reorder,
// start/pause, the one-queue limit, and the Origin check on every
// state-changing queue route (no side effect).
func TestIntegrationQueueRoutesUsePostgres(t *testing.T) {
	ctx := context.Background()
	host := os.Getenv("HOSTBUD_TEST_DB_HOST")
	if host == "" {
		host = "hostbud-test-postgres"
	}
	password := os.Getenv("HOSTBUD_TEST_DB_PASSWORD")
	if password == "" {
		password = "hostbud-test-password" //nolint:gosec // disposable integration database
	}
	schema := fmt.Sprintf("queue_api_%x", sha256.Sum256([]byte(t.TempDir())))[:24]
	repo, err := store.Open(ctx, store.Config{Host: host, Port: 5432, Name: "hostbud_test", User: "hostbud_test", Password: password, SSLMode: "disable", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repo.Close() }()
	if _, err := repo.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}
	project, err := repo.CreateProject(ctx, store.HostMachineID, "/home/dev/app", "app")
	if err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus()
	changes, cancel := bus.Subscribe(100)
	defer cancel()
	svc := queue.NewService(repo, agents.NewRegistry(agents.NewClaude(nil, nil, nil), agents.NewCodex(nil, 0)), bus)
	server := httptest.NewServer(New(Config{
		Log: slog.New(slog.DiscardHandler), Dist: fstest.MapFS{}, Origins: AllowedOrigins("", 9055), Bus: bus, Auth: &fakeAuth{}, Queues: svc,
	}))
	defer server.Close()

	call := func(method, path, body, originHeader string) (int, []byte) {
		t.Helper()
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if originHeader != "" {
			req.Header.Set("Origin", originHeader)
		}
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testToken})
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		data, _ := io.ReadAll(res.Body)
		return res.StatusCode, data
	}
	want := func(status int, got int, body []byte, what string) {
		t.Helper()
		if got != status {
			t.Fatalf("%s: %d %s", what, got, body)
		}
	}

	status, body := call("POST", "/api/queues", `{"projectId":"`+project.ID+`","name":"Milestones"}`, origin)
	want(201, status, body, "create queue")
	var q queue.View
	_ = json.Unmarshal(body, &q)
	var ids []string
	for _, instr := range []string{"work on milestone 1", "/goal m2", "/goal m3"} {
		status, body = call("POST", "/api/queues/"+q.ID+"/items", `{"agent":"claude","flags":"--model 'opus 4'","instruction":"`+instr+`"}`, origin)
		want(201, status, body, "add item")
		var it queue.ItemView
		_ = json.Unmarshal(body, &it)
		ids = append(ids, it.ID)
	}
	status, body = call("POST", "/api/queues/"+q.ID+"/items", `{"agent":"claude","instruction":""}`, origin)
	want(400, status, body, "invalid instruction")
	status, body = call("PUT", "/api/queues/"+q.ID+"/order", `{"itemIds":["`+ids[2]+`","`+ids[0]+`","`+ids[1]+`"]}`, origin)
	want(200, status, body, "reorder")
	status, body = call("PATCH", "/api/queue-items/"+ids[0], `{"instruction":"/goal m1 and docs"}`, origin)
	want(200, status, body, "edit")
	status, body = call("DELETE", "/api/queue-items/"+ids[1], "", origin)
	want(204, status, body, "delete item")
	status, body = call("GET", "/api/queue-history?limit=200&offset=0", "", "")
	want(200, status, body, "queue history")
	var history struct {
		Items []struct {
			QueueID     string `json:"queueId"`
			Instruction string `json:"instruction"`
			Action      string `json:"action"`
			Status      string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &history); err != nil {
		t.Fatal(err)
	}
	seenEdited, seenDeleted := false, false
	for _, row := range history.Items {
		if row.QueueID == q.ID && row.Instruction == "/goal m1 and docs" && row.Action == "edited" {
			seenEdited = true
		}
		if row.QueueID == q.ID && row.Action == "deleted" && row.Status == "queued" {
			seenDeleted = true
		}
	}
	if !seenEdited || !seenDeleted {
		t.Fatalf("history missing edit/delete snapshots: %s", body)
	}
	status, body = call("GET", "/api/queues", "", "")
	want(200, status, body, "list")
	var list struct{ Queues []queue.View }
	_ = json.Unmarshal(body, &list)
	if len(list.Queues) != 1 || len(list.Queues[0].Items) != 2 || list.Queues[0].Items[0].ID != ids[2] || list.Queues[0].Items[1].Instruction != "/goal m1 and docs" ||
		list.Queues[0].Items[0].Position != 1 || list.Queues[0].Items[1].Position != 2 {
		t.Fatalf("list after edits: %s", body)
	}

	// Every state-changing queue route refuses a foreign or missing Origin
	// and changes nothing.
	_, before := call("GET", "/api/queues", "", "")
	for _, r := range []struct{ method, path, body string }{
		{"POST", "/api/queues", `{"projectId":"` + project.ID + `","name":"x"}`},
		{"PATCH", "/api/queues/" + q.ID, `{"name":"x"}`},
		{"DELETE", "/api/queues/" + q.ID, ""},
		{"POST", "/api/queues/" + q.ID + "/items", `{"agent":"claude","instruction":"/goal x"}`},
		{"PUT", "/api/queues/" + q.ID + "/order", `{"itemIds":["` + ids[0] + `","` + ids[2] + `"]}`},
		{"PUT", "/api/queues/" + q.ID + "/loop", `{"enabled":true,"maxRuntime":"2h"}`},
		{"PUT", "/api/queues/" + q.ID + "/default-prompt", `{"enabled":true,"text":", commit regularly."}`},
		{"PATCH", "/api/queue-items/" + ids[0], `{"instruction":"/goal x"}`},
		{"DELETE", "/api/queue-items/" + ids[0], ""},
		{"POST", "/api/queues/" + q.ID + "/start", ""},
		{"POST", "/api/queues/" + q.ID + "/pause", ""},
		{"POST", "/api/queues/" + q.ID + "/resume", ""},
		{"POST", "/api/queue-items/" + ids[0] + "/retry", ""},
		{"POST", "/api/queue-items/" + ids[0] + "/skip", ""},
		{"POST", "/api/queue-items/" + ids[0] + "/mark-done", ""},
	} {
		for _, o := range []string{"http://evil.example.com", ""} {
			if status, body := call(r.method, r.path, r.body, o); status != http.StatusForbidden {
				t.Errorf("%s %s Origin %q: %d %s", r.method, r.path, o, status, body)
			}
		}
	}
	if _, after := call("GET", "/api/queues", "", ""); string(after) != string(before) {
		t.Fatalf("a refused request changed the queue:\n%s\n%s", before, after)
	}

	status, body = call("PUT", "/api/queues/"+q.ID+"/loop", `{"enabled":true,"maxRuntime":"2h"}`, origin)
	want(200, status, body, "loop on")
	var looped queue.View
	_ = json.Unmarshal(body, &looped)
	if looped.Loop == nil || !looped.Loop.Enabled || looped.Loop.MaxRuntimeSeconds != 7200 {
		t.Fatalf("loop on: %s", body)
	}
	status, body = call("PUT", "/api/queues/"+q.ID+"/loop", `{"enabled":false}`, origin)
	want(200, status, body, "loop off")
	if strings.Contains(string(body), `"loop"`) {
		t.Fatalf("loop off still shows loop settings: %s", body)
	}
	// The queue's default prompt: absent (off, ", commit regularly.") until
	// set, stored in PostgreSQL, refused on two lines.
	if strings.Contains(string(body), `"defaultPrompt"`) {
		t.Fatalf("a new queue shows a default prompt: %s", body)
	}
	status, body = call("PUT", "/api/queues/"+q.ID+"/default-prompt", `{"enabled":true,"text":"a\nb"}`, origin)
	want(400, status, body, "two-line default prompt")
	status, body = call("PUT", "/api/queues/"+q.ID+"/default-prompt", `{"enabled":true,"text":", commit regularly."}`, origin)
	want(200, status, body, "default prompt on")
	if !strings.Contains(string(body), `"defaultPrompt":{"enabled":true,"text":", commit regularly."}`) {
		t.Fatalf("default prompt on: %s", body)
	}
	if _, listed := call("GET", "/api/queues", "", ""); !strings.Contains(string(listed), `"defaultPrompt":{"enabled":true,"text":", commit regularly."}`) {
		t.Fatalf("GET /api/queues after default prompt on: %s", listed)
	}
	status, body = call("POST", "/api/queues/"+q.ID+"/start", "", origin)
	want(200, status, body, "start")
	status, body = call("POST", "/api/queues/"+q.ID+"/start", "", origin)
	want(409, status, body, "start twice")
	// Parallel queues off: a second queue can be created to organize work,
	// but it can't start while Milestones runs.
	status, body = call("POST", "/api/queues", `{"projectId":"`+project.ID+`","name":"Second"}`, origin)
	want(201, status, body, "second queue")
	var second queue.View
	_ = json.Unmarshal(body, &second)
	status, body = call("POST", "/api/queues/"+second.ID+"/items", `{"agent":"claude","instruction":"/goal s1"}`, origin)
	want(201, status, body, "second queue item")
	status, body = call("POST", "/api/queues/"+second.ID+"/start", "", origin)
	want(409, status, body, "start second queue")
	if !strings.Contains(string(body), "pause queue Milestones") {
		t.Fatalf("start second queue message: %s", body)
	}
	status, body = call("DELETE", "/api/queues/"+second.ID, "", origin)
	want(204, status, body, "delete second queue")
	status, body = call("POST", "/api/queues/"+q.ID+"/pause", "", origin)
	want(200, status, body, "pause")
	status, body = call("POST", "/api/queue-items/"+ids[0]+"/skip", "", origin)
	want(409, status, body, "skip a queued item")
	status, body = call("POST", "/api/queues/"+q.ID+"/resume", "", origin)
	want(200, status, body, "resume")

	n := 0
	for len(changes) > 0 {
		if e := <-changes; e.Type == events.QueueChanged {
			n++
		}
	}
	// 13 changes on Milestones (one sets its default prompt), 3 on Second,
	// each also heard by its peer in the same directory (3 more).
	if n != 19 {
		t.Fatalf("queue.changed events: %d, want one per change plus peers (19)", n)
	}
}
