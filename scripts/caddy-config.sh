#!/bin/sh
# Builds the hostbud-caddy image (deploy/caddy/Dockerfile) and records what the
# deploy-config integration tests check about it in .cache/caddy/:
#   modules.txt  `caddy list-modules` of the built image
# Skipped when deploy/caddy is unchanged since the last run (a `docker run`
# costs seconds), unless the outputs are missing.
set -eu
root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/.cache/caddy"
mkdir -p "$out"
cd "$root"

hash="$(find deploy/caddy -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -d' ' -f1)"
if [ "$(cat "$out/stamp" 2>/dev/null)" = "$hash" ] && [ -s "$out/modules.txt" ] &&
	docker image inspect hostbud-caddy:local >/dev/null 2>&1; then
	exit 0
fi

docker build -q -t hostbud-caddy:local deploy/caddy >/dev/null
docker run --rm hostbud-caddy:local caddy list-modules >"$out/modules.txt"
echo "$hash" >"$out/stamp"
