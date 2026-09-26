//go:build integration

// Package deploytest checks the real deploy config (docker-compose.yml,
// Dockerfile and the Caddy image) against the security checklist in
// AGENTS.md. `make test` renders the compose file with placeholder values
// into .cache/compose-config.json (scripts/compose-config.sh) and records
// facts about the built Caddy image in .cache/caddy/ (scripts/caddy-config.sh)
// first.
package deploytest

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

type port struct {
	HostIP    string `json:"host_ip"`
	Published string `json:"published"`
	Target    int    `json:"target"`
}

type volume struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

type build struct {
	Context    string `json:"context"`
	Dockerfile string `json:"dockerfile"`
}

type service struct {
	Build       *build             `json:"build"`
	Image       string             `json:"image"`
	User        string             `json:"user"`
	Ports       []port             `json:"ports"`
	Volumes     []volume           `json:"volumes"`
	Environment map[string]*string `json:"environment"`
}

type composeConfig struct {
	Services map[string]service `json:"services"`
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func load(t *testing.T) composeConfig {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), ".cache", "compose-config.json"))
	if err != nil {
		t.Fatalf("%v (run via `make test`, which renders it)", err)
	}
	var c composeConfig
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestHostbudPublishesNoPorts(t *testing.T) {
	c := load(t)
	app, ok := c.Services["hostbud"]
	if !ok {
		t.Fatal("no hostbud service")
	}
	if len(app.Ports) != 0 {
		t.Fatalf("hostbud publishes ports: %+v", app.Ports)
	}
}

func TestCaddyBindsLoopbackOnly(t *testing.T) {
	c := load(t)
	caddy, ok := c.Services["hostbud-caddy"]
	if !ok {
		t.Fatal("no hostbud-caddy service")
	}
	if len(caddy.Ports) == 0 {
		t.Fatal("hostbud-caddy publishes nothing")
	}
	for _, p := range caddy.Ports {
		// Never 0.0.0.0 or all interfaces. (M2 adds the ${TAILSCALE_IP} site.)
		if p.HostIP != "127.0.0.1" {
			t.Errorf("port %s bound to %q, want 127.0.0.1", p.Published, p.HostIP)
		}
	}
	for name, s := range c.Services {
		if name == "hostbud-caddy" || name == "hostbud-postgres" {
			continue
		}
		if len(s.Ports) != 0 {
			t.Errorf("service %s publishes ports", name)
		}
	}
}

func TestPostgresIsPrivateAndLoopbackOnly(t *testing.T) {
	c := load(t)
	db, ok := c.Services["hostbud-postgres"]
	if !ok {
		t.Fatal("no hostbud-postgres service")
	}
	if len(db.Ports) != 1 || db.Ports[0].HostIP != "127.0.0.1" || db.Ports[0].Target != 5432 {
		t.Fatalf("postgres ports = %+v; want one loopback-only 5432 mapping", db.Ports)
	}
	if len(db.Volumes) != 1 || db.Volumes[0].Target != "/var/lib/postgresql/data" || db.Volumes[0].Type != "volume" {
		t.Fatalf("postgres volumes = %+v; want one named data volume", db.Volumes)
	}
}

func TestHostbudRunsAsHostUser(t *testing.T) {
	c := load(t)
	if u := c.Services["hostbud"].User; u != "1000:1000" {
		t.Fatalf("hostbud user = %q, want ${HOST_UID}:${HOST_GID} (1000:1000 in the rendered config)", u)
	}
	if user := finalUser(t); user == "" || user == "root" || strings.HasPrefix(user, "0") {
		t.Fatalf("Dockerfile final stage USER = %q, want the non-root host uid", user)
	}
}

// finalUser returns the last USER instruction of the Dockerfile's last stage.
func finalUser(t *testing.T) string {
	t.Helper()
	f, err := os.Open(filepath.Join(repoRoot(), "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var user string
	from := regexp.MustCompile(`(?i)^FROM\s`)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case from.MatchString(line):
			user = ""
		case strings.HasPrefix(strings.ToUpper(line), "USER "):
			user = strings.TrimSpace(line[5:])
		}
	}
	return user
}

func TestHostbudMountsOnlyDataAgentAndPublicHostKeys(t *testing.T) {
	c := load(t)
	pub := regexp.MustCompile(`^/etc/ssh/ssh_host_[a-z0-9]+_key\.pub$`)
	var data, agent, keys int
	for _, v := range c.Services["hostbud"].Volumes {
		switch {
		case v.Type == "volume" && v.Target == "/data":
			data++
		case v.Type == "bind" && v.Target == "/run/ssh-agent.sock":
			agent++
		case v.Type == "bind" && pub.MatchString(v.Source) &&
			v.Target == "/run/host-keys/"+filepath.Base(v.Source):
			if !v.ReadOnly {
				t.Errorf("%s is not read-only", v.Source)
			}
			keys++
		default:
			t.Errorf("unexpected mount %+v (only the data volume, the agent socket and *.pub host keys)", v)
		}
	}
	if data != 1 || agent != 1 || keys == 0 {
		t.Fatalf("mounts: data=%d agent=%d host keys=%d", data, agent, keys)
	}
}

// readCache returns a file scripts/caddy-config.sh wrote into .cache/caddy.
func readCache(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), ".cache", "caddy", name)) //nolint:gosec // fixed file names from the tests
	if err != nil {
		t.Fatalf("%v (run via `make test`, which writes it)", err)
	}
	return string(data)
}

func TestCaddyIsTheCustomBuildWithCloudflareDNS(t *testing.T) {
	c := load(t)
	caddy := c.Services["hostbud-caddy"]
	if caddy.Build == nil || filepath.Base(caddy.Build.Context) != "caddy" ||
		filepath.Base(filepath.Dir(caddy.Build.Context)) != "deploy" || caddy.Build.Dockerfile != "Dockerfile" {
		t.Fatalf("hostbud-caddy build = %+v, want deploy/caddy/Dockerfile", caddy.Build)
	}
	if caddy.Image != "hostbud-caddy:local" {
		t.Errorf("hostbud-caddy image = %q, want hostbud-caddy:local", caddy.Image)
	}
	modules := strings.Fields(readCache(t, "modules.txt"))
	if !slices.Contains(modules, "dns.providers.cloudflare") {
		t.Fatal("the built Caddy image lacks dns.providers.cloudflare (caddy list-modules)")
	}
}

func TestCaddyMountsOnlyConfigAndCertVolumes(t *testing.T) {
	c := load(t)
	want := map[string]string{"/data": "hostbud-caddy-data", "/config": "hostbud-caddy-config"}
	var files int
	for _, v := range c.Services["hostbud-caddy"].Volumes {
		switch {
		case v.Type == "volume" && want[v.Target] == v.Source:
			delete(want, v.Target)
		case v.Type == "bind" && strings.HasPrefix(v.Target, "/etc/caddy/") &&
			filepath.Base(filepath.Dir(v.Source)) == "caddy" && v.Target == "/etc/caddy/"+filepath.Base(v.Source):
			if !v.ReadOnly {
				t.Errorf("%s is not read-only", v.Source)
			}
			files++
		default:
			t.Errorf("unexpected hostbud-caddy mount %+v", v)
		}
	}
	if files == 0 || len(want) != 0 {
		t.Fatalf("hostbud-caddy: %d config files mounted, missing volumes %v", files, want)
	}
}
