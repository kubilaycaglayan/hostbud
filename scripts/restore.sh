#!/bin/sh
set -eu

file=${1:-}
confirmation=${2:-}
if [ -z "$file" ] || [ ! -f "$file" ]; then
	echo 'restore: FILE must name an existing dump' >&2
	exit 2
fi

suffix=$(date -u +%Y%m%d%H%M%S)-$$
container_file="/tmp/hostbud-restore-$suffix.dump"
list_file=$(mktemp)
db=''
safety_name=''
restore_committed=0
cleanup() {
	docker compose exec -T hostbud-postgres rm -f "$container_file" >/dev/null 2>&1 || true
	rm -f "$list_file"
}
recovery() {
	if [ "$restore_committed" -eq 1 ]; then
		echo "The restore transaction committed, but hostbud did not recover. Safety backup: backups/$safety_name" >&2
		echo "After checking the app and database, restore it with: make restore FILE=backups/$safety_name CONFIRM=$db" >&2
	fi
}
trap cleanup EXIT

docker compose cp "$file" "hostbud-postgres:$container_file" >/dev/null
if ! docker compose exec -T hostbud-postgres pg_restore --list "$container_file" >"$list_file"; then
	echo 'restore: FILE is not a PostgreSQL custom-format dump' >&2
	exit 2
fi
if ! grep -Eq 'TABLE[[:space:]]+[^[:space:]]+[[:space:]]+goose_db_version([[:space:]]|$)' "$list_file"; then
	echo 'restore: dump does not contain the hostbud goose_db_version table' >&2
	exit 2
fi

db=$(docker compose exec -T hostbud printenv HOSTBUD_DB_NAME | tr -d '\r\n')
if [ -z "$db" ]; then echo 'restore: could not read the configured database name' >&2; exit 1; fi
printf 'This replaces every object in database %s from %s. A safety backup will be written first.\n' "$db" "$file"
if [ -z "$confirmation" ]; then
	if [ ! -t 0 ]; then echo "restore: type the database name or pass CONFIRM=$db" >&2; exit 2; fi
	printf 'Type %s to continue: ' "$db"
	IFS= read -r confirmation || true
fi
if [ "$confirmation" != "$db" ]; then echo 'restore: confirmation did not match; no database changes made' >&2; exit 2; fi

safety_output=$(make backup)
printf '%s\n' "$safety_output"
safety_name=$(printf '%s\n' "$safety_output" | sed -n 's/^Backup written: //p' | sed 's/ .*//')
if [ -z "$safety_name" ]; then echo 'restore: safety backup did not report its filename; database was not stopped' >&2; exit 1; fi

docker compose stop hostbud
if ! docker compose exec -T hostbud-postgres sh -ec '
	export PGPASSWORD="$POSTGRES_PASSWORD"
	pg_restore --clean --if-exists --single-transaction --no-owner --exit-on-error --no-password \
		-h 127.0.0.1 -p 5432 -U "$POSTGRES_USER" --dbname="$1" "$2"
' sh "$db" "$container_file"; then
	docker compose start hostbud || true
	echo 'restore: pg_restore failed; its single transaction rolled back, so the database is unchanged' >&2
	exit 1
fi
restore_committed=1

if ! docker compose start hostbud; then recovery; exit 1; fi
deadline=$(( $(date +%s) + 90 ))
while [ "$(date +%s)" -lt "$deadline" ]; do
	if docker compose exec -T hostbud hostbud healthcheck >/dev/null 2>&1; then
		echo "Restore completed and hostbud is healthy. Safety backup: backups/$safety_name"
		exit 0
	fi
	sleep 1
done
recovery
exit 1
