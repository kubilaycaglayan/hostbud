#!/bin/sh
# sshd-ctl.sh start|stop|stall tmux <seconds>|off — manage the disposable
# target and its bounded failure switches.
# connection (so ControlMaster sockets die too, like a real outage).
set -eu
case "${1:-}" in
start)
	mkdir -p /run/sshd
	/usr/sbin/sshd -E /var/log/sshd.log
	;;
stop)
	pkill -x sshd || true
	pkill -f '^sshd' || true
	;;
stall)
	if [ "${2:-}" != tmux ]; then echo "usage: sshd-ctl.sh stall tmux <seconds>|off" >&2; exit 2; fi
	stall_dir=/home/dev/.hostbud-stall
	install -d -o dev -g dev -m 700 "$stall_dir"
	case "${3:-}" in
		off) rm -f "$stall_dir/tmux" ;;
		*[!0-9]*|'') echo "usage: sshd-ctl.sh stall tmux <seconds>|off" >&2; exit 2 ;;
		*) seconds=$3; if [ "$seconds" -gt 60 ]; then seconds=60; fi
		   deadline=$(($(date +%s) + seconds)); printf '%s\n' "$deadline" >"$stall_dir/tmux"; chown dev:dev "$stall_dir/tmux" ;;
	esac
	;;
*)
	echo "usage: sshd-ctl.sh start|stop|stall tmux <seconds>|off" >&2
	exit 2
	;;
esac
