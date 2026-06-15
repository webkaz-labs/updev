#!/usr/bin/env bash
set -euo pipefail

version="v1.3.0"
cache_root="${UPDEV_AUDIT_CACHE_DIR:-${TMPDIR:-/tmp}/updev-audit}"
mkdir -p "$cache_root"

export GOPATH="$cache_root/gopath"
export GOCACHE="$cache_root/gocache"
export GOMODCACHE="$cache_root/gomodcache"

go run "golang.org/x/vuln/cmd/govulncheck@${version}" ./...

printf 'govulncheck %s: ok\n' "$version"
