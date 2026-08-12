#!/usr/bin/env bash

updev_lint_cache_base() {
  if [[ -n "${UPDEV_LINT_CACHE_DIR:-}" ]]; then
    printf '%s\n' "$UPDEV_LINT_CACHE_DIR"
    return 0
  fi
  if [[ -n "${TMPDIR:-}" ]] && [[ -d "$TMPDIR" ]]; then
    local tmp_owner
    if tmp_owner="$(updev_cache_owner "$TMPDIR")" && [[ "$tmp_owner" == "$(id -u)" ]]; then
      printf '%s/updev-lint\n' "${TMPDIR%/}"
      return 0
    fi
  fi
  if [[ -n "${XDG_CACHE_HOME:-}" ]]; then
    printf '%s/updev/lint\n' "$XDG_CACHE_HOME"
    return 0
  fi
  if [[ -n "${HOME:-}" ]]; then
    printf '%s/.cache/updev/lint\n' "$HOME"
    return 0
  fi
  printf '%s/updev-lint-%s\n' "${TMPDIR:-/tmp}" "$(id -u)"
}

updev_cache_owner() {
  local path="$1"
  local owner
  if owner="$(stat -f '%u' "$path" 2>/dev/null)"; then
    printf '%s\n' "$owner"
    return 0
  fi
  stat -c '%u' "$path"
}

updev_prepare_lint_cache() {
  local cache_base="$1"
  if [[ -L "$cache_base" ]]; then
    printf 'refusing symlinked Go tool cache: %s\n' "$cache_base" >&2
    return 1
  fi
  if ! mkdir -p "$cache_base"; then
    printf 'could not create Go tool cache: %s\n' "$cache_base" >&2
    return 1
  fi
  if [[ -L "$cache_base" ]]; then
    printf 'refusing symlinked Go tool cache: %s\n' "$cache_base" >&2
    return 1
  fi
  local owner
  if ! owner="$(updev_cache_owner "$cache_base")"; then
    printf 'could not inspect Go tool cache owner: %s\n' "$cache_base" >&2
    return 1
  fi
  if [[ "$owner" != "$(id -u)" ]]; then
    printf 'refusing Go tool cache not owned by current user: %s\n' "$cache_base" >&2
    return 1
  fi
  if ! chmod 700 "$cache_base"; then
    printf 'could not restrict Go tool cache permissions: %s\n' "$cache_base" >&2
    return 1
  fi
}

updev_prepare_go_analysis_cache() {
  local cache_root="$1"
  if [[ -L "$cache_root" ]]; then
    printf 'refusing symlinked Go analysis cache: %s\n' "$cache_root" >&2
    return 1
  fi
  if ! mkdir -p "$cache_root"; then
    printf 'could not create Go analysis cache: %s\n' "$cache_root" >&2
    return 1
  fi
  if [[ -L "$cache_root" ]]; then
    printf 'refusing symlinked Go analysis cache: %s\n' "$cache_root" >&2
    return 1
  fi

  local owner
  if ! owner="$(updev_cache_owner "$cache_root")"; then
    printf 'could not inspect Go analysis cache owner: %s\n' "$cache_root" >&2
    return 1
  fi
  if [[ "$owner" != "$(id -u)" ]]; then
    printf 'refusing Go analysis cache not owned by current user: %s\n' "$cache_root" >&2
    return 1
  fi

  local module_cache="$cache_root/gomodcache"
  if [[ -d "$module_cache" ]] && ! env \
    GOPATH="$cache_root/gopath" \
    GOCACHE="$cache_root/gocache" \
    GOMODCACHE="$module_cache" \
    go mod verify >/dev/null 2>&1; then
    printf 'Go analysis module cache is invalid; rebuilding: %s\n' "$module_cache" >&2
    if ! updev_remove_go_tool_tree "$cache_root"; then
      return 1
    fi
    if ! mkdir -p "$cache_root"; then
      printf 'could not recreate Go analysis cache: %s\n' "$cache_root" >&2
      return 1
    fi
  fi
}

