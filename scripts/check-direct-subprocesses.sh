#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
docs="$root/docs/ARCHITECTURE.md"
failures=0

fail() {
  printf 'direct-subprocess-check: %s\n' "$*" >&2
  failures=$((failures + 1))
}

function_at_line() {
  local path="$1"
  local line="$2"
  awk -v limit="$line" '
    NR > limit { exit }
    /^func[[:space:]]+/ {
      candidate = $0
      sub(/^func[[:space:]]+/, "", candidate)
      sub(/^\([^)]*\)[[:space:]]*/, "", candidate)
      sub(/\(.*/, "", candidate)
      gsub(/[[:space:]]/, "", candidate)
      if (candidate != "") {
        name = candidate
      }
    }
    END {
      if (name != "") {
        print name
      }
    }
  ' "$path"
}

allowed_entries=(
  "internal/runner/runner.go:RunStreamingWithEnv|runner.Local.RunStreamingWithEnv"
  "internal/brewfile/brewfile.go:runCommand|brewfile.runCommand"
  "internal/brewfile/brewfile.go:runCommandQuiet|brewfile.runCommandQuiet"
  "internal/cmd/cmd.go:runLegacy|cmd.runLegacy"
  "internal/cmd/i18n.go:readGlobalDefault|cmd.readGlobalDefault"
  "internal/cmd/list.go:translateBatch|cmd.translateBatch"
  "internal/cmd/security_github.go:githubTokenFromCLI|cmd.githubTokenFromCLI"
  "internal/cmd/mutation.go:runEdit|cmd.runEdit"
  "internal/cmd/inventory_review.go:runManualAgentCommand|cmd.runManualAgentCommand"
  "internal/cmd/inventory_review.go:editManualOverrideBlock|cmd.editManualOverrideBlock"
)

is_allowed_key() {
  local want="$1"
  local entry key
  for entry in "${allowed_entries[@]}"; do
    key="${entry%%|*}"
    if [[ "$key" == "$want" ]]; then
      return 0
    fi
  done
  return 1
}

for entry in "${allowed_entries[@]}"; do
  label="${entry#*|}"
  if ! grep -Fq "$label" "$docs"; then
    fail "documented exception missing from docs/ARCHITECTURE.md: $label"
  fi
done

while IFS=: read -r path line _; do
  rel="${path#"$root"/}"
  fn="$(function_at_line "$path" "$line")"
  if [[ -z "$fn" ]]; then
    fail "could not resolve function for $rel:$line"
    continue
  fi
  key="$rel:$fn"
  if ! is_allowed_key "$key"; then
    fail "undocumented direct subprocess: $rel:$line in $fn"
  fi
done < <(
  grep -REn 'exec\.Command(Context)?\(' "$root/internal" "$root/main.go" 2>/dev/null || true
)

if [[ "$failures" -gt 0 ]]; then
  exit 1
fi

printf 'direct-subprocess-check: ok\n'
