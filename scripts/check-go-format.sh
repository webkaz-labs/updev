#!/usr/bin/env bash
set -euo pipefail

files="$(gofmt -l .)"
if [[ -n "$files" ]]; then
  printf 'go format check failed; run gofmt on:\n%s\n' "$files" >&2
  exit 1
fi

printf 'go format check: ok\n'
