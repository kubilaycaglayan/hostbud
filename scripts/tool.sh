#!/bin/sh
# tool.sh <name> <image> <workdir> <cmd…>
#
# Runs <cmd> in a long-lived toolbox container `hostbud-tools-<name>` (repo
# mounted at /src, running as the invoking user), creating it on first use or
# when <image> changes. `docker exec` into a running container is ~100x faster
# than a fresh `docker run` per command. `make tools-down` removes them.
# With TOOL_NETWORK set, the container joins that Docker network (created if
# missing), e.g. so integration tests reach test/sshd.
set -eu
name="hostbud-tools-$1" image="$2" workdir="$3"
shift 3
root="$(cd "$(dirname "$0")/.." && pwd)"

net="${TOOL_NETWORK:-default}"
if [ "$net" != default ]; then
	docker network inspect "$net" >/dev/null 2>&1 || docker network create "$net" >/dev/null
fi
if [ "$(docker inspect -f '{{.Config.Image}} {{.State.Running}} {{.HostConfig.NetworkMode}}' "$name" 2>/dev/null)" != "$image true $net" ]; then
	docker rm -f "$name" >/dev/null 2>&1 || true
	docker run -d --name "$name" --label hostbud.tools=1 --init --network "$net" \
		-u "$(id -u):$(id -g)" -v "$root":/src -w /src \
		--entrypoint sleep "$image" infinity >/dev/null
fi

tty=""
[ -t 0 ] && [ -t 1 ] && tty="-t"
# Caches live in ./.cache (gitignored) so they survive container re-creation.
exec docker exec -i $tty -w "/src/$workdir" \
	-e HOME=/tmp -e CI=true -e GOFLAGS=-buildvcs=false \
	-e GOMODCACHE=/src/.cache/gomod -e GOCACHE=/src/.cache/gobuild \
	-e GOLANGCI_LINT_CACHE=/src/.cache/golangci \
	-e COREPACK_ENABLE_DOWNLOAD_PROMPT=0 -e COREPACK_HOME=/src/.cache/corepack \
	-e pnpm_config_store_dir=/src/.cache/pnpm-store \
	"$name" "$@"
