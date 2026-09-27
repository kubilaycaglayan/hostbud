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

printf '\nAlso run `make missing-target`.\n' >>"$fixture/README.md"
assert_fail sh "$fixture/scripts/check-docs.sh" "$fixture"
sed -i '/HOSTBUD_LISTEN/d' "$fixture/.env.example"
assert_fail sh "$fixture/scripts/check-docs.sh" "$fixture"
sed -i 's#docs/ARCHITECTURE.md#docs/ABSENT.md#' "$fixture/README.md"
assert_fail sh "$fixture/scripts/check-docs.sh" "$fixture"

printf 'docs shell checks passed\n'
