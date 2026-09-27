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
	"fmt"
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
	Build       *build                `json:"build"`
	Image       string                `json:"image"`
	User        string                `json:"user"`
	Ports       []port                `json:"ports"`
	Volumes     []volume              `json:"volumes"`
	Environment map[string]*string    `json:"environment"`
	ReadOnly    bool                  `json:"read_only"`
	Tmpfs       []string              `json:"tmpfs"`
	CapDrop     []string              `json:"cap_drop"`
	CapAdd      []string              `json:"cap_add"`
	SecurityOpt []string              `json:"security_opt"`
	PidsLimit   int64                 `json:"pids_limit"`
	ShmSize     string                `json:"shm_size"`
	Healthcheck *healthcheck          `json:"healthcheck"`
	DependsOn   map[string]dependency `json:"depends_on"`
	Logging     logging               `json:"logging"`
}

type healthcheck struct {
	Test        []string `json:"test"`
	Interval    string   `json:"interval"`
	Timeout     string   `json:"timeout"`
	Retries     int      `json:"retries"`
	StartPeriod string   `json:"start_period"`
}
type dependency struct {
	Condition string `json:"condition"`
}
type logging struct {
	Driver  string            `json:"driver"`
	Options map[string]string `json:"options"`
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

func loadE2E(t *testing.T) composeConfig {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), ".cache", "e2e-compose-config.json"))
	if err != nil {
		t.Fatalf("%v (run via `make test`, which renders both Compose files)", err)
	}
	var c composeConfig
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func loadTailscale(t *testing.T) composeConfig {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), ".cache", "compose-ts-config.json"))
	if err != nil {
		t.Fatalf("%v (run via `make test`, which renders the Tailscale override)", err)
	}
	var c composeConfig
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTailscaleOverrideMountIsOptionalAndReadOnly(t *testing.T) {
	base, with := load(t), loadTailscale(t)
	if got, ok := env(base.Services["hostbud"], "HOSTBUD_ALLOWED_TS_USERS"); ok && got != "" {
		t.Fatal("default compose enables Tailscale allowlist")
	}
	if len(base.Services["hostbud"].Volumes) != len(with.Services["hostbud"].Volumes)-1 {
		t.Fatalf("override mounts: base=%+v with=%+v", base.Services["hostbud"].Volumes, with.Services["hostbud"].Volumes)
	}
	if got, ok := env(with.Services["hostbud"], "HOSTBUD_ALLOWED_TS_USERS"); !ok || got != "owner@example.com" {
		t.Fatal("override did not pass allowlist")
	}
	found := false
	for _, v := range with.Services["hostbud"].Volumes {
		if v.Target == "/run/tailscale/tailscaled.sock" {
			found = v.Type == "bind" && v.ReadOnly && v.Source == "/run/tailscale/tailscaled.sock"
		}
	}
	if !found {
		t.Fatal("override socket bind is missing, writable, or has unexpected source")
	}
}

func TestCaddyMarksDomainAndLoopbackProxyPaths(t *testing.T) {
	raw, _ := adapted(t, "adapt.json")
	if strings.Count(raw, "X-Hostbud-Via") != 2 || !strings.Contains(raw, `"X-Hostbud-Via":["domain"]`) || !strings.Contains(raw, `"X-Hostbud-Via":["local"]`) {
		t.Fatalf("Caddy must overwrite X-Hostbud-Via for both sites, adapted config: %s", raw)
	}
}

