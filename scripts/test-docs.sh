#!/bin/sh
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
fixture=$tmp/repo
mkdir -p "$fixture/internal/config" "$fixture/internal/api/testdata" "$fixture/docs" "$fixture/deploy" "$fixture/scripts"
cp "$repo/scripts/check-docs.sh" "$fixture/scripts/"

cat >"$fixture/Makefile" <<'EOF'
.PHONY: build lint
build: ## build
lint: ## lint
EOF
cat >"$fixture/README.md" <<'EOF'
Run `make build`.
See [architecture](docs/ARCHITECTURE.md).
EOF
cat >"$fixture/docs/ARCHITECTURE.md" <<'EOF'
## 9. API surface (v1)
GET|PUT /api/ui-state/:key            state
POST   /api/queues/:id/start|pause    control
## 10. Next
EOF
cat >"$fixture/internal/api/testdata/routes.json" <<'EOF'
[
  {"method":"PUT","path":"/api/ui-state/{key}","stateChanging":true},
  {"method":"POST","path":"/api/queues/{id}/pause","stateChanging":true}
]
EOF
cat >"$fixture/internal/config/config.go" <<'EOF'
package config
var listen = getenv("HOSTBUD_LISTEN")
EOF
cat >"$fixture/.env.example" <<'EOF'
HOSTBUD_LISTEN=:8080
EOF
cat >"$fixture/docker-compose.yml" <<'EOF'
services:
  hostbud:
    environment:
      HOSTBUD_LISTEN: :8080
EOF
cat >"$fixture/deploy/compose.tailscale.yml" <<'EOF'
services:
  hostbud:
    environment: {}
EOF

assert_ok() { "$@" || { printf 'expected success: %s\n' "$*" >&2; exit 1; }; }
assert_fail() { if "$@"; then printf 'expected failure: %s\n' "$*" >&2; exit 1; fi; }
assert_ok sh "$fixture/scripts/check-docs.sh" "$fixture"

# A route the router has but ARCHITECTURE §9 doesn't list.
cp "$fixture/internal/api/testdata/routes.json" "$tmp/routes.json"
printf '[{"method":"DELETE","path":"/api/queues/{id}","stateChanging":true}]\n' >"$fixture/internal/api/testdata/routes.json"
assert_fail sh "$fixture/scripts/check-docs.sh" "$fixture"
cp "$tmp/routes.json" "$fixture/internal/api/testdata/routes.json"
assert_ok sh "$fixture/scripts/check-docs.sh" "$fixture"

# V2-M2: the parallel-queues switch and the capacity route are checked like
# every var and route: dropping either from its doc fails.
printf 'var parallel = get("HOSTBUD_PARALLEL_QUEUES", "false")\n' >>"$fixture/internal/config/config.go"
assert_fail sh "$fixture/scripts/check-docs.sh" "$fixture"
printf 'HOSTBUD_PARALLEL_QUEUES=false\n' >>"$fixture/.env.example"
assert_fail sh "$fixture/scripts/check-docs.sh" "$fixture" # not in the hostbud Compose environment yet
printf '      HOSTBUD_PARALLEL_QUEUES: false\n' >>"$fixture/docker-compose.yml"
assert_ok sh "$fixture/scripts/check-docs.sh" "$fixture"
cp "$fixture/internal/api/testdata/routes.json" "$tmp/routes.json"
printf '[{"method":"PUT","path":"/api/machines/{machine}/capacity","stateChanging":true}]\n' >"$fixture/internal/api/testdata/routes.json"
assert_fail sh "$fixture/scripts/check-docs.sh" "$fixture"
sed -i '/^## 10. Next/i GET|PUT /api/machines/:id/capacity    cap' "$fixture/docs/ARCHITECTURE.md"
assert_ok sh "$fixture/scripts/check-docs.sh" "$fixture"
cp "$tmp/routes.json" "$fixture/internal/api/testdata/routes.json"
# ...and the real docs name both.
grep -q '^HOSTBUD_PARALLEL_QUEUES=false$' "$repo/.env.example" || { echo 'HOSTBUD_PARALLEL_QUEUES missing from .env.example' >&2; exit 1; }
grep -q 'HOSTBUD_PARALLEL_QUEUES' "$repo/README.md" || { echo 'README misses HOSTBUD_PARALLEL_QUEUES' >&2; exit 1; }
grep -q '^GET|PUT /api/machines/:id/capacity' "$repo/docs/ARCHITECTURE.md" || { echo 'ARCHITECTURE §9 misses the capacity route' >&2; exit 1; }

printf '\nAlso run `make missing-target`.\n' >>"$fixture/README.md"
assert_fail sh "$fixture/scripts/check-docs.sh" "$fixture"
sed -i '/HOSTBUD_LISTEN/d' "$fixture/.env.example"
assert_fail sh "$fixture/scripts/check-docs.sh" "$fixture"
sed -i 's#docs/ARCHITECTURE.md#docs/ABSENT.md#' "$fixture/README.md"
assert_fail sh "$fixture/scripts/check-docs.sh" "$fixture"

printf 'docs shell checks passed\n'
