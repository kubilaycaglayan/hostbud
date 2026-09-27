#!/bin/sh
# Read-only checks for a hostbud installation. No check changes host state.
set -u

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
env_file=${HOSTBUD_DOCTOR_ENV_FILE:-$repo/.env}
failures=0

env_value() {
	file=$1 key=$2
	[ -r "$file" ] || return 1
	awk -v key="$key" '
		index($0, "=") { name=substr($0, 1, index($0, "=")-1); if (name == key) { value=substr($0, index($0, "=")+1); sub(/^[[:space:]]+/, "", value); sub(/[[:space:]]+$/, "", value); if (value ~ /^\047.*\047$/ || value ~ /^".*"$/) value=substr(value, 2, length(value)-2); print value; exit } }
	' "$file"
}

is_placeholder() {
	case "$1" in
		''|replace-with-a-random-password|hostbud.example.com|100.64.0.1|CHANGEME|CHANGE_ME|TODO) return 0 ;;
		*) return 1 ;;
	esac
}

check() {
	label=$1; shift
	if "$@" >/dev/null 2>&1; then printf '✓ %s\n' "$label"; return 0; fi
	printf '✗ %s\n' "$label"
	failures=$((failures + 1))
	return 1
}

check_env_file() {
	[ -f "$env_file" ] && [ -r "$env_file" ] || return 1
	mode=$(stat -c '%a' "$env_file" 2>/dev/null) || return 1
	case "$mode" in *[!0-7]*|'') return 1 ;; esac
	[ $((0$mode & 0177)) -eq 0 ] || return 1
	[ $((0$mode & 400)) -ne 0 ] || return 1
	for key in HOST_UID HOST_GID HOST_HOME HOST_SSH_USER HOST_SSH_AUTH_SOCK HOSTBUD_DOMAIN TAILSCALE_IP CLOUDFLARE_API_TOKEN HOSTBUD_SUBNET HOSTBUD_DB_NAME HOSTBUD_DB_USER HOSTBUD_DB_PASSWORD HOSTBUD_LOCAL_PORT HOSTBUD_DB_LOCAL_PORT; do
		value=$(env_value "$env_file" "$key") || return 1
		if is_placeholder "$value"; then return 1; fi
	done
	return 0
}

check_ids() {
	uid=$(env_value "$env_file" HOST_UID) && gid=$(env_value "$env_file" HOST_GID) || return 1
	[ "$uid" = "$(id -u)" ] && [ "$gid" = "$(id -g)" ]
}

check_agent() {
	sock=$(env_value "$env_file" HOST_SSH_AUTH_SOCK) || return 1
	[ -S "$sock" ] || return 1
	SSH_AUTH_SOCK=$sock ssh-add -l >/dev/null 2>&1
}

check_host_ssh() {
	user=$(env_value "$env_file" HOST_SSH_USER) || return 1
	[ -n "$user" ] && ssh -o BatchMode=yes -o ConnectTimeout=5 "${user}@localhost" true
}

check_host_tmux() { tmux -V >/dev/null 2>&1; }

check_hostkeys() {
	for kind in ed25519 ecdsa rsa; do [ -r "/etc/ssh/ssh_host_${kind}_key.pub" ] || return 1; done
}

check_tailscale_ip() {
	ipaddr=$(env_value "$env_file" TAILSCALE_IP) || return 1
	ip -o addr show | awk -v target="$ipaddr" '{ split($4, a, "/"); if (a[1] == target) found=1 } END { exit !found }'
}

check_domain_ip() {
	domain=$(env_value "$env_file" HOSTBUD_DOMAIN) || return 1
	ipaddr=$(env_value "$env_file" TAILSCALE_IP) || return 1
	answers=$(getent ahostsv4 "$domain" 2>/dev/null) || answers=
	[ -n "$answers" ] || return 2
	printf '%s\n' "$answers" | awk -v target="$ipaddr" '$1 == target { found=1 } END { exit !found }'
}

port_is_free_or_hostbud() {
	port=$1
	listeners=$(ss -ltnp 2>/dev/null) || return 1
	printf '%s\n' "$listeners" | grep -Eq "[:.]${port}[[:space:]]" || return 0
	containers=$(docker ps --format '{{.Names}} {{.Ports}}' 2>/dev/null) || return 1
	printf '%s\n' "$containers" | awk -v p="$port" 'tolower($0) ~ /hostbud/ && $0 ~ (":" p "->") { owned=1 } END { exit !owned }'
}

check_ports() {
	local_port=$(env_value "$env_file" HOSTBUD_LOCAL_PORT) && db_port=$(env_value "$env_file" HOSTBUD_DB_LOCAL_PORT) || return 1
	case "$local_port:$db_port" in *[!0-9:]*|:*) return 1 ;; esac
	port_is_free_or_hostbud "$local_port" && port_is_free_or_hostbud "$db_port"
}

