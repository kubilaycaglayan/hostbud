#!/bin/sh
set -eu

if [ "$#" -ne 1 ] || [ -z "$1" ] || [ ! -f "$1" ]; then
	echo 'usage: restore-check.sh FILE (existing dump file required)' >&2
	exit 2
fi

source_file=$1
container_file="/tmp/hostbud-restore-check-$$.dump"
cleanup() {
	docker compose exec -T hostbud rm -f "$container_file" >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM

docker compose exec -T hostbud sh -c 'umask 077; cat > "$1"' sh "$container_file" <"$source_file"
docker compose exec -T hostbud hostbud restore-check "$container_file"