func TestContainerHardeningAndE2EParity(t *testing.T) {
	production := load(t)
	e2e := loadE2E(t)
	app := production.Services["hostbud"]
	caddy := production.Services["hostbud-caddy"]
	postgres := production.Services["hostbud-postgres"]
	assertAppHardening(t, "hostbud", app)
	assertCaddyHardening(t, "hostbud-caddy", caddy)
	assertPostgresHardening(t, "hostbud-postgres", postgres)

	for _, name := range []string{"hostbud-e2e-app", "hostbud-e2e-app-notmux", "hostbud-e2e-app-ts"} {
		actual := e2e.Services[name]
		assertAppHardening(t, name, actual)
		if app.ReadOnly != actual.ReadOnly || !slices.Equal(app.Tmpfs, actual.Tmpfs) ||
			!slices.Equal(app.CapDrop, actual.CapDrop) || !slices.Equal(app.CapAdd, actual.CapAdd) ||
			!slices.Equal(app.SecurityOpt, actual.SecurityOpt) || app.PidsLimit != actual.PidsLimit ||
			!sameHealthcheck(app.Healthcheck, actual.Healthcheck) || app.Logging.Driver != actual.Logging.Driver || !mapsEqual(app.Logging.Options, actual.Logging.Options) {
			t.Errorf("%s doesn't mirror production app hardening", name)
		}
	}
	e2eCaddy := e2e.Services["hostbud-e2e-caddy"]
	assertCaddyHardening(t, "hostbud-e2e-caddy", e2eCaddy)
	if caddy.ReadOnly != e2eCaddy.ReadOnly || !slices.Equal(caddy.Tmpfs, e2eCaddy.Tmpfs) ||
		!slices.Equal(caddy.CapDrop, e2eCaddy.CapDrop) || !slices.Equal(caddy.CapAdd, e2eCaddy.CapAdd) ||
		!slices.Equal(caddy.SecurityOpt, e2eCaddy.SecurityOpt) || caddy.Logging.Driver != e2eCaddy.Logging.Driver || !mapsEqual(caddy.Logging.Options, e2eCaddy.Logging.Options) {
		t.Error("hostbud-e2e-caddy doesn't mirror production Caddy hardening")
	}
	e2ePostgres := e2e.Services["hostbud-e2e-postgres"]
	assertPostgresHardening(t, "hostbud-e2e-postgres", e2ePostgres)
	if !slices.Equal(postgres.CapDrop, e2ePostgres.CapDrop) || !slices.Equal(postgres.CapAdd, e2ePostgres.CapAdd) ||
		!slices.Equal(postgres.SecurityOpt, e2ePostgres.SecurityOpt) || postgres.ShmSize != e2ePostgres.ShmSize ||
		postgres.Logging.Driver != e2ePostgres.Logging.Driver || !mapsEqual(postgres.Logging.Options, e2ePostgres.Logging.Options) {
		t.Error("hostbud-e2e-postgres doesn't mirror production Postgres hardening")
	}
}

func TestProductionServicesHaveNoDangerousHostMounts(t *testing.T) {
	c := load(t)
	for name, service := range c.Services {
		for _, mount := range service.Volumes {
			value := strings.ToLower(mount.Source + " " + mount.Target)
			if strings.Contains(value, "docker.sock") || strings.Contains(value, ".ssh") ||
				strings.Contains(value, "private_key") || strings.HasSuffix(strings.ToLower(mount.Source), ".pem") ||
				(strings.Contains(value, "id_ed25519") && !strings.HasSuffix(strings.ToLower(mount.Source), ".pub")) ||
				(strings.Contains(value, "id_rsa") && !strings.HasSuffix(strings.ToLower(mount.Source), ".pub")) {
				t.Errorf("%s mounts a Docker socket, SSH directory or private key path: %+v", name, mount)
			}
		}
	}
}

func TestPostgresMinimalCapabilitiesStartedOnFreshVolume(t *testing.T) {
	marker, err := os.ReadFile(filepath.Join(repoRoot(), ".cache", "postgres-capabilities.ok"))
	if err != nil {
		t.Fatalf("fresh-volume Postgres capability check did not run (run via `make test`): %v", err)
	}
	if strings.TrimSpace(string(marker)) != "verified" {
		t.Fatalf("fresh-volume Postgres capability check marker = %q, want verified", marker)
	}
}

func TestReadOnlyAppImageHealthAndSessionListPassed(t *testing.T) {
	marker, err := os.ReadFile(filepath.Join(repoRoot(), ".cache", "readonly-image.ok"))
	if err != nil {
		t.Fatalf("read-only image integration check didn't run (run via `make test`): %v", err)
	}
	if strings.TrimSpace(string(marker)) != "verified" {
		t.Fatalf("read-only image integration check marker = %q, want verified", marker)
	}
}

