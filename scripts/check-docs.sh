#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
source "$script_dir/lib/mise-contract-workflow.sh"
failures=0
is_canonical=0
if grep -Eq '^module dotfiles/updev$' "$root/go.mod"; then
  is_canonical=1
fi

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

check_domain_index() {
  local domain="$1"
  local index="$2"
  local child

  if [[ ! -d "$root/docs/$domain" ]]; then
    return
  fi
  require_file "docs/$index"
  while IFS= read -r child; do
    if ! grep -Fq "$domain/$(basename "$child")" "$root/docs/$index"; then
      fail "orphaned docs/$domain child is not linked from docs/$index: $(basename "$child")"
    fi
  done < <(find "$root/docs/$domain" -mindepth 1 -maxdepth 1 -type f -name '*.md' -print)
  if find "$root/docs/$domain" -mindepth 2 -type f -print -quit | grep -q .; then
    fail "docs/$domain is deeper than the standard one-level domain hierarchy"
  fi
}

check_tui_semantic_ci_job() {
  local workflow="$root/.github/workflows/ci.yml"
  local job
  job="$(awk '
    /^  tui-semantic:$/ { capture=1 }
    capture && /^  [[:alnum:]_-]+:$/ && $0 != "  tui-semantic:" { exit }
    capture { print }
  ' "$workflow")"
  if [[ -z "$job" ]]; then
    fail "missing tui-semantic job in .github/workflows/ci.yml"
    return
  fi
  if ! grep -Fq 'runs-on: ${{ matrix.runner }}' <<<"$job"; then
    fail "tui-semantic job must bind runs-on to matrix.runner"
  fi
  if ! grep -Fq 'runner: [ubuntu-latest, macos-15-intel]' <<<"$job"; then
    fail "tui-semantic job must cover Linux and Intel macOS"
  fi
  if ! grep -Fq 'run: scripts/test-tui-shell-use.sh semantic' <<<"$job"; then
    fail "tui-semantic job must run the shell-use semantic lane"
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
require_file "docs/ARCHITECTURE.md"
require_file "docs/CLI.md"
require_file "docs/DATA-MODEL.md"
require_file "docs/PRODUCT.md"
require_file "docs/RELEASE.md"
require_file "docs/ROADMAP.md"
require_file "docs/SECURITY.md"
require_file "docs/VALIDATION.md"
require_file "docs/UX.md"
require_file "docs/DESIGN.md"
if [[ "$is_canonical" -eq 1 ]]; then
  require_file "AGENTS.md"
  require_file "CLAUDE.md"
  require_file "docs/PUBLISHING.md"
  require_grep '^@AGENTS\.md$' "CLAUDE.md"
fi
require_grep 'docs/agent/' "docs/PRODUCT.md"
check_domain_index "product" "PRODUCT.md"
check_domain_index "ux" "UX.md"
check_domain_index "architecture" "ARCHITECTURE.md"
check_domain_index "cli" "CLI.md"
check_domain_index "data-model" "DATA-MODEL.md"
check_domain_index "validation" "VALIDATION.md"
if ! mise_contract_error="$(updev_validate_mise_contract_workflow "$root/.github/workflows/ci.yml" 2>&1)"; then
  fail "$mise_contract_error"
fi
if grep -Fq '## Released patch notes' "$root/docs/RELEASE.md"; then
  fail "docs/RELEASE.md must not accumulate released patch-note history"
fi
if [[ "$(grep -Ec '^## Current Release$' "$root/docs/RELEASE.md")" -ne 1 ]]; then
  fail "docs/RELEASE.md must contain exactly one Current Release section"
fi
if [[ "$(grep -Ec '^## Next Release Target:' "$root/docs/RELEASE.md")" -ne 1 ]]; then
  fail "docs/RELEASE.md must contain exactly one Next Release Target section"
fi
go_cache="${GOCACHE:-}"
if [[ -z "$go_cache" ]]; then
  go_cache="${TMPDIR:-/tmp}/updev-gocache"
fi
go_path="${GOPATH:-}"
if [[ -z "$go_path" ]]; then
  go_path="${TMPDIR:-/tmp}/updev-gopath"
fi
go_mod_cache="${GOMODCACHE:-$go_path/pkg/mod}"
mkdir -p "$go_cache" "$go_mod_cache"
if ! diff -u "$root/docs/agent/SKILL.md" <(cd "$root" && GOPATH="$go_path" GOMODCACHE="$go_mod_cache" GOCACHE="$go_cache" go run . skill); then
  fail "embedded updev skill output drifted from docs/agent/SKILL.md"
fi
if ! diff -u "$root/docs/agent/USAGE.md" <(cd "$root" && GOPATH="$go_path" GOMODCACHE="$go_mod_cache" GOCACHE="$go_cache" go run . help agent); then
  fail "embedded updev help agent output drifted from docs/agent/USAGE.md"
fi
require_grep 'docs/release-notes/<tag>\.md' "docs/PRODUCT.md"

require_grep 'uses: actions/setup-go@v6' ".github/workflows/ci.yml"
require_grep 'run: scripts/check-mise-contract\.sh' ".github/workflows/ci.yml"
require_grep 'run: scripts/check-docs\.sh' ".github/workflows/ci.yml"
require_grep 'run: scripts/check-go-format\.sh' ".github/workflows/ci.yml"
require_grep 'run: scripts/check-staticcheck\.sh' ".github/workflows/ci.yml"
require_grep 'run: scripts/check-golangci-lint\.sh' ".github/workflows/ci.yml"
require_grep 'run: scripts/test-go-tool-cache\.sh' ".github/workflows/ci.yml"
require_grep 'go run github\.com/rhysd/actionlint/cmd/actionlint@v1\.7\.12' ".github/workflows/ci.yml"
require_grep 'shellcheck -S warning scripts/\*\.sh scripts/lib/\*\.sh' ".github/workflows/ci.yml"
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
require_grep '^  workflow_call:$' ".github/workflows/ci.yml"
require_grep '^    uses: \./\.github/workflows/ci\.yml$' ".github/workflows/release.yml"
require_grep '^    needs: check$' ".github/workflows/release.yml"
require_grep 'expected="updev \$\{TAG\}"' ".github/workflows/release.yml"
require_grep 'uses: actions/attest@v4' ".github/workflows/release.yml"
require_grep 'subject-checksums: \./dist/checksums\.txt' ".github/workflows/release.yml"
require_grep 'name_template: checksums\.txt' ".goreleaser.yml"
if [[ "$is_canonical" -eq 1 ]]; then
  require_grep 'mise -C tools/updev run audit' "AGENTS.md"
  require_grep 'mise -C tools/updev run docs-check' "AGENTS.md"
  require_grep 'mise -C tools/updev run validation-check' "AGENTS.md"
  require_grep 'mise -C tools/updev run test-tui' "AGENTS.md"
else
  require_grep 'mise run audit' "README.md"
  require_grep 'mise run docs-check' "README.md"
  require_grep 'mise run validation-check' "README.md"
  require_grep 'mise run test-tui' "README.md"
fi
require_grep '^actionlint = "1\.7\.12"$' "mise.toml"
require_grep 'depends = \["fmt-check", "staticcheck", "golangci-lint", "shellcheck", "actionlint"\]' "mise.toml"
require_grep 'depends = \["lint", "test", "vet", "mod-verify", "build", "mise-contract", "go-tool-cache-test"\]' "mise.toml"
require_grep 'depends = \["vuln", "supply-chain", "agent-quality"\]' "mise.toml"
require_grep 'run = "scripts/test-tui-shell-use\.sh all"' "mise.toml"
require_grep '"@microsoft/shell-use": "0\.0\.1-beta\.6"' "test/tui/package.json"
check_tui_semantic_ci_job
require_grep '"@resvg/resvg-js": "2\.6\.2"' "test/tui/package.json"
require_grep '"odiff-bin": "4\.5\.0"' "test/tui/package.json"

while IFS= read -r baseline; do
  [[ -z "$baseline" ]] && continue
  require_file "test/tui/baselines/terminal/$baseline.snap"
  require_file "test/tui/baselines/visual/$baseline.png"
done < <(sed -n 's/^| `\([^`]*\)` |.*$/\1/p' "$root/docs/DESIGN.md")

"$root/scripts/check-validation-blocks.sh" || failures=$((failures + 1))
"$root/scripts/test-validation-blocks.sh" || failures=$((failures + 1))

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
  find "$root" \( -path "$root/.git" -o \( -type d -name node_modules \) \) -prune -o \( -name README.md -o -path "$root/docs/*.md" -o -path "$root/docs/*/*.md" \) -type f -print |
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
