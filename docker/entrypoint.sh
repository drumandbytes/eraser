#!/bin/sh
# Container entrypoint: give Eraser a stable machine ID, then run it.
#
# Mail passwords in config.yaml are encrypted with a key derived from
# /etc/machine-id (internal/config/secret.go). A container has none and its
# hostname changes on every `docker run`/recreate, so without this a password
# saved by `serve` couldn't be decrypted by the next container. The image
# links /etc/machine-id to this file, kept in the data volume next to the
# config it unlocks. See docs/docker.md for what that does and doesn't protect.
set -eu

id_file="$HOME/.eraser/machine-id"
if [ ! -s "$id_file" ]; then
	umask 077
	tr -d '-' </proc/sys/kernel/random/uuid >"$id_file"
fi

exec eraser "$@"
