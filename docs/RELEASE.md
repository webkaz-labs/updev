# updev release target

This document tracks the current implemented release and the next release
target. Keep longer-term ordering in [ROADMAP.md](ROADMAP.md), implementation
history in git log, and detailed design in [DESIGN.md](DESIGN.md).

Version labels follow the shared
[Go CLI release standard](../../../docs/go-cli/RELEASE.md#version-numbering):
tool releases use `<tool> vMAJOR.MINOR.PATCH`, while JSON `schema_version`
stays an integer schema contract.

## Current Release

The current implemented release is `updev v0.5.1`. `updev version`,
`updev --version`, and `updev -v` report this command contract.

`updev v0.5.1` is a public release maintenance patch on the `v0.5.0` CLI
scope. It updates GitHub Actions workflows to Node 24 action runtimes and keeps
release asset publishing rerunnable. The command behavior remains the
manual/vendor inventory decision slice: it keeps
manual rows out of the default daily update table while adding focused
plan/check actions, explicit override write actions, a daily human selector
entry point, and gated vendor guidance.

The `v0.5.x` polish gate is folded into this current release state: real TTY
dogfood, override duplicate/update/list/remove ergonomics, plan detail text,
vendor URL evidence, provider adoption evidence, provider/backend preference
policy, backend convergence UX from `updev` / `updev list`, Japanese/human copy,
and safe accept/edit/ignore write-flow smoke are complete. Keep regression
commands in [VALIDATION.md](VALIDATION.md), not as a permanent release log here.

### v0.5.0 Scope

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
8. **Daily human entry point**: interactive daily `updev` defaults to the
   manual plan when manual actions need review, but manual/vendor rows remain
   outside the default package update table.
9. **Override write actions**: `updev inventory review --provider manual
   --action accept|edit|ignore --query <text>` appends exactly one selected
   override to the configured overrides TOML.
10. **Gated provider guidance**: plan JSON includes `attention_count`,
    `suggested_provider`, `review_url`, `install_hint`, and quoted
    `command_preview` fields for review; vendor installer commands are never
    executed from inventory output.

### v0.5.0 Non-Goals

- automatic installation of paid, privileged, vendor-account, or
  checksum-missing apps;
- broad Linux/Windows package provider support;
- replacing Homebrew casks that already work well;
- making manual/vendor rows part of the default daily `updev` inventory;
- claiming public `v1.0.0` readiness.

### v0.5.0 Release Criteria

- read-only scan, reconciliation, plan/check, review, and render paths have
  focused tests with fake filesystem / command evidence;
- JSON reports expose identity, lifecycle, owner, evidence, confidence,
  reason/remediation codes, params, suggested overrides, suggested providers,
  install hints, review URLs, and command previews without ANSI color or
  localization;
- manual/vendor inventory stays opt-in;
- `mise -C tools/updev run check`, touched smoke paths in
  [VALIDATION.md](VALIDATION.md), `git diff --check`, and
  `chezmoi apply --dry-run` pass.

## Next Release: v0.6.0

`updev v0.6.0` should continue provider-general inventory after the macOS
manual/vendor decision slice. The next target is Linux-first scanner groundwork:
build the cross-platform model, fixture tests, and Linux read-only evidence
before requiring a Windows environment. Windows support can stay at fixture or
spike level unless a real runner is available.

### v0.6.0 Target Scope

1. **Linux manual inventory scanner**: add read-only evidence from `.desktop`
   files, Flatpak, Snap, AppImage, and distro package metadata where cheap.
2. **Cross-platform fixtures**: cover Linux and Windows-style evidence with fake
   runner / fixture tests so most implementation can proceed on macOS.
3. **Windows scanner spike**: define registry / winget evidence mapping and keep
   it read-only; mark it experimental until a Windows runner or real machine
   dogfood exists.
4. **Provider promotion parity**: map Linux/Windows manual app rows into the
   same plan/check action vocabulary without making broad package management a
   stable promise.
5. **Evidence quality**: improve source URLs, ownership confidence, and
   provider-native metadata for manual/vendor decisions.

### v0.6.0 Non-Goals

- default daily update inclusion for manual/vendor rows;
- automatic external installer execution;
- requiring a Windows environment for the first Linux scanner implementation;
- public `v1.0.0` stability promise.

## Later Ordering

Longer-term priorities live in [ROADMAP.md](ROADMAP.md). The short version:

1. Continue provider-general inventory after `v0.6.0` with deeper
   Linux/Windows scanners.
2. Broaden Homebrew release-age and advisory confidence beyond GitHub
   release/tag/ref URL paths.
3. Add provider-native audit paths where package identity is reliable.
4. Continue scanner hardening after OSV-Scanner, gitleaks, zizmor, Trivy, and
   Grype.
5. Prepare for public `updev v1.0.0` only after the stable macOS/Homebrew/mise
   scope is deliberately narrowed and documented.

## Path Toward Public Release

The public path should stay narrow and incremental:

1. **macOS public preview** may publish the current macOS/Homebrew/mise workflow
   before broad Linux/Windows support if the README, install path, privacy
   boundaries, and support statement clearly label provider expansion as
   experimental.
2. **Pre-v1 hardening** freezes the stable command/config/JSON surface for the
   macOS/Homebrew/mise daily workflow, labels experimental provider surfaces,
   removes or clearly hides legacy Python/private migration paths, and keeps
   compatibility tests for exit codes, JSON shape, no-color/non-TTY output, and
   config parsing.
3. **Distribution readiness** adds a reproducible install/update path
   (`go install`, release binary, or Homebrew formula), uninstall guidance,
   release notes, and a repository publication decision. A split or standalone
   repository can be chosen only if this dotfiles repository remains an
   integration smoke consumer.
4. **`updev v1.0.0`** is the first stable public release. Its stable promise is
   macOS-first daily package/tool orchestration for Homebrew, Brewfile-derived
   desired state, mise, focused security gates, inventory, sync,
   add/remove/edit/rollback, and documented JSON reports. Manual/vendor app
   inventory may remain opt-in unless it reaches the same stability bar.
