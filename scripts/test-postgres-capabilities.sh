#!/bin/sh
# Start PostgreSQL with only the capabilities proven necessary for its
# official-image entrypoint, using a fresh disposable volume.
set -eu
root="$(cd "$(dirname "$0")/.." && pwd)"
marker="$root/.cache/postgres-capabilities.ok"
mkdir -p "$root/.cache"
rm -f "$marker"
name="hostbud-test-postgres-caps-$$"
volume="hostbud-test-postgres-caps-$$"
cleanup() {
	docker rm -f "$name" >/dev/null 2>&1 || true
	docker volume rm "$volume" >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM

docker volume create "$volume" >/dev/null
docker run -d --name "$name" --network hostbud-test --label hostbud.test=1 \
	--cap-drop ALL --cap-add DAC_OVERRIDE --cap-add SETGID --cap-add SETUID \
	--security-opt no-new-privileges:true --shm-size 128m \
	-e POSTGRES_DB=hostbud_test -e POSTGRES_USER=hostbud_test \
	-e POSTGRES_PASSWORD=hostbud-test-password \
	-v "$volume":/var/lib/postgresql/data postgres:15.13-bookworm >/dev/null

i=0
until docker exec "$name" psql -U hostbud_test -d hostbud_test -c 'SELECT 1' >/dev/null 2>&1; do
	i=$((i + 1))
	if [ "$i" -gt 90 ]; then
		docker logs "$name" >&2
		echo "test-postgres-capabilities: PostgreSQL didn't start with the reduced capability set" >&2
		exit 1
	fi
	sleep 0.5
done
echo verified >"$marker"
echo "test-postgres-capabilities: fresh-volume PostgreSQL started with DAC_OVERRIDE, SETGID and SETUID"
