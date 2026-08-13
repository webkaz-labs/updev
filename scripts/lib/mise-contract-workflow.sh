#!/usr/bin/env bash

updev_workflow_step() {
  local workflow="$1"
  local name="$2"
  awk -v name="$name" '
    $0 == "      - name: " name { capture=1 }
    capture && /^      - name:/ && $0 != "      - name: " name { exit }
    capture { print }
  ' "$workflow"
}

updev_validate_mise_contract_workflow() {
  local workflow="$1"
  local pinned_step
  local latest_step
  local step

  if ! grep -Fq 'mise-version: ["2026.8.2", "latest"]' "$workflow"; then
    printf 'mise contract matrix must cover pinned 2026.8.2 and latest\n' >&2
    return 1
  fi
  if [[ "$(grep -Fc 'uses: jdx/mise-action@' "$workflow")" -ne 2 ]]; then
    printf 'mise contract workflow must contain exactly two mise-action setup steps\n' >&2
    return 1
  fi

  pinned_step="$(updev_workflow_step "$workflow" "Setup pinned mise contract binary")"
  latest_step="$(updev_workflow_step "$workflow" "Setup latest mise contract binary")"
  if [[ -z "$pinned_step" ]] || ! grep -Fq "if: matrix.mise-version != 'latest'" <<<"$pinned_step" ||
    ! grep -Fq 'uses: jdx/mise-action@v4' <<<"$pinned_step" ||
    ! grep -Fq 'version: ${{ matrix.mise-version }}' <<<"$pinned_step"; then
    printf 'pinned mise setup must use mise-action@v4 with the matrix version\n' >&2
    return 1
  fi
  if [[ -z "$latest_step" ]] || ! grep -Fq "if: matrix.mise-version == 'latest'" <<<"$latest_step" ||
    ! grep -Fq 'uses: jdx/mise-action@v4' <<<"$latest_step"; then
    printf 'latest mise setup must use mise-action@v4 default version resolution\n' >&2
    return 1
  fi
  if grep -Eq '^[[:space:]]+version:' <<<"$latest_step"; then
    printf 'latest mise setup must omit version so mise-action resolves the latest release\n' >&2
    return 1
  fi
  for step in "$pinned_step" "$latest_step"; do
    if ! grep -Fq 'install: false' <<<"$step" ||
      ! grep -Fq 'cache: false' <<<"$step" ||
      ! grep -Fq 'env: false' <<<"$step"; then
      printf 'each mise setup must disable install, cache, and env mutation\n' >&2
      return 1
    fi
  done
}
