#!/bin/sh
# Temporary failure switch for the disposable test host.
set -eu
deadline_file=/home/dev/.hostbud-stall/tmux
if [ -r "$deadline_file" ]; then
	deadline=$(cat "$deadline_file" 2>/dev/null || true)
	now=$(date +%s)
	case "$deadline" in
		''|*[!0-9]*) ;;
		*) if [ "$deadline" -gt "$now" ]; then sleep "$((deadline - now))"; else rm -f "$deadline_file"; fi ;;
	esac
fi
exec /usr/bin/tmux "$@"
