#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "$0")/lib/go-tool-cache.sh"

version="v1.2.0"
govulncheck="$(updev_go_tool govulncheck golang.org/x/vuln/cmd/govulncheck "$version")"
cache_base="${UPDEV_AUDIT_CACHE_DIR:-$(updev_lint_cache_base)}"
updev_prepare_lint_cache "$cache_base"
run_root="$cache_base/govulncheck-run/$(go env GOVERSION)"
mkdir -p "$run_root"

GOCACHE="$run_root/gocache" "$govulncheck" ./...

printf 'govulncheck %s: ok\n' "$version"
