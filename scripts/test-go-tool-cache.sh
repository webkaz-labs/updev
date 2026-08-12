#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/lib/go-tool-cache.sh"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/updev-go-tool-cache-test.XXXXXX")"
cleanup() {
  rm -rf "$tmp"
}
trap cleanup EXIT

fail() {
  printf 'go-tool-cache-test: %s\n' "$1" >&2
  exit 1
}

mkdir -p "$tmp/bin"
cat >"$tmp/bin/go" <<'EOF'
#!/usr/bin/env bash
[[ "$*" == "mod verify" ]] || exit 1
[[ ! -f "$GOMODCACHE/.invalid" ]]
EOF
chmod +x "$tmp/bin/go"
PATH="$tmp/bin:$PATH"

cache_root="$tmp/lint/golangci-run/go-test"
peer_root="$tmp/lint/staticcheck-run/go-test"
mkdir -p "$cache_root/gomodcache" "$cache_root/gocache" "$peer_root"
touch "$cache_root/gomodcache/.valid" "$cache_root/gocache/keep" "$peer_root/keep"

updev_prepare_go_analysis_cache "$cache_root"
[[ -f "$cache_root/gomodcache/.valid" ]] || fail 'healthy module cache was rebuilt'
[[ -f "$cache_root/gocache/keep" ]] || fail 'healthy build cache was rebuilt'

touch "$cache_root/gomodcache/.invalid"
updev_prepare_go_analysis_cache "$cache_root"
[[ -d "$cache_root" ]] || fail 'corrupt analyzer cache was not recreated'
[[ ! -e "$cache_root/gomodcache/.invalid" ]] || fail 'corrupt module cache was retained'
[[ ! -e "$cache_root/gocache/keep" ]] || fail 'target analyzer cache was not cleared'
[[ -f "$peer_root/keep" ]] || fail 'peer analyzer cache was removed'

mkdir -p "$tmp/real-cache"
ln -s "$tmp/real-cache" "$tmp/symlink-cache"
if updev_prepare_go_analysis_cache "$tmp/symlink-cache" 2>/dev/null; then
  fail 'symlinked analyzer cache was accepted'
fi

printf 'go-tool-cache-test: ok\n'
