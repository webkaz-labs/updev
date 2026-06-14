#!/usr/bin/env bash
set -euo pipefail

cache_root="${UPDEV_LINT_CACHE_DIR:-${TMPDIR:-/tmp}/updev-lint}"
mkdir -p "$cache_root"

export GOPATH="$cache_root/gopath"
export GOCACHE="$cache_root/gocache"
export GOMODCACHE="$cache_root/gomodcache"
export STATICCHECK_CACHE="$cache_root/staticcheck"

go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 '-checks=SA*' ./...

printf 'staticcheck SA*: ok\n'
