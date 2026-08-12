#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
checker="$script_dir/check-validation-blocks.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/updev-validation-test.XXXXXX")"
cleanup() {
  rm -rf "$tmp"
}
trap cleanup EXIT

expect_status() {
  local expected="$1"
  local description="$2"
  local document="$3"
  local status

  set +e
  "$checker" "$document" sample >/dev/null 2>&1
  status=$?
  set -e
  if [[ "$status" -ne "$expected" ]]; then
    echo "validation-blocks-test: $description should exit $expected, got $status" >&2
    exit 1
  fi
}

cat >"$tmp/pass.md" <<'EOF'
<!-- validation:sample -->
```bash
test -f docs/PRODUCT.md
grep -q '^# updev product' docs/PRODUCT.md
```
EOF
"$checker" "$tmp/pass.md" sample >/dev/null

cat >"$tmp/comment-only.md" <<'EOF'
<!-- validation:sample -->
```bash
# assert: docs exist
```
EOF
expect_status 64 "comment-only block" "$tmp/comment-only.md"

cat >"$tmp/incomplete.md" <<'EOF'
<!-- validation:sample -->
```bash
test -f docs/PRODUCT.md \
```
EOF
expect_status 64 "incomplete block" "$tmp/incomplete.md"

cat >"$tmp/false-assertion.md" <<'EOF'
<!-- validation:sample -->
```bash
test -f docs/THIS_FILE_MUST_NOT_EXIST.md
```
EOF
expect_status 1 "false assertion" "$tmp/false-assertion.md"

echo "validation-blocks-test: ok"
