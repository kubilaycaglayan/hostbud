#!/bin/sh
# sshd-ctl.sh start|stop — start sshd, or stop it together with every open
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
*)
	echo "usage: sshd-ctl.sh start|stop" >&2
	exit 2
	;;
esac
