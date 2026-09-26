#!/bin/sh
# Builds the hostbud-caddy image (deploy/caddy/Dockerfile) and records what the
# deploy-config integration tests check about it in .cache/caddy/:
#   modules.txt      `caddy list-modules` of the built image
#   adapt.json       the production Caddyfile adapted to JSON, with placeholder
#                    values (never the real .env) and ACME_EMAIL empty
#   adapt-email.json the same with ACME_EMAIL set
# Skipped when deploy/caddy is unchanged since the last run (a `docker run`
# costs seconds), unless the outputs are missing.
set -eu
root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/.cache/caddy"
mkdir -p "$out"
cd "$root"

hash="$(find deploy/caddy -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -d' ' -f1)"
if [ "$(cat "$out/stamp" 2>/dev/null)" = "$hash" ] && [ -s "$out/modules.txt" ] && [ -s "$out/adapt-email.json" ] &&
	docker image inspect hostbud-caddy:local >/dev/null 2>&1; then
	exit 0
fi

docker build -q -t hostbud-caddy:local deploy/caddy >/dev/null
# One container for all three outputs (each `docker run` costs seconds).
docker run --rm -v "$root/deploy/caddy:/etc/caddy:ro" \
	-e HOSTBUD_DOMAIN=hostbud.example.com -e HOSTBUD_LOCAL_PORT=9055 \
	-e CLOUDFLARE_API_TOKEN=placeholder-cloudflare-token \
	hostbud-caddy:local sh -ec '
		caddy list-modules >/tmp/modules.txt
		ACME_EMAIL= caddy adapt --config /etc/caddy/Caddyfile >/tmp/adapt.json
		ACME_EMAIL=owner@example.com caddy adapt --config /etc/caddy/Caddyfile >/tmp/adapt-email.json
		tar -C /tmp -c modules.txt adapt.json adapt-email.json' | tar -C "$out" -x
echo "$hash" >"$out/stamp"
