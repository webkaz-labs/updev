#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "$0")/lib/go-tool-cache.sh"

cache_base="$(updev_lint_cache_base)"
updev_prepare_lint_cache "$cache_base"
cache_root="$cache_base/staticcheck-run/$(go env GOVERSION)"
updev_prepare_go_analysis_cache "$cache_root"

export GOPATH="$cache_root/gopath"
export GOCACHE="$cache_root/gocache"
export GOMODCACHE="$cache_root/gomodcache"
export STATICCHECK_CACHE="$cache_root/staticcheck"

staticcheck="$(updev_go_tool staticcheck honnef.co/go/tools/cmd/staticcheck v0.6.0)"
"$staticcheck" '-checks=SA*,U1000' ./...

printf 'staticcheck SA*,U1000: ok\n'
