#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "$0")/lib/go-tool-cache.sh"

gofumpt_version="v0.9.2"
goimports_version="v0.46.0"
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

gofumpt="$(updev_go_tool gofumpt mvdan.cc/gofumpt "$gofumpt_version")"
goimports="$(updev_go_tool goimports golang.org/x/tools/cmd/goimports "$goimports_version")"

gofumpt_files="$(
  "$gofumpt" -l "${files[@]}"
)"
goimports_files="$(
  "$goimports" -local "$module_path" -l "${files[@]}"
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
