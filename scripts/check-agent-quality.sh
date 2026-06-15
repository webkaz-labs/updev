#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
strict="${UPDEV_AGENT_QUALITY_STRICT:-0}"
aislop_version="0.12.0"
cache_root="${UPDEV_AUDIT_CACHE_DIR:-${TMPDIR:-/tmp}/updev-audit}"
findings=0

note() {
  printf 'agent-quality: %s\n' "$*" >&2
  findings=$((findings + 1))
}

scan_rg() {
  local label="$1"
  local pattern="$2"
  local include="$3"
  local matches file
  matches="$(
    cd "$root"
    while IFS= read -r -d '' file; do
      grep -nE "$pattern" "$file" | sed "s#^#${file#./}:#" || true
    done < <(
      find . -type f -name "$include" \
        -not -path './vendor/*' \
        -not -path './dist/*' \
        -not -path './docs/release-notes/*' \
        -not -path './scripts/check-agent-quality.sh' \
        -print0
    )
  )"
  if [[ -n "$matches" ]]; then
    note "$label"
    printf '%s\n' "$matches" >&2
  fi
}

scan_rg "unfinished implementation markers" '(^|[^A-Za-z0-9_])(TODO|FIXME|HACK|XXX|quick and dirty|not implemented)($|[^A-Za-z0-9_])' '*.go'
scan_rg "debug output left in Go source" 'fmt\.(Println|Printf|Print|Fprintln)\([^)]*(debug|DEBUG|todo|TODO)' '*.go'
scan_rg "narrative comments that often duplicate obvious code" '^\s*//\s*(This|These|Here|Now|Simply|Just|Basically|Obviously)\b' '*.go'
scan_rg "unfinished shell markers" '(^|[^A-Za-z0-9_])(TODO|FIXME|HACK|XXX|not implemented)($|[^A-Za-z0-9_])' '*.sh'

run_aislop() {
  local home_dir npm_cache json_file status
  home_dir="$cache_root/aislop-home"
  npm_cache="$cache_root/npm-cache"
  json_file="$cache_root/aislop-report.json"
  mkdir -p "$home_dir" "$npm_cache"
  rm -f "$json_file"

  if command -v npx >/dev/null 2>&1; then
    status=0
    (
      cd "$root"
      HOME="$home_dir" npm_config_cache="$npm_cache" \
        npx --yes "aislop@${aislop_version}" scan \
        --json \
        --exclude docs/release-notes \
        --exclude dist \
        --exclude legacy \
        --exclude vendor \
        . >"$json_file"
    ) || status=$?
    summarize_aislop_json "$json_file"
    return "$status"
  fi

  if command -v aislop >/dev/null 2>&1; then
    status=0
    (cd "$root" && HOME="$home_dir" aislop scan --json . >"$json_file") || status=$?
    summarize_aislop_json "$json_file"
    return "$status"
  fi

  printf 'agent-quality: aislop unavailable; used built-in deterministic heuristics only\n'
  return 0
}

summarize_aislop_json() {
  local json_file="$1"
  if [[ ! -s "$json_file" || ! "$(command -v node || true)" ]]; then
    printf 'agent-quality: aislop %s completed; JSON summary unavailable\n' "$aislop_version" >&2
    return 0
  fi
  node -e '
const fs = require("fs");
const path = process.argv[1];
const report = JSON.parse(fs.readFileSync(path, "utf8"));
const diagnostics = Array.isArray(report.diagnostics) ? report.diagnostics : [];
const knownFalsePositive = (finding) =>
  finding.filePath === "internal/securityreason/reason.go" &&
  finding.rule === "security/hardcoded-secret" &&
  Number(finding.line || 0) === 16;
const active = diagnostics.filter((finding) => !knownFalsePositive(finding));
const known = diagnostics.length - active.length;
const errors = active.filter((finding) => finding.severity === "error").length;
const warnings = active.filter((finding) => finding.severity === "warning").length;
const rules = new Map();
for (const finding of active) {
  const key = finding.rule || "unknown";
  rules.set(key, (rules.get(key) || 0) + 1);
}
const topRules = [...rules.entries()]
  .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  .slice(0, 6)
  .map(([rule, count]) => `${rule}=${count}`)
  .join(", ");
console.error(`agent-quality: aislop ${report.version || "unknown"} score=${report.score ?? "?"} ${report.label || ""}; active=${errors} error(s), ${warnings} warning(s); known_false_positive=${known}`);
if (topRules) {
  console.error(`agent-quality: aislop top rules: ${topRules}`);
}
if (errors > 0) {
  console.error("agent-quality: run `npx aislop scan -d` for details");
}
' "$json_file"
}

if ! run_aislop; then
  if [[ "$strict" == "1" ]]; then
    note "aislop ${aislop_version} reported findings"
  else
    printf 'agent-quality: aislop %s reported findings; non-blocking in default mode\n' "$aislop_version" >&2
  fi
fi

if (( findings > 0 )); then
  printf 'agent-quality: %d non-blocking finding group(s)\n' "$findings" >&2
  if [[ "$strict" == "1" ]]; then
    exit 1
  fi
else
  printf 'agent-quality: ok\n'
fi
