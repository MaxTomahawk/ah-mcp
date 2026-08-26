#!/bin/sh
set -eu

mkdir -p /data
chown -R ahmcp:ahmcp /data

exec su-exec ahmcp:ahmcp "$@"
