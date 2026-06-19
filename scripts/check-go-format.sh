#!/usr/bin/env bash
set -euo pipefail

gofumpt_version="v0.9.2"
goimports_version="v0.46.0"
cache_base="${UPDEV_LINT_CACHE_DIR:-${TMPDIR:-/tmp}/updev-lint}"
cache_root="$cache_base/fmt"
mkdir -p "$cache_root"

export GOPATH="$cache_root/gopath"
export GOCACHE="$cache_root/gocache"
export GOMODCACHE="$cache_root/gomodcache"

module_path="$(
  awk '$1 == "module" { print $2; exit }' go.mod
)"
if [[ -z "$module_path" ]]; then
  printf 'go format check failed: could not detect module path from go.mod\n' >&2
  exit 1
fi

files=()
while IFS= read -r -d '' file; do
  files+=("$file")
done < <(find . -type f -name '*.go' -not -path './.git/*' -print0)

if (( ${#files[@]} == 0 )); then
  printf 'go format check: ok\n'
  exit 0
fi

gofumpt_files="$(
  go run "mvdan.cc/gofumpt@${gofumpt_version}" -l "${files[@]}"
)"
goimports_files="$(
  go run "golang.org/x/tools/cmd/goimports@${goimports_version}" -local "$module_path" -l "${files[@]}"
)"

failed=0
if [[ -n "$gofumpt_files" ]]; then
  printf 'go format check failed; run gofumpt on:\n%s\n' "$gofumpt_files" >&2
  failed=1
fi
if [[ -n "$goimports_files" ]]; then
  printf 'go import check failed; run goimports -local %s on:\n%s\n' "$module_path" "$goimports_files" >&2
  failed=1
fi
if (( failed != 0 )); then
  exit 1
fi

printf 'go format check: ok (gofumpt %s, goimports %s)\n' "$gofumpt_version" "$goimports_version"
