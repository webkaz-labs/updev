#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
failures=0

fail() {
  printf 'docs-check: %s\n' "$*" >&2
  failures=$((failures + 1))
}

require_file() {
  local path="$1"
  if [[ ! -f "$root/$path" ]]; then
    fail "missing required file: $path"
  fi
}

require_grep() {
  local pattern="$1"
  local path="$2"
  if ! grep -Eq "$pattern" "$root/$path"; then
    fail "missing pattern in $path: $pattern"
  fi
}

current_version="$(
  sed -n 's/^The current implemented release is `updev \(v[0-9][^`]*\)`.*/\1/p' "$root/docs/RELEASE.md" |
    head -n 1
)"
if [[ -z "$current_version" ]]; then
  fail "could not find current release version in docs/RELEASE.md"
else
  require_file "docs/release-notes/${current_version}.md"
  require_grep "^# updev ${current_version}$" "docs/release-notes/${current_version}.md"
fi

require_file "docs/agent/USAGE.md"
require_file "docs/agent/SKILL.md"
require_grep 'docs/agent/' "docs/DESIGN.md"
require_grep 'docs/release-notes/<tag>\.md' "docs/DESIGN.md"

require_grep 'uses: actions/setup-go@v6' ".github/workflows/ci.yml"
require_grep 'run: scripts/check-docs\.sh' ".github/workflows/ci.yml"
require_grep 'run: go mod verify' ".github/workflows/ci.yml"
require_grep 'run: go vet \./\.\.\.' ".github/workflows/ci.yml"
require_grep 'run: go test \./\.\.\.' ".github/workflows/ci.yml"
require_grep 'run: go build \./\.\.\.' ".github/workflows/ci.yml"
require_grep 'depends = \["test", "vet", "mod-verify", "build"\]' "mise.toml"

while IFS= read -r link; do
  target="${link%%#*}"
  if [[ -z "$target" ]]; then
    continue
  fi
  if [[ "$target" == http://* || "$target" == https://* || "$target" == mailto:* ]]; then
    continue
  fi
  if [[ "$target" == /* ]]; then
    continue
  fi
  if [[ "$target" == docs/* || "$target" == README.md ]]; then
    if [[ ! -e "$root/$target" ]]; then
      fail "README link target does not exist: $target"
    fi
  fi
done < <(
  grep -Eo '\[[^]]+\]\([^)]+\)' "$root/README.md" |
    sed -E 's/^.*\(([^)]+)\)$/\1/'
)

if [[ "$failures" -gt 0 ]]; then
  exit 1
fi

printf 'docs-check: ok\n'
