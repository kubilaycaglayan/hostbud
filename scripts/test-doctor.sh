#!/bin/sh
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

export HOSTBUD_DOCTOR_LIBRARY_ONLY=1
. "$repo/scripts/doctor.sh"

assert_ok() { "$@" || { printf 'expected success: %s\n' "$*" >&2; exit 1; }; }
assert_fail() { if "$@"; then printf 'expected failure: %s\n' "$*" >&2; exit 1; fi; }

cat >"$tmp/env" <<EOF
HOST_UID=$(id -u)
HOST_GID=$(id -g)
HOST_HOME=/home/dev
HOST_SSH_USER=dev
HOST_SSH_AUTH_SOCK=$tmp/agent.sock
HOSTBUD_DOMAIN=hostbud.example.test
TAILSCALE_IP=192.0.2.4
CLOUDFLARE_API_TOKEN=example-token
HOSTBUD_SUBNET=172.29.55.0/24
HOSTBUD_DB_NAME=hostbud
HOSTBUD_DB_USER=hostbud
HOSTBUD_DB_PASSWORD=example-database-password
HOSTBUD_LOCAL_PORT=9055
HOSTBUD_DB_LOCAL_PORT=9543
EOF
chmod 600 "$tmp/env"
HOSTBUD_DOCTOR_ENV_FILE=$tmp/env
env_file=$tmp/env
assert_ok check_env_file

sed 's/replace-never-values/replace-with-a-random-password/' "$tmp/env" >"$tmp/placeholder"
sed -i 's/^HOSTBUD_DB_PASSWORD=.*/HOSTBUD_DB_PASSWORD=replace-with-a-random-password/' "$tmp/placeholder"
chmod 600 "$tmp/placeholder"
env_file=$tmp/placeholder
assert_fail check_env_file
env_file=$tmp/env

chmod 644 "$tmp/env"
assert_fail check_env_file
chmod 700 "$tmp/env"
assert_fail check_env_file
chmod 600 "$tmp/env"
assert_fail check_agent

mkdir -p "$tmp/bin"
cat >"$tmp/bin/docker" <<'EOF'
#!/bin/sh
case "$*" in
  'network ls -q') echo sample-id ;;
  *'network inspect'*) echo "$DOCTOR_NETWORKS" ;;
  'ps --format {{.Names}} {{.Ports}}') echo "$DOCTOR_CONTAINERS" ;;
  *) exit 1 ;;
esac
EOF
cat >"$tmp/bin/ss" <<'EOF'
#!/bin/sh
printf '%s\n' "$DOCTOR_LISTENERS"
EOF
cat >"$tmp/bin/getent" <<'EOF'
#!/bin/sh
printf '%s\n' "$DOCTOR_DNS"
EOF
chmod +x "$tmp/bin/docker" "$tmp/bin/ss" "$tmp/bin/getent"
PATH=$tmp/bin:$PATH
export PATH

DOCTOR_NETWORKS='bridge 172.17.0.0/16'
export DOCTOR_NETWORKS
assert_ok check_subnet
DOCTOR_NETWORKS='other 172.29.0.0/16'
export DOCTOR_NETWORKS
assert_fail check_subnet
DOCTOR_NETWORKS='bridge 172.17.0.0/16'
DOCTOR_LISTENERS='LISTEN 0 4096 127.0.0.1:9055 0.0.0.0:* users:("other",pid=12,fd=3)'
DOCTOR_CONTAINERS='other-service 0.0.0.0:9055->9055/tcp'
export DOCTOR_NETWORKS DOCTOR_LISTENERS DOCTOR_CONTAINERS
assert_fail port_is_free_or_hostbud 9055
DOCTOR_CONTAINERS='hostbud-caddy 127.0.0.1:9055->9055/tcp'
export DOCTOR_CONTAINERS
assert_ok port_is_free_or_hostbud 9055

DOCTOR_DNS='192.0.2.4 STREAM hostbud.example.test'
export DOCTOR_DNS
assert_ok check_domain_ip
DOCTOR_DNS=''
export DOCTOR_DNS
if check_domain_ip; then
	printf 'expected unresolved host DNS\n' >&2
	exit 1
else
	[ "$?" -eq 2 ] || { printf 'expected DNS resolution warning state\n' >&2; exit 1; }
fi
DOCTOR_DNS='203.0.113.9 STREAM hostbud.example.test'
export DOCTOR_DNS
assert_fail check_domain_ip

printf 'doctor shell checks passed\n'
