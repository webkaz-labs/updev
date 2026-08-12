#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
document="${1:-$root/docs/VALIDATION.md}"
block="${2:-release-smoke}"

if [[ ! -f "$document" || -z "$block" ]]; then
  echo "usage: check-validation-blocks.sh [VALIDATION.md] [block]" >&2
  exit 64
fi

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/updev-validation.XXXXXX")"
tmp="$tmp_dir/commands"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

export GOCACHE="${GOCACHE:-${TMPDIR:-/tmp}/updev-gocache}"
mkdir -p "$GOCACHE"

if ! awk -v marker="<!-- validation:$block -->" '
  $0 == marker { found++; waiting=1; next }
  waiting && $0 == "```bash" { capture=1; waiting=0; next }
  capture && $0 == "```" { capture=0; done=1; next }
  capture { print }
  END {
    if (found != 1 || !done || capture) exit 64
  }
' "$document" >"$tmp"; then
  echo "validation-blocks: malformed or duplicate block: $block" >&2
  exit 64
fi

if grep -Eiq '^[[:space:]]*#[[:space:]]*(assert|expect|verify|check|confirm|ensure)[[:space:]]*:' "$tmp"; then
  echo "validation-blocks: assertion labels must be executable commands: $block" >&2
  exit 64
fi

commands=0
completed=0
line_number=0
while IFS= read -r line || [[ -n "$line" ]]; do
  line_number=$((line_number + 1))
  trimmed="${line#"${line%%[![:space:]]*}"}"
  if [[ -z "$trimmed" || "$trimmed" == \#* ]]; then
    continue
  fi
  if [[ "$trimmed" == *\\ || "$trimmed" == *"<<"* || "$trimmed" =~ ^exit([[:space:]]|$) ]]; then
    echo "validation-blocks: unsupported line $line_number in $block: $line" >&2
    exit 64
  fi
  commands=$((commands + 1))
  if ! (cd "$root" && bash -euo pipefail -c "$line"); then
    echo "validation-blocks: FAIL $block:$line_number: $line" >&2
    exit 1
  fi
  completed=$((completed + 1))
  printf 'validation-blocks: PASS %s:%d\n' "$block" "$line_number"
done <"$tmp"

if [[ "$commands" -eq 0 || "$completed" -ne "$commands" ]]; then
  echo "validation-blocks: block did not complete: $block" >&2
  exit 64
fi

printf 'validation-blocks: %s ok (%d commands)\n' "$block" "$completed"
