#!/bin/sh
# Exercise the built app with a read-only root filesystem against only the
# disposable integration target and database.
set -eu
root="$(cd "$(dirname "$0")/.." && pwd)"
uid="$(id -u)"
gid="$(id -g)"
keys="$root/.cache/test-sshd/keys"
image=hostbud-test-readonly:local
app=hostbud-test-readonly-app
agent=hostbud-test-readonly-agent
data_volume="hostbud-test-readonly-data-$$"
agent_volume="hostbud-test-readonly-agent-$$"
marker="$root/.cache/readonly-image.ok"
email="readonly-$(date +%s)-$$@example.test"
password=hostbud-readonly-integration-password
cleanup() {
	docker rm -f "$app" "$agent" >/dev/null 2>&1 || true
	docker volume rm "$data_volume" "$agent_volume" >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM
mkdir -p "$root/.cache"
rm -f "$marker"

docker build -q -t "$image" --build-arg HOST_UID="$uid" --build-arg HOST_GID="$gid" "$root" >/dev/null
docker volume create "$data_volume" >/dev/null
docker volume create "$agent_volume" >/dev/null
docker run --rm --user 0:0 --mount "type=volume,src=$agent_volume,dst=/data" \
	--entrypoint chown "$image" "$uid:$gid" /data
docker run -d --name "$agent" --network hostbud-test --label hostbud.test=1 \
	--user "$uid:$gid" --mount "type=volume,src=$agent_volume,dst=/agent" \
	--mount "type=bind,src=$keys,dst=/keys,readonly" --entrypoint sh "$image" -ec '
	ssh-agent -D -a /agent/ssh-agent.sock &
	while [ ! -S /agent/ssh-agent.sock ]; do sleep 0.1; done
	SSH_AUTH_SOCK=/agent/ssh-agent.sock ssh-add -q /keys/client/id_ed25519
	wait
'
i=0
until docker exec "$agent" sh -c 'SSH_AUTH_SOCK=/agent/ssh-agent.sock ssh-add -l >/dev/null 2>&1'; do
	i=$((i + 1))
	if [ "$i" -gt 30 ]; then docker logs "$agent" >&2; echo "test-readonly-image: ssh-agent didn't become ready" >&2; exit 1; fi
	sleep 0.2
done

docker run -d --name "$app" --network hostbud-test --label hostbud.test=1 \
	--user "$uid:$gid" --read-only --tmpfs /tmp --cap-drop ALL \
	--security-opt no-new-privileges:true --pids-limit 1024 \
	--add-host host.docker.internal:host-gateway \
	--mount "type=volume,src=$data_volume,dst=/data" \
	--mount "type=volume,src=$agent_volume,dst=/run/agent,readonly" \
	--mount "type=bind,src=$keys/hostpub/ssh_host_ed25519_key.pub,dst=/run/host-keys/ssh_host_ed25519_key.pub,readonly" \
	--mount "type=bind,src=$keys/hostpub/ssh_host_ecdsa_key.pub,dst=/run/host-keys/ssh_host_ecdsa_key.pub,readonly" \
	--mount "type=bind,src=$keys/hostpub/ssh_host_rsa_key.pub,dst=/run/host-keys/ssh_host_rsa_key.pub,readonly" \
	-e HOSTBUD_LISTEN=:8080 -e HOSTBUD_DATA_DIR=/data -e HOSTBUD_LOCAL_PORT=9055 \
	-e HOSTBUD_DOMAIN=hostbud.example.com -e HOSTBUD_HOST_ADDR=hostbud-test-sshd \
	-e HOSTBUD_HOST_LABEL='Disposable test target' -e HOST_SSH_USER=dev \
	-e SSH_AUTH_SOCK=/run/agent/ssh-agent.sock -e HOSTBUD_DB_HOST=hostbud-test-postgres \
	-e HOSTBUD_DB_PORT=5432 -e HOSTBUD_DB_NAME=hostbud_test -e HOSTBUD_DB_USER=hostbud_test \
	-e HOSTBUD_DB_PASSWORD=hostbud-test-password -e HOSTBUD_DB_SSLMODE=disable \
	"$image" >/dev/null

i=0
until docker exec "$app" hostbud healthcheck >/dev/null 2>&1; do
	i=$((i + 1))
	if [ "$i" -gt 60 ]; then docker logs "$app" >&2; echo "test-readonly-image: healthcheck failed" >&2; exit 1; fi
	sleep 0.5
done
docker exec hostbud-test-postgres psql -v ON_ERROR_STOP=1 -U hostbud_test -d hostbud_test \
	-c "INSERT INTO email_allowlist (email_normalized, enabled, note) VALUES ('$email', true, 'read-only image integration check')"
TOOL_NETWORK=hostbud-test scripts/tool.sh go "$(scripts/go-toolbox-image.sh golang:1.27.1-bookworm)" . \
	env HOSTBUD_TEST_EMAIL="$email" HOSTBUD_TEST_PASSWORD="$password" go run ./scripts/test-readonly-client
echo verified >"$marker"
echo "test-readonly-image: read-only app health and authenticated session list passed"