updev_remove_go_tool_tree() {
  local path="$1"
  if [[ ! -e "$path" ]]; then
    return 0
  fi
  chmod -R u+w "$path" 2>/dev/null || true
  if ! rm -rf -- "$path"; then
    printf 'could not remove Go tool cache tree: %s\n' "$path" >&2
    return 1
  fi
}

updev_go_tool() {
  local name="$1"
  local package_path="$2"
  local version="$3"

  if [[ ! "$name" =~ ^[a-zA-Z0-9._-]+$ ]] || [[ ! "$version" =~ ^v?[a-zA-Z0-9._-]+$ ]]; then
    printf 'invalid Go tool cache key: %s@%s\n' "$name" "$version" >&2
    return 1
  fi

  local cache_base
  if ! cache_base="$(updev_lint_cache_base)"; then
    return 1
  fi
  if ! updev_prepare_lint_cache "$cache_base"; then
    return 1
  fi

  local go_version
  if ! go_version="$(go env GOVERSION)"; then
    printf 'could not resolve Go version for tool cache\n' >&2
    return 1
  fi
  if [[ ! "$go_version" =~ ^go[0-9.]+$ ]]; then
    printf 'invalid Go version for tool cache: %s\n' "$go_version" >&2
    return 1
  fi

  local tool_parent="$cache_base/go-tools/$name"
  local cache_key="${version#v}-$go_version"
  local binary="$tool_parent/$cache_key"
  local legacy_binary="$tool_parent/$cache_key/bin/$name"
  if [[ -f "$binary" ]] && [[ -x "$binary" ]]; then
    printf '%s\n' "$binary"
    return 0
  fi
  if [[ -f "$legacy_binary" ]] && [[ -x "$legacy_binary" ]]; then
    printf '%s\n' "$legacy_binary"
    return 0
  fi

  if ! mkdir -p "$tool_parent"; then
    printf 'could not create Go tool cache parent: %s\n' "$tool_parent" >&2
    return 1
  fi
  local build_root
  if ! build_root="$(mktemp -d "$tool_parent/.generation.XXXXXX")"; then
    printf 'could not create Go tool build directory under %s\n' "$tool_parent" >&2
    return 1
  fi
  local cleanup_command
  printf -v cleanup_command 'updev_remove_go_tool_tree %q' "$build_root"
  # Expand now so the trap retains this invocation's local build path.
  # shellcheck disable=SC2064
  trap "$cleanup_command" EXIT
  # shellcheck disable=SC2064
  trap "$cleanup_command; exit 130" HUP INT TERM

  if ! env \
    GOBIN="$build_root/bin" \
    GOPATH="$build_root/gopath" \
    GOCACHE="$build_root/gocache" \
    GOMODCACHE="$build_root/gomodcache" \
    go install "$package_path@$version"; then
    updev_remove_go_tool_tree "$build_root" || true
    trap - EXIT HUP INT TERM
    return 1
  fi

  if [[ ! -x "$build_root/bin/$name" ]]; then
    printf 'Go tool install did not produce %s\n' "$name" >&2
    updev_remove_go_tool_tree "$build_root" || true
    trap - EXIT HUP INT TERM
    return 1
  fi

  for path in "$build_root/gopath" "$build_root/gocache" "$build_root/gomodcache"; do
    if ! updev_remove_go_tool_tree "$path"; then
      updev_remove_go_tool_tree "$build_root" || true
      trap - EXIT HUP INT TERM
      return 1
    fi
  done

  if ! ln "$build_root/bin/$name" "$binary" 2>/dev/null && { [[ ! -f "$binary" ]] || [[ ! -x "$binary" ]]; }; then
    printf 'could not publish Go tool cache binary: %s\n' "$binary" >&2
    updev_remove_go_tool_tree "$build_root" || true
    trap - EXIT HUP INT TERM
    return 1
  fi
  if ! updev_remove_go_tool_tree "$build_root"; then
    trap - EXIT HUP INT TERM
    return 1
  fi
  trap - EXIT HUP INT TERM

  if [[ ! -f "$binary" ]] || [[ ! -x "$binary" ]]; then
    printf 'Go tool cache publish did not produce %s\n' "$binary" >&2
    return 1
  fi
  printf '%s\n' "$binary"
}
