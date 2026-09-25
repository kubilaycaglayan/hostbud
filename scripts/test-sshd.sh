#!/bin/sh
# test-sshd.sh up|down — the integration-test targets for `make test`.
#
#   up    Ensure network hostbud-test, throwaway keys in .cache/test-sshd/keys
#         (never committed), and two running targets built from test/sshd:
#         hostbud-test-sshd (tmux) and hostbud-test-sshd-notmux. They stay up
#         between runs (fast loop); images and containers are recreated only
#         when test/sshd changes. The tests restore any state they change.
#   down  Remove the containers, network and keys.
set -eu
root="$(cd "$(dirname "$0")/.." && pwd)"
keys="$root/.cache/test-sshd/keys"
net=hostbud-test
uid="$(id -u)"

hash="$(cd "$root" && find test/sshd -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -c1-12)"

# target <name> <with-tmux>
target() {
	name="$1" image="hostbud-test-sshd:$hash-tmux$2"
	if ! docker image inspect "$image" >/dev/null 2>&1; then
		docker build -q -t "$image" --build-arg WITH_TMUX="$2" "$root/test/sshd" >/dev/null
	fi
	if [ "$(docker inspect -f '{{.Config.Image}} {{.State.Running}}' "$name" 2>/dev/null)" != "$image true" ]; then
		docker rm -f "$name" >/dev/null 2>&1 || true
		docker run -d --name "$name" --network "$net" --label hostbud.test=1 \
			-v "$keys":/keys:ro "$image" >/dev/null
	fi
}

wait_healthy() {
	for name in "$@"; do
		i=0
		until [ "$(docker inspect -f '{{.State.Health.Status}}' "$name")" = healthy ]; do
			i=$((i + 1))
			[ $i -gt 60 ] && { echo "test-sshd: $name never became healthy" >&2; exit 1; }
			sleep 0.5
		done
	done
}

case "${1:-}" in
up)
	docker network inspect "$net" >/dev/null 2>&1 || docker network create "$net" >/dev/null
	if [ ! -f "$keys/client/id_ed25519" ]; then
		mkdir -p "$keys"
		base="hostbud-test-sshd:$hash-tmux1"
		docker image inspect "$base" >/dev/null 2>&1 ||
			docker build -q -t "$base" --build-arg WITH_TMUX=1 "$root/test/sshd" >/dev/null
		docker run --rm -v "$keys":/keys --entrypoint /usr/local/bin/keygen.sh "$base" "$uid"
		# New keys: the running targets must pick them up.
		docker rm -f hostbud-test-sshd hostbud-test-sshd-notmux >/dev/null 2>&1 || true
	fi
	target hostbud-test-sshd 1
	target hostbud-test-sshd-notmux 0
	wait_healthy hostbud-test-sshd hostbud-test-sshd-notmux
	;;
down)
	docker rm -f hostbud-test-sshd hostbud-test-sshd-notmux >/dev/null 2>&1 || true
	# Keys are root-owned in part; remove them from a container.
	[ -d "$keys" ] && docker run --rm -v "$root/.cache/test-sshd":/d --entrypoint rm debian:stable-slim -rf /d/keys
	docker network rm "$net" >/dev/null 2>&1 || true
	;;
*)
	echo "usage: test-sshd.sh up|down" >&2
	exit 2
	;;
esac
