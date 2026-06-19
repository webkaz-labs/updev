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
go_cache="${GOCACHE:-${TMPDIR:-/tmp}/updev-gocache}"
if ! diff -u "$root/docs/agent/SKILL.md" <(cd "$root" && GOCACHE="$go_cache" go run . skill); then
  fail "embedded updev skill output drifted from docs/agent/SKILL.md"
fi
if ! diff -u "$root/docs/agent/USAGE.md" <(cd "$root" && GOCACHE="$go_cache" go run . help agent); then
  fail "embedded updev help agent output drifted from docs/agent/USAGE.md"
fi
if [[ -f "$root/docs/PUBLISHING.md" ]]; then
  require_grep 'docs/release-notes/<tag>\.md' "docs/DESIGN.md"
fi

require_grep 'uses: actions/setup-go@v6' ".github/workflows/ci.yml"
require_grep 'run: scripts/check-docs\.sh' ".github/workflows/ci.yml"
require_grep 'run: scripts/check-go-format\.sh' ".github/workflows/ci.yml"
require_grep 'run: scripts/check-staticcheck\.sh' ".github/workflows/ci.yml"
require_grep 'run: scripts/check-golangci-lint\.sh' ".github/workflows/ci.yml"
require_grep 'shellcheck -S warning scripts/\*\.sh' ".github/workflows/ci.yml"
require_grep 'run: go mod verify' ".github/workflows/ci.yml"
require_grep 'run: go vet \./\.\.\.' ".github/workflows/ci.yml"
require_grep 'run: go test \./\.\.\.' ".github/workflows/ci.yml"
require_grep 'run: go build \./\.\.\.' ".github/workflows/ci.yml"
require_file ".github/dependabot.yml"
require_file ".github/workflows/audit.yml"
require_file ".github/workflows/codeql.yml"
require_file ".github/workflows/dependency-review.yml"
require_grep 'uses: github/codeql-action/init@v4' ".github/workflows/codeql.yml"
require_grep 'uses: github/codeql-action/analyze@v4' ".github/workflows/codeql.yml"
require_grep 'uses: actions/dependency-review-action@v5' ".github/workflows/dependency-review.yml"
require_grep 'run: scripts/check-govulncheck\.sh' ".github/workflows/audit.yml"
require_grep 'run: scripts/check-github-actions-security\.sh' ".github/workflows/audit.yml"
require_grep 'run: scripts/check-agent-quality\.sh' ".github/workflows/audit.yml"
require_grep 'aislop@\$\{aislop_version\}' "scripts/check-agent-quality.sh"
require_file ".goreleaser.yml"
require_grep 'uses: goreleaser/goreleaser-action@v7' ".github/workflows/release.yml"
require_grep 'version: "~> v2"' ".github/workflows/release.yml"
require_grep 'uses: actions/attest@v4' ".github/workflows/release.yml"
require_grep 'subject-checksums: \./dist/checksums\.txt' ".github/workflows/release.yml"
require_grep 'name_template: checksums\.txt' ".goreleaser.yml"
if [[ -f "$root/AGENTS.md" ]]; then
  require_grep 'mise -C tools/updev run audit' "AGENTS.md"
  require_grep 'mise -C tools/updev run docs-check' "AGENTS.md"
else
  require_grep 'mise run audit' "README.md"
  require_grep 'mise run docs-check' "README.md"
fi
require_grep 'depends = \["fmt-check", "staticcheck", "golangci-lint", "shellcheck"\]' "mise.toml"
require_grep 'depends = \["lint", "test", "vet", "mod-verify", "build"\]' "mise.toml"
require_grep 'depends = \["vuln", "supply-chain", "agent-quality"\]' "mise.toml"

"$root/scripts/check-direct-subprocesses.sh" || failures=$((failures + 1))
"$root/scripts/check-source-structure.sh" || failures=$((failures + 1))

while IFS=$'\t' read -r md_rel link; do
  target="${link%%#*}"
  target="${target%% \"*}"
  target="${target%% \'*}"
  target="${target#<}"
  target="${target%>}"
  if [[ -z "$target" || "$target" == \#* ]]; then
    continue
  fi
  if [[ "$target" =~ ^[A-Za-z][A-Za-z0-9+.-]*: || "$target" == /* ]]; then
    continue
  fi
  md_dir="$(dirname "$md_rel")"
  if ! (cd "$root/$md_dir" && [[ -e "$target" ]]); then
    fail "$md_rel link target does not exist: $link"
  fi
done < <(
  find "$root" -path "$root/.git" -prune -o \( -name README.md -o -path "$root/docs/*.md" -o -path "$root/docs/*/*.md" \) -type f -print |
    while IFS= read -r md; do
      md_rel="${md#"$root"/}"
      while IFS= read -r link; do
        printf '%s\t%s\n' "$md_rel" "$link"
      done < <(
        grep -Eo '\[[^]]+\]\([^)]+\)' "$md" |
          sed -E 's/^.*\(([^)]+)\)$/\1/' ||
          true
      )
    done
)

if [[ "$failures" -gt 0 ]]; then
  exit 1
fi

printf 'docs-check: ok\n'
