#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
test_root="$root/test/tui"

mode="${1:-semantic}"
case "$mode" in
  semantic|visual|all) ;;
  *)
    printf 'usage: %s [semantic|visual|all]\n' "$0" >&2
    exit 64
    ;;
esac

if ! command -v node >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then
  echo "tui-test: Node.js 20+ and npm are required" >&2
  exit 1
fi

node_major="$(node -p 'process.versions.node.split(".")[0]')"
if (( node_major < 20 )); then
  echo "tui-test: Node.js 20+ is required" >&2
  exit 1
fi

export npm_config_cache="${UPDEV_TUI_NPM_CACHE:-${TMPDIR:-/tmp}/updev-tui-npm-cache}"
if [[ ! -f "$test_root/node_modules/.package-lock.json" || "$test_root/package-lock.json" -nt "$test_root/node_modules/.package-lock.json" ]]; then
  npm ci --prefix "$test_root" --no-audit --no-fund
fi

exec node "$test_root/run.mjs" "$mode"