func sameHealthcheck(a, b *healthcheck) bool {
	if a == nil || b == nil {
		return a == b
	}
	return slices.Equal(a.Test, b.Test) && a.Interval == b.Interval && a.Timeout == b.Timeout && a.Retries == b.Retries && a.StartPeriod == b.StartPeriod
}

func assertAppHardening(t *testing.T, name string, s service) {
	t.Helper()
	if !s.ReadOnly || !slices.Equal(s.Tmpfs, []string{"/tmp"}) || !slices.Equal(s.CapDrop, []string{"ALL"}) || len(s.CapAdd) != 0 ||
		!slices.Equal(s.SecurityOpt, []string{"no-new-privileges:true"}) || s.PidsLimit != 1024 {
		t.Errorf("%s app hardening = read_only:%v tmpfs:%v cap_drop:%v cap_add:%v security_opt:%v pids_limit:%d", name, s.ReadOnly, s.Tmpfs, s.CapDrop, s.CapAdd, s.SecurityOpt, s.PidsLimit)
	}
	if s.Healthcheck == nil || !slices.Equal(s.Healthcheck.Test, []string{"CMD", "hostbud", "healthcheck"}) ||
		s.Healthcheck.Timeout != "2s" || s.Healthcheck.Interval != "10s" || s.Healthcheck.Retries != 3 {
		t.Errorf("%s healthcheck = %+v", name, s.Healthcheck)
	}
	assertLogging(t, name, s.Logging)
}

