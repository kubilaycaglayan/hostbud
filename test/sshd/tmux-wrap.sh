#!/bin/sh
# Temporary failure switch for the disposable test host.
set -eu
deadline_file=/home/dev/.hostbud-stall/tmux
if [ -r "$deadline_file" ]; then
	while [ -r "$deadline_file" ]; do
		deadline=$(cat "$deadline_file" 2>/dev/null || true)
		now=$(date +%s)
		case "$deadline" in
			''|*[!0-9]*) break ;;
			*) if [ "$deadline" -le "$now" ]; then rm -f "$deadline_file"; break; fi ;;
		esac
		sleep 1
	done
fi
exec /usr/bin/tmux "$@"
