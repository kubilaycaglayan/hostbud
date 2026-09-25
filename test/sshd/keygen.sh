#!/bin/sh
# keygen.sh <client-uid> — writes a fresh set of throwaway keys into /keys:
#   /keys/host/     target host keys (private + public)
#   /keys/hostpub/  public host keys only (what the app pins)
#   /keys/client/   client key, owned by <client-uid> (the ssh-agent's user)
#   /keys/known_hosts  pinned keys for the e2e runner's own ssh client
# and hands /agent to <client-uid> for the agent socket.
set -eu
uid="${1:?usage: keygen.sh <client-uid>}"

# /keys/hostpub may be its own volume (a mount point): empty it, don't remove it.
rm -rf /keys/host /keys/client /keys/known_hosts
rm -f /keys/hostpub/*
mkdir -p /keys/host /keys/hostpub /keys/client /tmp/root/etc/ssh

ssh-keygen -A -f /tmp/root >/dev/null
cp /tmp/root/etc/ssh/ssh_host_*_key /tmp/root/etc/ssh/ssh_host_*_key.pub /keys/host/
cp /tmp/root/etc/ssh/ssh_host_*_key.pub /keys/hostpub/
chmod 644 /keys/hostpub/*.pub

ssh-keygen -q -t ed25519 -N '' -C hostbud-e2e -f /keys/client/id_ed25519
chown -R "$uid:$uid" /keys/client
chmod 700 /keys/client

for f in /keys/hostpub/*.pub; do
	printf 'hostbud-e2e-target,hostbud-e2e-target-notmux %s\n' "$(cut -d' ' -f1,2 "$f")"
done >/keys/known_hosts

# Host keys and known_hosts belong to <client-uid> too, so a bind-mounted
# /keys (integration tests) can be removed without root. The target copies
# the host keys and fixes their modes itself.
chown -R "$uid:$uid" /keys/host /keys/hostpub /keys/known_hosts

if [ -d /agent ]; then
	chown "$uid:$uid" /agent
	chmod 700 /agent
fi