check_subnet() {
	subnet=$(env_value "$env_file" HOSTBUD_SUBNET) || return 1
	network_ids=$(docker network ls -q) || return 1
	networks=
	for network_id in $network_ids; do
		network_info=$(docker network inspect -f '{{.Name}} {{range .IPAM.Config}}{{.Subnet}} {{end}}' "$network_id") || return 1
		networks="$networks
$network_info"
	done
	awk -v candidate="$subnet" -v networks="$networks" '
	function ipnum(ip, a,n,value,i) { n=split(ip,a,"."); if(n!=4)return -1; value=0; for(i=1;i<=4;i++){if(a[i]!~/^[0-9]+$/||a[i]>255)return -1; value=value*256+a[i]} return value }
	function bounds(c, parts, slash, bits, start, size) { split(c,parts,"/"); bits=parts[2]+0; start=ipnum(parts[1]); if(start<0||parts[2]!~/^[0-9]+$/||bits<0||bits>32)return 0; size=2^(32-bits); low=int(start/size)*size; high=low+size-1; return 1 }
	BEGIN {
		if (!bounds(candidate, cp)) exit 1
		if (cp[2]+0 < 12 || ipnum(cp[1]) < ipnum("172.16.0.0") || ipnum(cp[1]) > ipnum("172.31.255.255") || low != ipnum(cp[1])) exit 1
		candidate_low=low; candidate_high=high
		n=split(networks, lines, "\n")
		for(i=1;i<=n;i++) { count=split(lines[i], words, " "); name=words[1]; if(name==""||name=="hostbud")continue; for(j=2;j<=count;j++)if(words[j]!="") { if(!bounds(words[j], op))continue; if(low<=candidate_high&&high>=candidate_low)exit 1 } }
	}'
}

check_tailscaled_socket_if_enabled() {
	users=$(env_value "$env_file" HOSTBUD_ALLOWED_TS_USERS)
	compose_file=$(env_value "$env_file" COMPOSE_FILE)
	case "$compose_file" in *compose.tailscale.yml*) enabled=1 ;; *) enabled=0 ;; esac
	[ -n "$users" ] && enabled=1
	[ "$enabled" -eq 0 ] && return 0
	socket=$(env_value "$env_file" TAILSCALED_SOCKET)
	[ -n "$socket" ] && [ -S "$socket" ]
}

if [ "${HOSTBUD_DOCTOR_LIBRARY_ONLY:-0}" = 1 ]; then return 0 2>/dev/null || exit 0; fi

check 'Docker and Compose v2 are available' sh -c 'command -v docker >/dev/null && docker compose version >/dev/null && docker info >/dev/null' || printf '  Fix: install Docker Engine with the Compose v2 plugin and grant this user daemon access.\n'
check 'local .env is private, complete and has no placeholders' check_env_file || printf '  Fix: copy .env.example to .env, fill required values, then chmod 600 .env.\n'
check 'HOST_UID and HOST_GID match this user' check_ids || printf '  Fix: set HOST_UID=$(id -u) and HOST_GID=$(id -g) in .env.\n'
check 'SSH agent socket has at least one key' check_agent || printf '  Fix: start the configured agent, load the hostbud key with ssh-add, and set HOST_SSH_AUTH_SOCK to its socket.\n'
check 'sshd accepts the configured user over localhost' check_host_ssh || printf '  Fix: enable sshd and verify the configured user can run a non-interactive localhost SSH command.\n'
check 'tmux is installed on the host' check_host_tmux || printf '  Fix: install tmux on the host (for Debian/Ubuntu: sudo apt install tmux).\n'
check 'all three pinned SSH host public keys exist' check_hostkeys || printf '  Fix: ensure ed25519, ecdsa and rsa host public keys exist under /etc/ssh.\n'
check 'TAILSCALE_IP is assigned to a local interface' check_tailscale_ip || printf '  Fix: start Tailscale and set TAILSCALE_IP to its local IPv4 address.\n'
if check_domain_ip; then
	printf '✓ HOSTBUD_DOMAIN resolves to TAILSCALE_IP\n'
else
	dns_status=$?
	if [ "$dns_status" -eq 2 ]; then
		printf '! HOSTBUD_DOMAIN did not resolve from this host\n  Fix: verify the DNS A record points to TAILSCALE_IP; host DNS may not resolve tailnet records.\n'
	else
		printf '✗ HOSTBUD_DOMAIN resolves to a different address\n  Fix: point its DNS A record to TAILSCALE_IP.\n'
		failures=$((failures + 1))
	fi
fi
check 'HOSTBUD_LOCAL_PORT and HOSTBUD_DB_LOCAL_PORT are free or held by hostbud' check_ports || printf '  Fix: stop the process using either port or choose unused loopback ports.\n'
check 'HOSTBUD_SUBNET is private and does not overlap another Docker network' check_subnet || printf '  Fix: choose an unused subnet inside 172.16.0.0/12 in HOSTBUD_SUBNET.\n'
check 'tailscaled socket exists when the identity allowlist is enabled' check_tailscaled_socket_if_enabled || printf '  Fix: start tailscaled and set TAILSCALED_SOCKET to its LocalAPI socket.\n'

if [ "$failures" -ne 0 ]; then printf 'make doctor: %s required check(s) failed\n' "$failures"; exit 1; fi
printf 'make doctor: all required checks passed\n'
