# updev release target

This document tracks the current implemented release and the next release
target. Keep longer-term ordering in [ROADMAP.md](ROADMAP.md), implementation
history in git log, and detailed design in [DESIGN.md](DESIGN.md).

Version labels use `<tool> vMAJOR.MINOR.PATCH`, while JSON `schema_version`
stays an integer schema contract. `v0.x` releases are public preview releases;
`v1.0.0` is reserved for the first stable public contract.

## Current Release

The current implemented release is `updev v0.5.6`. `updev version`,
`updev --version`, and `updev -v` report this command contract.

`updev v0.5.6` is a Japanese description-translation UX patch on the `v0.5.x`
CLI scope. It keeps the main update workflow stable while letting Japanese
TTY `updev` / `updev list` refresh cached `updev list` descriptions
through the optional Codex CLI, exposing `[ui].description_translation` to
choose `auto`, `manual`, or `off`, and reporting the optional Codex backend in
dependency diagnostics. It builds on the `v0.5.5` mise diagnostics, agent
guidance, and documentation source-of-truth patch.

The `v0.5.x` polish gate is folded into this current release state: real TTY
dogfood, override duplicate/update/list/remove ergonomics, plan detail text,
vendor URL evidence, provider adoption evidence, provider/backend preference
policy, backend convergence UX from `updev` / `updev list`, Japanese/human copy,
and safe accept/edit/ignore write-flow smoke are complete. Keep regression
commands in local test plans, not as a permanent release log here.

### v0.5.x Scope

1. **Read-only app scan**: `updev inventory scan --provider manual` reports
   evidence from `/Applications`, `~/Applications`, bundle `Info.plist`, Mac
   App Store receipts / `mas list` where available, and cached Homebrew cask
   inventory.
2. **Normalized identity model**: manual/vendor app rows reconcile by display
   name, normalized name, bundle id, MAS id, cask token, path, aliases, and
   provider ownership evidence.
3. **Cask/manual reconciliation**: Homebrew cask evidence merges into the manual
   row instead of creating duplicate GUI app entries when identity matches.
4. **Review candidates**: live-only or ambiguous rows emit
   `review_candidates[]` with stable `reason_code`, `remediation_code`,
   `confidence`, params, evidence, and suggested override fields.
5. **Review override preview**: `updev inventory review --provider manual`
   renders read-only TOML snippets for the configured inventory override file
   and returns exit `2` while review is needed.
6. **Generated report preview**: `updev inventory render --report manual-apps`
   previews generated Markdown without writing desired state.
7. **Manual inventory plan/check**: `updev inventory plan --provider manual`
   and `check` group rows into `keep-manual`, `adopt-brew`, `adopt-mas`,
   `ignore-local`, `open-vendor`, and `needs-review`.
8. **Main human entry point**: interactive `updev` defaults to the
   manual plan when manual actions need review, but manual/vendor rows remain
   outside the default package update table.
9. **Override write actions**: `updev inventory review --provider manual
   --action accept|edit|ignore --query <text>` appends exactly one selected
   override to the configured overrides TOML.
10. **Gated provider guidance**: plan JSON includes `attention_count`,
    `suggested_provider`, `review_url`, `install_hint`, and quoted
    `command_preview` fields for review; vendor installer commands are never
    executed from inventory output.
11. **mise native age-policy diagnostics**: `updev doctor dependencies` and
    `updev check --dependencies` report mise `minimum_release_age`, source,
    active/inactive status, and command-shape support.
12. **mise fix age-policy evidence**: `updev fix mise` text/JSON reports include
    whether `mise latest <tool>` resolutions ran under an effective mise
    `minimum_release_age` policy.
13. **Documentation source-of-truth checks**: `docs/agent/` is the canonical
    agent guidance tree, `docs/release-notes/<tag>.md` is the GitHub Release
    body source, and `mise run docs-check` covers release-note presence,
    README links, agent guidance files, and mise/CI validation parity.
14. **Description translation UX**: Japanese TTY `updev` and
    `updev list` can best-effort refresh cached `updev list` descriptions via
    the optional Codex CLI, `[ui].description_translation` can switch between
    `auto`, `manual`, and `off`, missing Codex remains non-fatal, and
    `updev doctor dependencies` reports the optional backend.

### v0.5.x Non-Goals

- automatic installation of paid, privileged, vendor-account, or
  checksum-missing apps;
- broad Linux/Windows package provider support;
- replacing Homebrew casks that already work well;
- making manual/vendor rows part of the default `updev` inventory;
- claiming public `v1.0.0` readiness.

### v0.5.x Release Criteria

- read-only scan, reconciliation, plan/check, review, and render paths have
  focused tests with fake filesystem / command evidence;
