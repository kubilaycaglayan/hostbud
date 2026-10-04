//go:build integration

package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"hostbud/internal/events"
	"hostbud/internal/inventory"
	"hostbud/internal/machines"
	"hostbud/internal/projects"
	"hostbud/internal/session"
	"hostbud/internal/sshx"
	"hostbud/internal/store"
	"hostbud/internal/testenv"
)

// registryAdapter is cmd/hostbud's adapter, for the tests.
type registryAdapter struct{ *machines.Registry }

func (a registryAdapter) Snapshotters() []Snapshotter {
	var out []Snapshotter
	for _, inv := range a.Inventories() {
		out = append(out, inv)
	}
	return out
}

func (a registryAdapter) FileSystemFor(id string) (FileBrowser, bool) {
	fs, ok := a.FileSystem(id)
	if !ok {
		return nil, false
	}
	return fs, true
}

func (a registryAdapter) Tracker(id string) (session.Tracker, bool) {
	inv, ok := a.Inventory(id)
	if !ok {
		return nil, false
	}
	return inv, true
}

func openServersStore(t *testing.T) *store.Store {
	t.Helper()
	schema := fmt.Sprintf("servers_api_it_%x", sha256.Sum256([]byte(t.TempDir())))[:24]
	host := os.Getenv("HOSTBUD_TEST_DB_HOST")
	if host == "" {
		host = "hostbud-test-postgres"
	}
	password := os.Getenv("HOSTBUD_TEST_DB_PASSWORD")
	if password == "" {
		password = "hostbud-test-password" //nolint:gosec // disposable integration database
	}
	repo, err := store.Open(t.Context(), store.Config{Host: host, Port: 5432, Name: "hostbud_test", User: "hostbud_test", Password: password, SSLMode: "disable", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	if _, err := repo.EnsureHostMachine(t.Context(), "Host machine"); err != nil {
		t.Fatal(err)
	}
	return repo
}

func startRegistry(t *testing.T, client *sshx.Client, repo *store.Store, bus *events.Bus) *machines.Registry {
	t.Helper()
	reg := machines.New(client, repo, bus, machines.Options{HostLabel: "Host machine", PollInterval: 200 * time.Millisecond})
	if err := reg.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	reg.Start(ctx)
	t.Cleanup(func() { cancel(); reg.Wait(); _ = reg.Close() })
	return reg
}

func waitStatus(t *testing.T, e *env, id string, want inventory.Status) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		rec := e.do(t, http.MethodGet, "/api/machines", "", nil)
		var body struct{ Machines []inventory.Machine }
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		for _, m := range body.Machines {
			if m.ID == id && m.Status == want {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never reached %s: %s", id, want, rec.Body)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func addServer(t *testing.T, e *env, label, host string) string {
	t.Helper()
	rec := e.do(t, http.MethodPost, "/api/machines/scan", `{"host":"`+host+`","port":22}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("scan %s = %d %s", host, rec.Code, rec.Body)
	}
	var scan struct {
		HostKeys []sshx.HostKey `json:"hostKeys"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &scan)
	keys, _ := json.Marshal(scan.HostKeys)
	// The client sends back type and key only; the fingerprint is recomputed.
	body := `{"label":"` + label + `","host":"` + host + `","port":22,"user":"dev","hostKeys":` +
		strings.ReplaceAll(string(keys), `"fingerprint"`, `"type2"`) + `}`
	if rec := e.do(t, http.MethodPost, "/api/machines", body, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown key field accepted: %d", rec.Code)
	}
	var plain []map[string]string
	for _, k := range scan.HostKeys {
		plain = append(plain, map[string]string{"type": k.Type, "key": k.Key})
	}
	keys, _ = json.Marshal(plain)
	rec = e.do(t, http.MethodPost, "/api/machines", `{"label":"`+label+`","host":"`+host+`","port":22,"user":"dev","hostKeys":`+string(keys)+`}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add %s = %d %s", host, rec.Code, rec.Body)
	}
	var m inventory.Machine
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return m.ID
}

func TestIntegrationServersAddUseAndRemove(t *testing.T) {
	client := testenv.Connected(t, testenv.SSHD)
	repo := openServersStore(t)
	bus := events.NewBus()
	reg := startRegistry(t, client, repo, bus)
	projectService := projects.New(repo, bus, nil)
	sessions := session.New(client, registryAdapter{reg}, nil, projectService)
	e := &env{}
	e.h = New(Config{
		Dist: fstest.MapFS{}, Origins: AllowedOrigins("", 9055), Bus: bus, Auth: &fakeAuth{},
		Registry: registryAdapter{reg}, Sessions: sessions, Projects: projectService,
	})

	notmux := addServer(t, e, "No tmux", testenv.SSHDNoTmux)
	waitStatus(t, e, notmux, inventory.StatusTmuxMissing)
	if rec := e.do(t, http.MethodPost, "/api/machines", `{"label":"no TMUX","host":"server-a","user":"dev","hostKeys":[]}`, nil); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate nickname = %d %s", rec.Code, rec.Body)
	}

	// The tmux target again, as a server: a session created through the
	// server's id lands on it.
	second := addServer(t, e, "Second", testenv.SSHD)
	waitStatus(t, e, second, inventory.StatusOK)
	const name = "srv-it-session"
	t.Cleanup(func() {
		_, _ = client.Exec(context.Background(), sshx.HostMachineID, "tmux", "kill-session", "-t", name)
	})
	if rec := e.do(t, http.MethodPost, "/api/machines/"+second+"/sessions", `{"name":"`+name+`","path":"~"}`, nil); rec.Code != http.StatusCreated {
		t.Fatalf("create on server = %d %s", rec.Code, rec.Body)
	}
	if _, err := client.Exec(t.Context(), second, "tmux", "has-session", "-t", name); err != nil {
		t.Fatalf("session missing on the server: %v", err)
	}
	if rec := e.do(t, http.MethodGet, "/api/machines/"+second+"/fs/home", "", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "/home/dev") {
		t.Fatalf("server home = %d %s", rec.Code, rec.Body)
	}

	// A project keeps the server; removal waits until it's gone.
	rec := e.do(t, http.MethodPost, "/api/projects", `{"machineId":"`+second+`","path":"/home/dev"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("project on server = %d %s", rec.Code, rec.Body)
	}
	var project struct {
		ID        string `json:"id"`
		MachineID string `json:"machineId"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &project)
	if project.MachineID != second {
		t.Fatalf("project = %+v", project)
	}
	if rec := e.do(t, http.MethodDelete, "/api/machines/"+second, "", nil); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "projects") {
		t.Fatalf("remove in use = %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(t, http.MethodDelete, "/api/projects/"+project.ID, "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete project = %d", rec.Code)
	}
	if rec := e.do(t, http.MethodDelete, "/api/machines/"+second, "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("remove = %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(t, http.MethodGet, "/api/machines/"+second+"/sessions", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("removed server still routed: %d", rec.Code)
	}
	// Its tmux session keeps running: removal never touches tmux.
	if _, err := client.Exec(t.Context(), sshx.HostMachineID, "tmux", "has-session", "-t", name); err != nil {
		t.Fatalf("removal touched tmux: %v", err)
	}
	if rec := e.do(t, http.MethodDelete, "/api/machines/host", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("host removal = %d", rec.Code)
	}

	// After a restart the remaining server is loaded and pinned again.
	reloaded := machines.New(client, repo, events.NewBus(), machines.Options{HostLabel: "Host machine"})
	if err := reloaded.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Inventory(notmux); !ok {
		t.Fatal("stored server not reloaded")
	}
	if _, ok := reloaded.Inventory(second); ok {
		t.Fatal("removed server reloaded")
	}
	conf, _ := os.ReadFile(client.ConfigPath())
	if !strings.Contains(string(conf), sshx.TargetAliasPrefix+notmux) || strings.Contains(string(conf), sshx.TargetAliasPrefix+second) {
		t.Fatalf("config after reload:\n%s", conf)
	}
}
