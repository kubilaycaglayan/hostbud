#!/bin/sh
# Renders docker-compose.yml with placeholder values (never the real .env)
# into .cache/compose-config.json for the deploy-config integration tests.
set -eu
root="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$root/.cache"
cd "$root"
env -i PATH="$PATH" HOME="$HOME" DOCKER_HOST="${DOCKER_HOST:-}" \
	HOST_UID=1000 HOST_GID=1000 HOST_SSH_USER=dev HOST_SSH_AUTH_SOCK=/run/user/1000/openssh_agent \
	HOSTBUD_DB_NAME=hostbud HOSTBUD_DB_USER=hostbud HOSTBUD_DB_PASSWORD=placeholder-password \
	docker compose --env-file /dev/null config --format json >"$root/.cache/compose-config.json"