- JSON reports expose identity, lifecycle, owner, evidence, confidence,
  reason/remediation codes, params, suggested overrides, suggested providers,
  install hints, review URLs, and command previews without ANSI color or
  localization;
- manual/vendor inventory stays opt-in;
- docs-check passes for high-value documentation mirrors;
- `go mod verify`, `go vet ./...`, `go test ./...`, `go build ./...`, and
  `git diff --check` pass. Repository-local integration smoke checks may add
  extra gates before mirroring a release.

## Next Minor: v0.6.0

`updev v0.6.0` should add the first updev-owned mise update gate and establish
the provider-wide gate model. Homebrew already has an updev-owned release-age
gate; v0.6.0 should bring mise into the same decision vocabulary, then make that
model the direction for VS Code and future providers. Linux scanner groundwork
can proceed after this safety model is explicit.

### v0.6.0 Target Scope

1. **updev-owned mise release-age gate**: evaluate GitHub and registry-backed
   mise candidates with updev config/env thresholds, cache keys, text/JSON
   evidence, and `allow|hold|review|block` decisions aligned with Homebrew.
2. **Provider-native policy evidence**: report mise `minimum_release_age` as
   evidence, but keep updev-owned gate decisions independent and explainable.
3. **Provider-wide gate contract**: document the common fields every provider
   gate should expose: candidate identity, release date/age, min age, evidence,
   policy source, decision, confidence, and remediation.
4. **Agent skill command**: add `updev skill`, `updev skill --full`, and
   `updev help agent` after the `docs/agent/` source tree exists. These commands
   should embed the canonical files and avoid duplicating command reference in
   code.
5. **Documentation drift checks**: add a focused local/CI check for high-value
   mirrors such as release-note presence for tags, embedded agent skill output,
   README links, and mise/CI validation parity. Expand later only where drift
   has caused real maintenance risk.
6. **Linux manual inventory scanner**: add read-only evidence from `.desktop`
   files, Flatpak, Snap, AppImage, and distro package metadata where cheap.
7. **Cross-platform fixtures**: cover Linux and Windows-style evidence with fake
   runner / fixture tests so most implementation can proceed on macOS.
8. **Windows scanner spike**: define registry / winget evidence mapping and keep
   it read-only; mark it experimental until a Windows runner or real machine
   dogfood exists.
9. **Provider promotion parity**: map Linux/Windows manual app rows into the
   same plan/check action vocabulary without making broad package management a
   stable promise.
10. **Evidence quality**: improve source URLs, ownership confidence, and
   provider-native metadata for manual/vendor decisions.

### v0.6.0 Non-Goals

- default update inclusion for manual/vendor rows;
- automatic external installer execution;
- requiring a Windows environment for the first Linux scanner implementation;
- silently mutating mise provider settings;
- public `v1.0.0` stability promise.

## Later Ordering

Longer-term priorities live in [ROADMAP.md](ROADMAP.md). The short version:

1. Continue provider-general inventory after `v0.6.0` with deeper
   Linux/Windows scanners.
2. Broaden Homebrew and mise release-age/advisory confidence beyond GitHub and
   first registry paths.
3. Apply the common updev-owned gate model to VS Code and future providers.
4. Broaden Homebrew release-age and advisory confidence beyond GitHub
   release/tag/ref URL paths.
5. Add provider-native audit paths where package identity is reliable.
6. Continue scanner hardening after OSV-Scanner, gitleaks, zizmor, Trivy, and
   Grype.
7. Prepare for public `updev v1.0.0` only after the stable macOS/Homebrew/mise
   scope is deliberately narrowed and documented.

## Public Preview Maintenance

The public preview should stay narrow and incremental:

1. **macOS-first support** remains the stable preview path while
   Linux/Windows binaries and provider expansion are clearly labeled
   experimental.
2. **Pre-v1 hardening** freezes the stable command/config/JSON surface for the
   macOS/Homebrew/mise update workflow, labels experimental provider surfaces,
   removes or clearly hides compatibility-only paths, and keeps
   compatibility tests for exit codes, JSON shape, no-color/non-TTY output, and
   config parsing.
3. **Distribution maintenance** keeps the mise GitHub backend install path,
   release binaries, checksums, source install path, and
   `docs/release-notes/<tag>.md` in sync. The tag workflow uses that note as
   the GitHub Release body and updates existing release notes on reruns.
   The repository-local `mise.toml` pins the Go toolchain and `mise run check`
   mirrors the public CI's module verify, vet, test, and build gates.
4. **`updev v1.0.0`** is the first stable public release. Its stable promise is
   macOS-first package/tool orchestration for Homebrew, Brewfile-derived
   desired state, mise, focused security gates, inventory, sync,
   add/remove/edit/rollback, and documented JSON reports. Manual/vendor app
   inventory may remain opt-in unless it reaches the same stability bar.
