#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "$0")/lib/go-tool-cache.sh"

version="v2.6.2"
cache_base="$(updev_lint_cache_base)"
updev_prepare_lint_cache "$cache_base"
cache_root="$cache_base/golangci-run/$(go env GOVERSION)"
updev_prepare_go_analysis_cache "$cache_root"

export GOPATH="$cache_root/gopath"
export GOCACHE="$cache_root/gocache"
export GOMODCACHE="$cache_root/gomodcache"
export GOLANGCI_LINT_CACHE="$cache_root/golangci-lint"

golangci_lint="$(updev_go_tool golangci-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint "$version")"
"$golangci_lint" run ./...

printf 'golangci-lint %s: ok\n' "$version"
