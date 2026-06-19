#!/usr/bin/env bash
set -euo pipefail

version="v1.2.0"
cache_base="${UPDEV_AUDIT_CACHE_DIR:-${TMPDIR:-/tmp}/updev-audit}"
cache_root="$cache_base/govulncheck-$version"
mkdir -p "$cache_root"

export GOPATH="$cache_root/gopath"
export GOCACHE="$cache_root/gocache"
export GOMODCACHE="$cache_root/gomodcache"
export GOBIN="$cache_root/bin"

go install "golang.org/x/vuln/cmd/govulncheck@${version}"
"$GOBIN/govulncheck" ./...

printf 'govulncheck %s: ok\n' "$version"
