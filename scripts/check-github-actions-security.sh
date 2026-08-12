#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
failures=0
warnings=0
details="${UPDEV_ACTIONS_SECURITY_DETAILS:-0}"

fail() {
  printf 'github-actions-security: %s\n' "$*" >&2
  failures=$((failures + 1))
}

warn() {
  if [[ "$details" == "1" ]]; then
    printf 'github-actions-security: warning: %s\n' "$*" >&2
  fi
  warnings=$((warnings + 1))
}

require_grep() {
  local pattern="$1"
  local path="$2"
  if ! grep -Eq "$pattern" "$root/$path"; then
    fail "missing pattern in $path: $pattern"
  fi
}

require_action_major() {
  local action="$1"
  local major="$2"
  if ! grep -RqsE "uses: ${action}@${major}([[:space:]]|$)" "$root/.github/workflows"; then
    fail "missing required action version: ${action}@${major}"
  fi
}

require_grep '^  contents: read$' ".github/workflows/ci.yml"
require_grep '^  contents: read$' ".github/workflows/codeql.yml"
require_grep '^      security-events: write$' ".github/workflows/codeql.yml"
require_grep '^  pull-requests: write$' ".github/workflows/dependency-review.yml"
require_grep '^  contents: write$' ".github/workflows/release.yml"
require_grep '^  id-token: write$' ".github/workflows/release.yml"
require_grep '^  attestations: write$' ".github/workflows/release.yml"
require_grep 'uses: goreleaser/goreleaser-action@v7' ".github/workflows/release.yml"
require_grep 'uses: actions/attest@v4' ".github/workflows/release.yml"
require_grep 'subject-checksums:' ".github/workflows/release.yml"
require_grep 'mise-version: \["2026\.8\.2", "latest"\]' ".github/workflows/ci.yml"
require_grep 'install: false' ".github/workflows/ci.yml"
require_grep 'cache: false' ".github/workflows/ci.yml"
require_grep 'env: false' ".github/workflows/ci.yml"

require_action_major "actions/checkout" "v6"
require_action_major "actions/setup-go" "v6"
require_action_major "jdx/mise-action" "v3"
require_action_major "goreleaser/goreleaser-action" "v7"
require_action_major "github/codeql-action/init" "v4"
require_action_major "github/codeql-action/analyze" "v4"
require_action_major "actions/dependency-review-action" "v5"
require_action_major "actions/attest" "v4"

while IFS= read -r line; do
  file="${line%%:*}"
  rest="${line#*:}"
  usage="${rest#*uses: }"
  action="${usage%@*}"
  action="${action%% *}"
  ref="${rest##*@}"
  ref="${ref%% *}"
  ref="${ref%%	*}"
  if [[ "$rest" != *"@"* ]]; then
    fail "$file uses an action without a pinned ref: $rest"
    continue
  fi
  case "$ref" in
    main | master | HEAD)
      fail "$file uses a mutable branch ref: $rest"
      ;;
    v[0-9]* | [0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]*)
      if [[ "$ref" != [0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]* ]]; then
        warn "$file uses version tag instead of immutable SHA: $rest"
      fi
      ;;
    *)
      warn "$file uses an uncommon action ref; review manually: $rest"
      ;;
  esac
  expected_major=""
  case "$action" in
    actions/checkout)
      expected_major="v6"
      ;;
    actions/setup-go)
      expected_major="v6"
      ;;
    jdx/mise-action)
      expected_major="v3"
      ;;
    goreleaser/goreleaser-action)
      expected_major="v7"
      ;;
    actions/attest)
      expected_major="v4"
      ;;
    github/codeql-action/init | github/codeql-action/autobuild | github/codeql-action/analyze)
      expected_major="v4"
      ;;
    actions/dependency-review-action)
      expected_major="v5"
      ;;
  esac
  if [[ -n "$expected_major" && "$ref" == v* && "$ref" != "$expected_major"* ]]; then
    fail "$file uses $action@$ref; expected $expected_major or a reviewed immutable SHA"
  fi
done < <(grep -RsnE 'uses: [^[:space:]]+' "$root/.github/workflows" || true)

if (( failures > 0 )); then
  exit 1
fi

if (( warnings > 0 )); then
  printf 'github-actions-security: ok with %d review note(s); set UPDEV_ACTIONS_SECURITY_DETAILS=1 for details\n' "$warnings"
else
  printf 'github-actions-security: ok\n'
fi
