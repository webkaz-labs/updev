#!/usr/bin/env bash
set -euo pipefail

version="v2.12.2"
cache_root="${UPDEV_LINT_CACHE_DIR:-${TMPDIR:-/tmp}/updev-lint}"
mkdir -p "$cache_root"

export GOPATH="$cache_root/gopath"
export GOCACHE="$cache_root/gocache"
export GOMODCACHE="$cache_root/gomodcache"
export GOLANGCI_LINT_CACHE="$cache_root/golangci-lint"

use_local=0
if command -v golangci-lint >/dev/null 2>&1; then
  local_version="$(golangci-lint version 2>/dev/null || true)"
  if [[ "$local_version" == *"version 2."* ]]; then
    use_local=1
  fi
fi

if (( use_local == 1 )); then
  golangci-lint run ./...
else
  go run "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${version}" run ./...
fi

printf 'golangci-lint %s: ok\n' "$version"
