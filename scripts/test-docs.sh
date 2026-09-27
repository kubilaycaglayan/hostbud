#!/bin/sh
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
fixture=$tmp/repo
mkdir -p "$fixture/internal/config" "$fixture/docs" "$fixture/deploy" "$fixture/scripts"
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
touch "$fixture/docs/ARCHITECTURE.md"
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

printf '\nAlso run `make missing-target`.\n' >>"$fixture/README.md"
assert_fail sh "$fixture/scripts/check-docs.sh" "$fixture"
sed -i '/HOSTBUD_LISTEN/d' "$fixture/.env.example"
assert_fail sh "$fixture/scripts/check-docs.sh" "$fixture"
sed -i 's#docs/ARCHITECTURE.md#docs/ABSENT.md#' "$fixture/README.md"
assert_fail sh "$fixture/scripts/check-docs.sh" "$fixture"

printf 'docs shell checks passed\n'