func assertCaddyHardening(t *testing.T, name string, s service) {
	t.Helper()
	if !s.ReadOnly || !slices.Equal(s.Tmpfs, []string{"/tmp"}) || !slices.Equal(s.CapDrop, []string{"ALL"}) ||
		!slices.Equal(s.CapAdd, []string{"NET_BIND_SERVICE"}) || !slices.Equal(s.SecurityOpt, []string{"no-new-privileges:true"}) {
		t.Errorf("%s Caddy hardening = read_only:%v tmpfs:%v cap_drop:%v cap_add:%v security_opt:%v", name, s.ReadOnly, s.Tmpfs, s.CapDrop, s.CapAdd, s.SecurityOpt)
	}
	if name == "hostbud-caddy" && s.DependsOn["hostbud"].Condition != "service_healthy" {
		t.Errorf("%s must wait for hostbud healthcheck, got %v", name, s.DependsOn)
	}
	if name == "hostbud-e2e-caddy" && (s.DependsOn["hostbud-e2e-app"].Condition != "service_healthy" || s.DependsOn["hostbud-e2e-app-notmux"].Condition != "service_healthy") {
		t.Errorf("%s must wait for both app healthchecks, got %v", name, s.DependsOn)
	}
	assertLogging(t, name, s.Logging)
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func assertPostgresHardening(t *testing.T, name string, s service) {
	t.Helper()
	if !slices.Equal(s.CapDrop, []string{"ALL"}) || !slices.Equal(s.CapAdd, []string{"DAC_OVERRIDE", "SETGID", "SETUID"}) ||
		!slices.Equal(s.SecurityOpt, []string{"no-new-privileges:true"}) || s.ShmSize != "134217728" {
		t.Errorf("%s Postgres hardening = cap_drop:%v cap_add:%v security_opt:%v shm_size:%s", name, s.CapDrop, s.CapAdd, s.SecurityOpt, s.ShmSize)
	}
	assertLogging(t, name, s.Logging)
}

func assertLogging(t *testing.T, name string, log logging) {
	t.Helper()
	if log.Driver != "json-file" || log.Options["max-size"] != "10m" || log.Options["max-file"] != "5" {
		t.Errorf("%s logging = %+v", name, log)
	}
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

func TestHardeningLimitsOnlyReachHostbud(t *testing.T) {
	c := load(t)
	want := map[string]string{
		"HOSTBUD_EXEC_TIMEOUT":           "10s",
		"HOSTBUD_SFTP_TIMEOUT":           "10s",
		"HOSTBUD_MAX_TERMINALS_PER_USER": "32",
		"HOSTBUD_MAX_TERMINALS":          "128",
	}
	app := c.Services["hostbud"]
	for key, value := range want {
		if got, ok := env(app, key); !ok || got != value {
			t.Errorf("hostbud %s = %q, present=%v; want %q", key, got, ok, value)
		}
		for name, svc := range c.Services {
			if name == "hostbud" {
				continue
			}
			if _, ok := env(svc, key); ok {
				t.Errorf("%s unexpectedly receives %s", name, key)
			}
		}
	}
}

// Placeholder values scripts/compose-config.sh and caddy-config.sh render with.
const (
	placeholderDomain = "hostbud.example.com"
	placeholderTSIP   = "100.64.0.1"
	placeholderToken  = "placeholder-cloudflare-token"
)

func TestCaddyPublishesOnlyLoopbackAndTailscale(t *testing.T) {
	c := load(t)
	caddy, ok := c.Services["hostbud-caddy"]
	if !ok {
		t.Fatal("no hostbud-caddy service")
	}
	// Never 0.0.0.0 or all interfaces: the loopback site for SSH port
	// forwards, and the TLS site on the Tailscale IP only.
	want := map[string]bool{
		"127.0.0.1:9055->9055":        true,
		placeholderTSIP + ":443->443": true,
		placeholderTSIP + ":80->80":   true,
	}
	for _, p := range caddy.Ports {
		key := fmt.Sprintf("%s:%s->%d", p.HostIP, p.Published, p.Target)
		if !want[key] {
			t.Errorf("unexpected hostbud-caddy port %s", key)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Errorf("hostbud-caddy doesn't publish %v", want)
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

func env(s service, key string) (string, bool) {
	v, ok := s.Environment[key]
	if !ok || v == nil {
		return "", false
	}
	return *v, true
}

func TestDomainAndTokenWiring(t *testing.T) {
	c := load(t)
	// The app needs the domain for its Origin allowlist; Caddy for its site.
	for _, name := range []string{"hostbud", "hostbud-caddy"} {
		if d, _ := env(c.Services[name], "HOSTBUD_DOMAIN"); d != placeholderDomain {
			t.Errorf("%s: HOSTBUD_DOMAIN = %q", name, d)
		}
	}
	// Only Caddy gets the Cloudflare token, as an env var.
	for name, s := range c.Services {
		tok, ok := env(s, "CLOUDFLARE_API_TOKEN")
		if name == "hostbud-caddy" {
			if tok != placeholderToken {
				t.Errorf("hostbud-caddy: CLOUDFLARE_API_TOKEN = %q", tok)
			}
			continue
		}
		if ok {
			t.Errorf("service %s receives CLOUDFLARE_API_TOKEN", name)
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

// adapted loads a Caddyfile adapted to JSON by scripts/caddy-config.sh.
func adapted(t *testing.T, name string) (raw string, cfg map[string]any) {
	t.Helper()
	raw = readCache(t, name)
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	return raw, cfg
}

// dig follows keys (string) and indexes (int) into decoded JSON.
func dig(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			a, _ := v.([]any)
			if k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

// upstreams lists the dial addresses of every reverse_proxy handler under v.
func upstreams(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		if x["handler"] == "reverse_proxy" {
			for _, u := range x["upstreams"].([]any) {
				out = append(out, dig(u, "dial").(string))
			}
		}
		for _, c := range x {
			out = append(out, upstreams(c)...)
		}
	case []any:
		for _, c := range x {
			out = append(out, upstreams(c)...)
		}
	}
	return out
}

func responseHeader(v any, name string) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		if x["handler"] == "headers" {
			if values, ok := dig(x, "response", "set", name).([]any); ok {
				for _, value := range values {
					out = append(out, fmt.Sprint(value))
				}
			}
		}
		for _, child := range x {
			out = append(out, responseHeader(child, name)...)
		}
	case []any:
		for _, child := range x {
			out = append(out, responseHeader(child, name)...)
		}
	}
	return out
}

func TestCaddyfileServesBothSites(t *testing.T) {
	raw, cfg := adapted(t, "adapt.json")
	if dig(cfg, "admin", "disabled") != true {
		t.Error("the Caddy admin API is enabled")
	}
	servers, _ := dig(cfg, "apps", "http", "servers").(map[string]any)
	var tlsSite, localSite bool
	for name, srv := range servers {
		listen := fmt.Sprint(dig(srv, "listen"))
		if p := fmt.Sprint(dig(srv, "protocols")); p != "[h1 h2]" {
			t.Errorf("%s: protocols %s, want h1 h2 (no HTTP/3: UDP isn't published)", name, p)
		}
		if ups := upstreams(srv); len(ups) == 0 || slices.ContainsFunc(ups, func(u string) bool { return u != "hostbud:8080" }) {
			t.Errorf("%s (%s) proxies to %v, want hostbud:8080", name, listen, ups)
		}
		switch listen {
		case "[:443]":
			tlsSite = fmt.Sprint(dig(srv, "routes", 0, "match", 0, "host")) == "["+placeholderDomain+"]"
			if got := responseHeader(srv, "Strict-Transport-Security"); fmt.Sprint(got) != "[max-age=31536000]" {
				t.Errorf("domain HSTS = %v, want max-age=31536000 only", got)
			}
		case "[:9055]":
			// Plain HTTP: no TLS on the loopback site.
			localSite = dig(srv, "tls_connection_policies") == nil
			if got := responseHeader(srv, "Strict-Transport-Security"); len(got) != 0 {
				t.Errorf("loopback site must not send HSTS, got %v", got)
			}
		default:
			t.Errorf("unexpected server %s listening on %s", name, listen)
		}
	}
	if !tlsSite || !localSite {
		t.Fatalf("sites: domain on :443 %v, plain-HTTP loopback on :9055 %v", tlsSite, localSite)
	}

	// The domain's certificate: ACME DNS-01 via Cloudflare, token from env.
	policies, _ := dig(cfg, "apps", "tls", "automation", "policies").([]any)
	if len(policies) != 1 || fmt.Sprint(dig(policies[0], "subjects")) != "["+placeholderDomain+"]" {
		t.Fatalf("TLS policies = %v", policies)
	}
	issuers, _ := dig(policies[0], "issuers").([]any)
	if len(issuers) == 0 {
		t.Fatal("no ACME issuer")
	}
	for _, is := range issuers {
		if dig(is, "module") != "acme" || dig(is, "challenges", "dns", "provider", "name") != "cloudflare" ||
			dig(is, "challenges", "dns", "provider", "api_token") != "{env.CLOUDFLARE_API_TOKEN}" {
			t.Errorf("issuer %v: want acme with the cloudflare DNS provider and {env.CLOUDFLARE_API_TOKEN}", is)
		}
		// Public resolvers for the zone lookup, and a fixed wait instead of
		// the propagation check (see the Caddyfile).
		if fmt.Sprint(dig(is, "challenges", "dns", "resolvers")) != "[1.1.1.1 1.0.0.1]" {
			t.Errorf("issuer %v: want resolvers 1.1.1.1 1.0.0.1", dig(is, "challenges", "dns"))
		}
		if d, _ := dig(is, "challenges", "dns", "propagation_delay").(float64); d < 10e9 ||
			dig(is, "challenges", "dns", "propagation_timeout") != float64(-1) {
			t.Errorf("issuer %v: want a propagation_delay of at least 10s and propagation_timeout -1", dig(is, "challenges", "dns"))
		}
		if dig(is, "email") != nil {
			t.Errorf("issuer has an email with ACME_EMAIL empty: %v", dig(is, "email"))
		}
	}
	if strings.Contains(raw, placeholderToken) {
		t.Error("the token value is written into the adapted config")
	}
}

func TestCaddyfileTakesAnOptionalACMEEmail(t *testing.T) {
	raw, cfg := adapted(t, "adapt-email.json")
	issuers, _ := dig(cfg, "apps", "tls", "automation", "policies", 0, "issuers").([]any)
	if len(issuers) == 0 {
		t.Fatal("no issuers")
	}
	for _, is := range issuers {
		if dig(is, "email") != "owner@example.com" {
			t.Errorf("issuer email = %v", dig(is, "email"))
		}
	}
	if strings.Contains(raw, placeholderToken) {
		t.Error("the token value is written into the adapted config")
	}
}
