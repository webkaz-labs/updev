#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
doc="$root/docs/SOURCE-STRUCTURE.md"
failures=0

fail() {
  printf 'source-structure-check: %s\n' "$*" >&2
  failures=$((failures + 1))
}

go_file_count() {
  local dir="$1"
  find "$dir" -maxdepth 1 -type f -name '*.go' | wc -l | tr -d ' '
}

if [[ ! -f "$doc" ]]; then
  fail "missing docs/SOURCE-STRUCTURE.md"
else
  require_pattern() {
    local pattern="$1"
    if ! grep -Eq "$pattern" "$doc"; then
      fail "docs/SOURCE-STRUCTURE.md missing pattern: $pattern"
    fi
  }

  cmd_count="$(go_file_count "$root/internal/cmd")"
  cmd_ceiling=50
  cmd_status="within target"
  if (( cmd_count > cmd_ceiling )); then
    cmd_status="over target"
  fi
  require_pattern "\| \`internal/cmd\` \| ${cmd_count} \| <= 50 \| ${cmd_ceiling} \| ${cmd_status} \|"
  if (( cmd_count > cmd_ceiling )); then
    fail "internal/cmd has ${cmd_count} Go files; ceiling is ${cmd_ceiling}"
  fi

  while IFS= read -r dir; do
    package="${dir#"$root/"}"
    count="$(go_file_count "$dir")"
    if [[ "$package" == "internal/cmd" ]]; then
      continue
    fi
    if (( count > 20 )); then
      fail "$package has ${count} Go files; package budget is 20"
    fi
    if (( count >= 5 )); then
      require_pattern "\| \`${package}\` \| ${count} \| ok \|"
    fi
  done < <(find "$root/internal" -mindepth 1 -maxdepth 1 -type d | sort)

  require_pattern "\| all other \`internal/\*\` packages \| <= 20 each \| <= 20 \| 20 \| within target \|"
fi

if (( failures > 0 )); then
  exit 1
fi

printf 'source-structure-check: ok\n'
