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
rotate-hostkey)
	ssh-keygen -q -t ed25519 -N '' -f /tmp/hostbud-rotated-ed25519
	cp /tmp/hostbud-rotated-ed25519 /etc/ssh/ssh_host_ed25519_key
	cp /tmp/hostbud-rotated-ed25519.pub /etc/ssh/ssh_host_ed25519_key.pub
	cp /tmp/hostbud-rotated-ed25519.pub /keys/hostpub/ssh_host_ed25519_key.pub
	chmod 600 /etc/ssh/ssh_host_ed25519_key
	"$0" stop
	"$0" start
	;;
restore-hostkey)
	cp /keys/host/ssh_host_ed25519_key /etc/ssh/ssh_host_ed25519_key
	cp /keys/host/ssh_host_ed25519_key.pub /etc/ssh/ssh_host_ed25519_key.pub
	cp /keys/host/ssh_host_ed25519_key.pub /keys/hostpub/ssh_host_ed25519_key.pub
	chmod 600 /etc/ssh/ssh_host_ed25519_key
	"$0" stop
	"$0" start
	rm -f /tmp/hostbud-rotated-ed25519 /tmp/hostbud-rotated-ed25519.pub
	;;
stop)
	pkill -x sshd || true
	pkill -f '^sshd' || true
	;;
stall)
	case "${2:-}" in tmux|sftp) kind=$2 ;; *) echo "usage: sshd-ctl.sh stall tmux|sftp <seconds>|off" >&2; exit 2 ;; esac
	stall_dir=/home/dev/.hostbud-stall
	install -d -o dev -g dev -m 700 "$stall_dir"
	stall_file=$stall_dir/$kind
	case "${3:-}" in
		off) rm -f "$stall_file" ;;
		*[!0-9]*|'') echo "usage: sshd-ctl.sh stall tmux|sftp <seconds>|off" >&2; exit 2 ;;
		*) seconds=$3; if [ "$seconds" -gt 60 ]; then seconds=60; fi
		   deadline=$(($(date +%s) + seconds)); printf '%s\n' "$deadline" >"$stall_file"; chown dev:dev "$stall_file"
		   # An open SFTP connection would bypass the switch: drop it, like a hung host.
		   if [ "$kind" = sftp ]; then pkill -x sftp-server || true; fi ;;
	esac
	;;
*)
	echo "usage: sshd-ctl.sh start|stop|rotate-hostkey|restore-hostkey|stall tmux|sftp <seconds>|off" >&2
	exit 2
	;;
esac
