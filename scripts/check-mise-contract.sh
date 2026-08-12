#!/usr/bin/env bash
set -euo pipefail

minimum_version="2026.8.2"

fail() {
  printf 'mise-contract: %s\n' "$*" >&2
  exit 1
}

assert_contains() {
  local label="$1"
  local payload="$2"
  shift 2
  local needle
  for needle in "$@"; do
    if ! grep -Fq "$needle" <<<"$payload"; then
      fail "$label is missing required evidence: $needle"
    fi
  done
}

assert_excludes() {
  local label="$1"
  local payload="$2"
  shift 2
  local needle
  for needle in "$@"; do
    if grep -Fq "$needle" <<<"$payload"; then
      fail "$label unexpectedly contains inactive evidence: $needle"
    fi
  done
}

version_at_least() {
  local actual="$1"
  local required="$2"
  local actual_major actual_minor actual_patch actual_extra
  local required_major required_minor required_patch required_extra

  IFS=. read -r actual_major actual_minor actual_patch actual_extra <<<"$actual"
  IFS=. read -r required_major required_minor required_patch required_extra <<<"$required"
  if [[ -n "${actual_extra:-}" || -n "${required_extra:-}" ]]; then
    return 1
  fi
  for component in "$actual_major" "$actual_minor" "$actual_patch" "$required_major" "$required_minor" "$required_patch"; do
    if [[ -z "$component" || "$component" == *[!0-9]* ]]; then
      return 1
    fi
  done

  if ((10#$actual_major != 10#$required_major)); then
    ((10#$actual_major > 10#$required_major))
    return
  fi
  if ((10#$actual_minor != 10#$required_minor)); then
    ((10#$actual_minor > 10#$required_minor))
    return
  fi
  ((10#$actual_patch >= 10#$required_patch))
}

if ! command -v mise >/dev/null 2>&1; then
  fail "mise is required; install v${minimum_version} or newer"
fi

version_output="$(mise --version 2>&1)" || fail "mise --version failed: $version_output"
actual_version="${version_output%% *}"
actual_version="${actual_version#v}"
if ! version_at_least "$actual_version" "$minimum_version"; then
  fail "mise v${minimum_version} or newer is required; found: $version_output"
fi

fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/updev-mise-contract.XXXXXX")"
cleanup() {
  rm -rf "$fixture_root"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

fixture_home="$fixture_root/home"
fixture_work="$fixture_root/work"
fixture_config="$fixture_home/.config/mise"
fixture_system="$fixture_root/system"
mkdir -p "$fixture_config" "$fixture_system" "$fixture_work"

printf 'min_version = { hard = "%s" }\n\n[tools]\ngo = "1.26.5"\n' "$minimum_version" >"$fixture_config/config.toml"
printf '[tools]\nuv = "0.11.19"\n' >"$fixture_config/config.scope-alpha.toml"
printf '[tools]\npython = "3.14.5"\n' >"$fixture_config/config.scope-beta.toml"

run_fixture_mise() {
  local environment="$1"
  shift
  HOME="$fixture_home" \
    XDG_CONFIG_HOME="$fixture_home/.config" \
    MISE_SYSTEM_CONFIG_DIR="$fixture_system" \
    MISE_GLOBAL_CONFIG_ROOT="$fixture_home" \
    MISE_DATA_DIR="$fixture_root/data" \
    MISE_CACHE_DIR="$fixture_root/cache" \
    MISE_STATE_DIR="$fixture_root/state" \
    MISE_CEILING_PATHS="$fixture_work" \
    MISE_SAFE=1 \
    MISE_ENV="$environment" \
    mise "$@"
}

base_json="$(run_fixture_mise "" config ls --json --cd "$fixture_work")" || fail "mise config ls --json failed for the base fixture"
assert_contains "base config source" "$base_json" 'config.toml' '"go"'
assert_excludes "base config source" "$base_json" 'config.scope-'

alpha_json="$(run_fixture_mise "" config ls --json --env scope-alpha --cd "$fixture_work")" || fail "mise config ls --env scope-alpha failed"
assert_contains "scope-alpha config source" "$alpha_json" 'config.toml' 'config.scope-alpha.toml' '"go"' '"uv"'
assert_excludes "scope-alpha config source" "$alpha_json" 'config.scope-beta.toml' '"python"'

beta_json="$(run_fixture_mise scope-beta config ls --json --cd "$fixture_work")" || fail "mise config ls with MISE_ENV=scope-beta failed"
assert_contains "scope-beta config source" "$beta_json" 'config.toml' 'config.scope-beta.toml' '"go"' '"python"'
assert_excludes "scope-beta config source" "$beta_json" 'config.scope-alpha.toml' '"uv"'

status_json="$(run_fixture_mise "" bootstrap status --json --cd "$fixture_work")" || fail "mise bootstrap status --json failed"
assert_contains "bootstrap status JSON" "$status_json" '"packages"' '"tools"'

plan_json="$(run_fixture_mise "" bootstrap plan --json --cd "$fixture_work")" || fail "mise bootstrap plan --json failed"
assert_contains "bootstrap plan JSON" "$plan_json" '"resources"' '"summary"'

dry_run_output="$(run_fixture_mise "" bootstrap packages apply --dry-run --cd "$fixture_work" 2>&1)" || fail "mise bootstrap packages apply --dry-run failed: $dry_run_output"

printf 'mise-contract: ok\n'
printf '  mise: %s\n' "$actual_version"
printf '  fixtures: base, scope-alpha, scope-beta\n'
printf '  bootstrap: status-json, plan-json, packages-dry-run\n'
