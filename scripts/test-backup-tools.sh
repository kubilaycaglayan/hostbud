#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
mkdir -p "$tmp/bin"
cat >"$tmp/bin/docker" <<'MOCK'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$MOCK_DOCKER_LOG"
case "${MOCK_MODE:-normal}:$*" in
	format-error:*"pg_restore --list"*) exit 1 ;;
	foreign-dump:*"pg_restore --list"*) echo 'TABLE public unrelated'; exit 0 ;;
	*"pg_restore --list"*) echo 'TABLE public goose_db_version'; exit 0 ;;
	*"printenv HOSTBUD_DB_NAME"*) echo hostbud_test; exit 0 ;;
	check-fail:*createdb*) exit 1 ;;
	check-fail:*"hostbud restore-check"*) exit 1 ;;
	*) exit 0 ;;
esac
MOCK
chmod +x "$tmp/bin/docker"
export PATH="$tmp/bin:$PATH" MOCK_DOCKER_LOG="$tmp/docker.log"
touch "$tmp/source.dump"

: >"$MOCK_DOCKER_LOG"
if make -C "$root" restore FILE= >/dev/null 2>&1; then echo 'restore accepted a missing FILE' >&2; exit 1; fi
if make -C "$root" restore-check FILE= >/dev/null 2>&1; then echo 'restore-check accepted a missing FILE' >&2; exit 1; fi
[ ! -s "$MOCK_DOCKER_LOG" ] || { echo 'missing FILE reached docker' >&2; exit 1; }

for mode in format-error foreign-dump; do
	: >"$MOCK_DOCKER_LOG"
	if MOCK_MODE="$mode" "$root/scripts/restore.sh" "$tmp/source.dump" hostbud_test >/dev/null 2>&1; then echo "restore accepted $mode" >&2; exit 1; fi
	if grep -Eq 'stop hostbud|pg_restore --clean|hostbud backup' "$MOCK_DOCKER_LOG"; then echo "$mode caused a database-side effect" >&2; exit 1; fi
done

: >"$MOCK_DOCKER_LOG"
if "$root/scripts/restore.sh" "$tmp/source.dump" wrong >/dev/null 2>&1; then echo 'restore accepted wrong confirmation' >&2; exit 1; fi
if grep -Eq 'stop hostbud|pg_restore --clean|hostbud backup' "$MOCK_DOCKER_LOG"; then echo 'wrong confirmation caused a database-side effect' >&2; exit 1; fi

for mode in check-ok check-fail; do
	: >"$MOCK_DOCKER_LOG"
	MOCK_MODE="$mode" "$root/scripts/restore-check.sh" "$tmp/source.dump" >/dev/null 2>&1 || [ "$mode" = check-fail ]
	grep -q 'hostbud restore-check' "$MOCK_DOCKER_LOG" || { echo "restore-check did not invoke isolated restore command ($mode)" >&2; exit 1; }
	grep -q 'rm -f /tmp/hostbud-restore-check-' "$MOCK_DOCKER_LOG" || { echo "restore-check did not schedule dump-file cleanup ($mode)" >&2; exit 1; }
done

echo 'backup tool argument and cleanup checks passed'
