#!/bin/sh
# Check the repository's user-facing setup docs against its build and config.
set -eu

repo=${1:-$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)}
cd "$repo"
errors=0
fail() { printf 'check-docs: %s\n' "$1" >&2; errors=$((errors + 1)); }

make_targets=$(awk -F: '/^[a-zA-Z0-9_.-]+:/{split($1, targets, " "); for (i in targets) print targets[i]}' Makefile | sort -u)
readme_make_targets=$(awk '
function print_targets(command, words, count, i) {
	count=split(command, words, /[[:space:]]+/)
	for (i=2; i<=count; i++) {
		if (words[i] ~ /^[a-z][a-z0-9-]*$/) print words[i]; else break
	}
}
{
	line=$0
	while (match(line, /`make [^`]+`/)) {
		command=substr(line, RSTART+1, RLENGTH-2)
		print_targets(command)
		line=substr(line, RSTART+RLENGTH)
	}
	if ($0 ~ /^```/) { in_code=!in_code; next }
	if (in_code && $0 ~ /^[[:space:]]*make[[:space:]]/) print_targets($0)
}' README.md | sort -u)
for target in $readme_make_targets; do
	printf '%s\n' "$make_targets" | grep -Fxq "$target" || fail "README names unknown make target: $target"
done

config_vars=$(sed -nE 's/.*(get|getenv|duration|durationRange|count|countRange)\("([A-Z][A-Z0-9_]+)".*/\2/p' internal/config/config.go | sort -u)
example_vars=$(sed -nE 's/^([A-Z][A-Z0-9_]*)=.*/\1/p' .env.example | sort -u)
compose_vars=$(sed -n '/^  hostbud:/,/^  hostbud-postgres:/p' docker-compose.yml deploy/compose.tailscale.yml | grep -Eo '[A-Z][A-Z0-9_]+' | sort -u)

for var in $config_vars; do
	printf '%s\n' "$example_vars" | grep -Fxq "$var" || fail "$var is read by internal/config but missing from .env.example"
	printf '%s\n' "$compose_vars" | grep -Fxq "$var" || fail "$var is read by internal/config but missing from hostbud Compose environment"
done

for var in $example_vars; do
	case "$var" in HOSTBUD_*) ;; *) continue ;; esac
	if ! grep -R -F -l -- "$var" internal docker-compose.yml deploy scripts >/dev/null 2>&1; then
		if ! awk -v key="$var" '
			/^#.*v2|^#.*unused/ { allowed=1 }
			/^# ──/ && $0 !~ /v2/ { allowed=0 }
			/^[A-Z][A-Z0-9_]*=/ && $0 ~ "^" key "=" && allowed { found=1 }
			END { exit !found }
		' .env.example; then
			fail "$var in .env.example is neither read nor documented as v2/unused"
		fi
	fi
done

# Every HTTP route in internal/api/testdata/routes.json (the router's own
# inventory) is listed in ARCHITECTURE §9. Path parameters compare by
# position ({id} and :id alike); "GET|PUT" and a last segment "a|b" expand.
normalize_route() { sed -E 's#\{[^}]*\}#:p#g; s#:[a-z_]+#:p#g'; }
documented_routes=$(sed -n '/^## 9\. API surface/,/^## 10\./p' docs/ARCHITECTURE.md | awk '
	/^(GET|POST|PUT|PATCH|DELETE)(\|(GET|POST|PUT|PATCH|DELETE))*[[:space:]]+\/(api|ws)\// {
		n = split($1, methods, "|")
		path = $2
		sub(/\?.*/, "", path)
		last = path
		sub(/.*\//, "", last)
		base = substr(path, 1, length(path) - length(last))
		m = split(last, tails, "|")
		for (i = 1; i <= n; i++) for (j = 1; j <= m; j++) print methods[i] " " base tails[j]
	}' | normalize_route | sort -u)
router_routes=$(sed -nE 's/.*"method":"([A-Z]+)","path":"(\/api\/[^"]*)".*/\1 \2/p' internal/api/testdata/routes.json | normalize_route | sort -u)
for route in $(printf '%s\n' "$router_routes" | tr ' ' '_'); do
	route=$(printf '%s' "$route" | tr '_' ' ')
	printf '%s\n' "$documented_routes" | grep -Fxq "$route" || fail "route $route (internal/api/testdata/routes.json) is missing from ARCHITECTURE §9"
done

links=$(sed -nE 's/.*\]\((docs\/[^)#]+)(#[^)]*)?\).*/\1/p' README.md)
for link in $links; do
	[ -f "$link" ] || fail "README links to missing file: $link"
done

if [ "$errors" -ne 0 ]; then
	printf 'check-docs: %s problem(s) found\n' "$errors" >&2
	exit 1
fi
printf 'check-docs: README targets, environment docs and links are consistent\n'
