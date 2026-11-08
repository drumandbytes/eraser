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
	# Write it whole under a temp name, then hard-link it into place: ln
	# never replaces an existing file, so when two containers start on a
	# fresh volume at once the first ID wins and nobody sees a partial one.
	umask 077
	tmp="$id_file.$$"
	tr -d '-' </proc/sys/kernel/random/uuid >"$tmp"
	ln "$tmp" "$id_file" 2>/dev/null || true
	rm -f "$tmp"
	[ -s "$id_file" ] || { echo "eraser: can't create $id_file" >&2; exit 1; }
fi

exec eraser "$@"
