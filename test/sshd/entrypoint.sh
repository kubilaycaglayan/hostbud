#!/bin/sh
# Starts sshd with the per-run keys from /keys (written by keygen.sh):
#   /keys/host/ssh_host_*_key[.pub]  host keys (generated here if absent)
#   /keys/client/id_ed25519.pub      authorized for user dev
# sshd runs as a daemon so sshd-ctl can stop/start it without ending the
# container (the "host unreachable" scenarios).
set -eu

if ls /keys/host/ssh_host_*_key >/dev/null 2>&1; then
	cp /keys/host/ssh_host_*_key /keys/host/ssh_host_*_key.pub /etc/ssh/
	chmod 600 /etc/ssh/ssh_host_*_key
else
	ssh-keygen -A >/dev/null
fi

install -d -o dev -g dev -m 700 /home/dev/.ssh
if [ -n "${AUTHORIZED_KEY:-}" ]; then
	printf '%s\n' "$AUTHORIZED_KEY" >/home/dev/.ssh/authorized_keys
elif [ -f /keys/client/id_ed25519.pub ]; then
	cp /keys/client/id_ed25519.pub /home/dev/.ssh/authorized_keys
else
	echo "entrypoint: no client key (set AUTHORIZED_KEY or mount /keys/client)" >&2
	exit 1
fi
chown dev:dev /home/dev/.ssh/authorized_keys
chmod 600 /home/dev/.ssh/authorized_keys

/usr/local/bin/sshd-ctl.sh start
exec sleep infinity
