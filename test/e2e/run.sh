#!/bin/sh
# E2E driver for the hostbud-e2e Compose project (docs/ARCHITECTURE.md §13.1).
#
#   run.sh full [args]  make e2e: fresh stack, run Playwright, tear everything
#                       down (down -v), also on failure or Ctrl-C.
#   run.sh up           make e2e-up: start (or update) a persistent stack.
#   run.sh run [args]   make e2e-run: `up`, then run Playwright against it,
#                       leaving it running (fast edit/test loop).
#   run.sh down         make e2e-down: remove the stack and its volumes.
#
# Images are rebuilt only when their inputs change (content hash stamped in
# .cache/e2e/), so an unchanged tree skips docker build entirely. Extra args
# go to `playwright test` (e.g. `make e2e ARGS="-g smoke"`).
set -u
cd "$(dirname "$0")"
root="$(cd ../.. && pwd)"
stamps="$root/.cache/e2e"

dc() { docker compose -p hostbud-e2e -f compose.yml "$@"; }

# hash <paths…>: content hash of the files under the given paths (relative to
# the repo root), ignoring dependency and build output directories.
hash() {
	(cd "$root" && find "$@" -type f \
		-not -path '*/node_modules/*' -not -path 'web/dist/*' -not -path '*/results/*' \
		-print0 2>/dev/null | sort -z | xargs -0 sha256sum | sha256sum | cut -d' ' -f1)
}

# build_if_changed <service> <image> <paths…>
build_if_changed() {
	svc="$1" image="$2"
	shift 2
	h="$(hash "$@")"
	if [ "$(cat "$stamps/$svc" 2>/dev/null)" = "$h" ] && docker image inspect "$image" >/dev/null 2>&1; then
		return 0
	fi
	echo "e2e: building $svc"
	dc build "$svc" || exit 1
	# The image it replaced is now untagged; drop it instead of accumulating.
	docker image prune -f --filter label=hostbud.image=1 >/dev/null
	mkdir -p "$stamps" && echo "$h" >"$stamps/$svc"
}

build() {
	build_if_changed hostbud-e2e-app hostbud-e2e-app:local \
		Dockerfile .dockerignore go.mod go.sum cmd internal web test/e2e/compose.yml
	build_if_changed hostbud-e2e-keygen hostbud-e2e-target:local test/sshd
	build_if_changed hostbud-e2e-target-notmux hostbud-e2e-target-notmux:local test/sshd
	build_if_changed hostbud-e2e-caddy hostbud-e2e-caddy:local deploy/caddy/Dockerfile
	build_if_changed hostbud-e2e-ctl hostbud-e2e-ctl:local test/e2e/ctl
	build_if_changed hostbud-e2e-runner hostbud-e2e-runner:local \
		test/e2e/Dockerfile test/e2e/package.json test/e2e/pnpm-lock.yaml test/e2e/pnpm-workspace.yaml
}

up() {
	build
	dc up -d --wait || exit 1
}

# Runs Playwright in the idle runner, then hands results/ back to the invoking
# user (the runner writes them as root).
playwright() {
	# Empty results/ but keep the directory: it is bind-mounted into the
	# running runner, and replacing it would orphan that mount.
	mkdir -p results
	find results -mindepth 1 -delete 2>/dev/null
	docker exec hostbud-e2e-runner sh -c \
		'corepack pnpm exec playwright test "$@"; s=$?; chown -R '"$(id -u):$(id -g)"' results; exit $s' \
		-- "$@"
}

# logs_clean: "Logs clean" scenario. Fails if the app's (info-level) logs
# contain any value a spec registered with forbidInLogs().
logs_clean() {
	[ -s results/log-markers.txt ] || return 0
	docker logs hostbud-e2e-app >results/app.log 2>&1
	if grep -F -f results/log-markers.txt results/app.log >results/log-leaks.txt; then
		echo "e2e: FAIL logs clean: hostbud-e2e-app logs contain scenario paths/commands/markers:" >&2
		cat results/log-leaks.txt >&2
		return 1
	fi
	echo "e2e: logs clean ($(wc -l <results/log-markers.txt) markers checked)"
}

down() {
	dc down -v --remove-orphans --timeout 5 >/dev/null 2>&1
}

cleanup() {
	status=$?
	trap - EXIT INT TERM
	mkdir -p results
	dc logs --no-color --timestamps >results/compose.log 2>&1
	down
	echo "e2e: torn down; logs and failure artifacts in test/e2e/results/"
	exit "$status"
}

mode="${1:-full}"
[ $# -gt 0 ] && shift
case "$mode" in
full)
	down # leftovers from an interrupted run or a persistent stack
	trap cleanup EXIT
	trap 'exit 130' INT TERM
	up
	playwright "$@"
	status=$?
	logs_clean || status=1
	exit "$status"
	;;
up) up ;;
run)
	up
	playwright "$@"
	status=$?
	logs_clean || status=1
	dc logs --no-color --timestamps >results/compose.log 2>&1
	exit "$status"
	;;
down) down ;;
*)
	echo "usage: run.sh full|up|run|down [playwright args]" >&2
	exit 2
	;;
esac
