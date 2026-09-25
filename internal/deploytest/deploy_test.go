//go:build integration

// Package deploytest checks the real deploy config (docker-compose.yml and
// Dockerfile) against the security checklist in AGENTS.md. `make test`
// renders the compose file with placeholder values into
// .cache/compose-config.json first (scripts/compose-config.sh).
package deploytest

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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

type service struct {
	User    string   `json:"user"`
	Ports   []port   `json:"ports"`
	Volumes []volume `json:"volumes"`
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
		if name == "hostbud-caddy" {
			continue
		}
		if len(s.Ports) != 0 {
			t.Errorf("service %s publishes ports", name)
		}
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
