//go:build integration

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"

	"hostbud/internal/auth"
	"hostbud/internal/events"
	"hostbud/internal/fsbrowse"
	"hostbud/internal/inventory"
	"hostbud/internal/projects"
	"hostbud/internal/queue"
	"hostbud/internal/session"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
	"hostbud/internal/term"
	"hostbud/internal/testenv"
)

type permissiveTestAuthRepository struct{ store.AuthRepository }

func (permissiveTestAuthRepository) EmailAllowed(context.Context, string) (bool, error) {
	return true, nil
}

func TestIntegrationInfoLogsOmitCanariesAcrossAccountAndHostCycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))

	root := t.TempDir()
	schema := fmt.Sprintf("log_it_%x", sha256.Sum256([]byte(root)))[:22]
	dbHost := os.Getenv("HOSTBUD_TEST_DB_HOST")
	if dbHost == "" {
		dbHost = "hostbud-test-postgres"
	}
	dbPassword := os.Getenv("HOSTBUD_TEST_DB_PASSWORD")
	if dbPassword == "" {
		dbPassword = "hostbud-test-password" //nolint:gosec // disposable integration database
	}
	repository, err := store.Open(ctx, store.Config{Host: dbHost, Port: 5432, Name: "hostbud_test", User: "hostbud_test", Password: dbPassword, SSLMode: "disable", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	if _, err := repository.EnsureHostMachine(ctx, "Host machine"); err != nil {
		t.Fatal(err)
	}

	c := testenv.Connected(t, testenv.SSHD)
	runID := fmt.Sprintf("%x", time.Now().UnixNano())
	projectPath := "/home/dev/canary-path-7f3e-" + runID
	if _, err := c.Exec(ctx, sshx.HostMachineID, "mkdir", "-p", projectPath); err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus()
	inv := inventory.New(c, bus, inventory.Options{MachineID: sshx.HostMachineID, Label: "test", Interval: 250 * time.Millisecond})
	inventoryCtx, stopInventory := context.WithCancel(ctx)
	inventoryDone := make(chan struct{})
	go func() { inv.Run(inventoryCtx); close(inventoryDone) }()
	defer func() { stopInventory(); <-inventoryDone }()
	if err := inv.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	authService, err := auth.New(permissiveTestAuthRepository{AuthRepository: repository}, auth.Config{
		Key: []byte("test-only-auth-key-for-log-check"), Log: logger,
		Params: auth.Params{Memory: 64, Time: 1, Threads: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	projectService := projects.New(repository, bus, logger)
	sessions := session.New(c, map[string]session.Tracker{sshx.HostMachineID: inv}, logger, projectService)
	projectService.SetSessionCreator(sessions)
	filesystem := fsbrowse.New(c, sshx.HostMachineID, time.Minute, 5*time.Second)
	defer func() { _ = filesystem.Close() }()
	terminal := &term.Handler{SSH: c, Log: logger}
	handler := New(Config{
		Log: logger, Dist: fstest.MapFS{}, Origins: AllowedOrigins("", 9055), Bus: bus,
		Machines: []Snapshotter{inv}, Sessions: sessions, Projects: projectService,
		FileSystem: filesystem, Terminal: terminal, Auth: authService,
		Hooks: queue.NewHooks(repository, nil, logger),
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	const email = "canary-email@example.com"
	const password = "canary-password-for-log-check"
	projectName := "canary-project-name-" + runID
	sessionName := "canary-session-name-" + runID
	renamedName := "canary-session-renamed-" + runID
	folderName := "canary-folder-name-" + runID
	const startCommand = "printf canary-cmd-7f3e; sleep 120"
	t.Cleanup(func() {
		_, _ = c.Exec(context.Background(), sshx.HostMachineID, "tmux", "kill-session", "-t", "="+sessionName)
		_, _ = c.Exec(context.Background(), sshx.HostMachineID, "tmux", "kill-session", "-t", "="+renamedName)
		_, _ = c.Exec(context.Background(), sshx.HostMachineID, "rm", "-rf", projectPath)
	})

	type apiResponse struct {
		status  int
		cookies []*http.Cookie
	}
	request := func(method, path string, payload any, cookie string) (apiResponse, []byte) {
		t.Helper()
		var body io.Reader
		if payload != nil {
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			body = bytes.NewReader(encoded)
		}
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
		}
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return apiResponse{status: response.StatusCode, cookies: response.Cookies()}, data
	}
	wantStatus := func(response apiResponse, body []byte, want int, action string) {
		t.Helper()
		if response.status != want {
			t.Fatalf("%s: status=%d want=%d body=%s", action, response.status, want, body)
		}
	}

	response, body := request(http.MethodPost, "/api/auth/register", map[string]string{"email": email, "password": password}, "")
	wantStatus(response, body, http.StatusCreated, "register")
	response, body = request(http.MethodPost, "/api/auth/login", map[string]string{"email": email, "password": password}, "")
	wantStatus(response, body, http.StatusOK, "sign in")
	var cookieToken string
	for _, cookie := range response.cookies {
		if cookie.Name == SessionCookie {
			cookieToken = cookie.Value
		}
	}
	if cookieToken == "" {
		t.Fatal("sign in did not return an auth cookie")
	}

	response, body = request(http.MethodPost, "/api/projects", map[string]string{"machineId": sshx.HostMachineID, "path": projectPath, "name": projectName}, cookieToken)
	wantStatus(response, body, http.StatusCreated, "create project")
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.ID == "" {
		t.Fatalf("create project response = %s (%v)", body, err)
	}
	response, body = request(http.MethodPost, "/api/projects/"+url.PathEscape(created.ID)+"/sessions",
		map[string]string{"name": sessionName, "startCommand": startCommand}, cookieToken)
	wantStatus(response, body, http.StatusCreated, "create session")
	if err := inv.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	response, body = request(http.MethodGet, "/api/machines/host/sessions", nil, cookieToken)
	wantStatus(response, body, http.StatusOK, "list sessions")
	if !bytes.Contains(body, []byte(sessionName)) {
		t.Fatalf("session list omitted created session: %s", body)
	}
	response, body = request(http.MethodPatch, "/api/machines/host/sessions/"+url.PathEscape(sessionName), map[string]string{"name": renamedName}, cookieToken)
	wantStatus(response, body, http.StatusOK, "rename session")

	response, body = request(http.MethodPost, "/api/machines/host/fs/mkdir", map[string]string{"path": projectPath, "name": folderName}, cookieToken)
	wantStatus(response, body, http.StatusCreated, "create folder")
	response, body = request(http.MethodGet, "/api/machines/host/fs?path="+url.QueryEscape(projectPath), nil, cookieToken)
	wantStatus(response, body, http.StatusOK, "browse project")
	if !bytes.Contains(body, []byte(folderName)) {
		t.Fatalf("directory listing omitted created folder: %s", body)
	}

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/term?machine=host&session=" + url.QueryEscape(renamedName) + "&cols=80&rows=24"
	ws, wsResponse, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{
		"Origin": {origin}, "Cookie": {SessionCookie + "=" + cookieToken},
	}})
	if wsResponse != nil && wsResponse.Body != nil {
		_ = wsResponse.Body.Close()
	}
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for terminal.Active() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if terminal.Active() == 0 {
		t.Fatal("terminal attach did not start")
	}
	response, body = request(http.MethodDelete, "/api/machines/host/sessions/"+url.PathEscape(renamedName), nil, cookieToken)
	wantStatus(response, body, http.StatusNoContent, "kill session")
	_ = ws.CloseNow()
	deadline = time.Now().Add(5 * time.Second)
	for terminal.Active() != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if terminal.Active() != 0 {
		t.Fatal("terminal attach did not stop after session kill")
	}
	// V2-M1 T3: run hooks. Tokens and hook bodies never reach info logs,
	// also for refused hooks (bad token, rate limit).
	queueRow, err := repository.CreateQueue(ctx, created.ID, "canary-queue-"+runID)
	if err != nil {
		t.Fatal(err)
	}
	item, err := repository.AddQueueItem(ctx, queueRow.ID, "claude", "--canary-flag", "/goal canary-goal-"+runID)
	if err != nil {
		t.Fatal(err)
	}
	runToken, tokenHash, err := queue.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	run, err := repository.CreateRun(ctx, item.ID, tokenHash, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	hookBody := `{"session_id":"canary-agent-session-` + runID + `","transcript_path":"/home/dev/.claude/projects/canary-transcript-` + runID + `.jsonl"}`
	hook := func(token string) int {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/hooks/"+run.ID+"/turn_end", strings.NewReader(hookBody))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		return response.StatusCode
	}
	if got := hook(runToken); got != http.StatusNoContent {
		t.Fatalf("hook with the run token = %d", got)
	}
	wrongToken, _, _ := queue.NewToken()
	if got := hook(wrongToken); got != http.StatusUnauthorized {
		t.Fatalf("hook with another token = %d", got)
	}
	for range queue.HookBurst {
		_ = hook(runToken)
	}
	if !strings.Contains(logs.String(), `"reason":"rate_limited"`) || !strings.Contains(logs.String(), `"reason":"bad_token"`) {
		t.Fatalf("refused hooks aren't logged: %s", logs.String())
	}
	if events, err := repository.RunEvents(ctx, run.ID, 50); err != nil || len(events) == 0 || string(events[len(events)-1].Payload) != hookBody {
		t.Fatalf("hook event not recorded verbatim: %v, %v", events, err)
	}
	if err := repository.DeleteQueue(ctx, queueRow.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("delete queue with an active run: %v", err)
	}

	response, body = request(http.MethodPost, "/api/auth/logout", nil, cookieToken)
	wantStatus(response, body, http.StatusNoContent, "sign out")

	for _, canary := range []string{email, password, cookieToken, projectPath, projectName, sessionName, renamedName, startCommand, folderName,
		runToken, wrongToken, "canary-agent-session-" + runID, "canary-transcript-" + runID, "canary-goal-" + runID, "--canary-flag"} {
		if strings.Contains(logs.String(), canary) {
			t.Fatalf("info logs contain canary %q: %s", canary, logs.String())
		}
	}
}
