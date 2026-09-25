#!/bin/sh
# go-toolbox-image.sh <golang-image> — prints the Go toolbox image name,
# building it first if needed. The tag is a hash of the inputs, so changing
# the base image or the Dockerfile yields a new name (and tool.sh recreates
# the toolbox container).
set -eu
base="$1"
dir="$(cd "$(dirname "$0")" && pwd)"
uid="$(id -u)" gid="$(id -g)"
tag="$( (cat "$dir/toolbox/go.Dockerfile"; echo "$base $uid $gid") | sha256sum | cut -c1-12)"
image="hostbud-toolbox-go:$tag"
if ! docker image inspect "$image" >/dev/null 2>&1; then
	docker build -q -t "$image" --build-arg GO_IMAGE="$base" --build-arg UID="$uid" --build-arg GID="$gid" \
		-f "$dir/toolbox/go.Dockerfile" "$dir/toolbox" >&2
fi
echo "$image"
