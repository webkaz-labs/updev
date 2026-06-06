# updev release target

This document tracks the current implemented release and the next release
target. Keep longer-term ordering in [ROADMAP.md](ROADMAP.md), implementation
history in git log, and detailed design in [DESIGN.md](DESIGN.md).

Version labels use `<tool> vMAJOR.MINOR.PATCH`, while JSON `schema_version`
stays an integer schema contract. `v0.x` releases are public preview releases;
`v1.0.0` is reserved for the first stable public contract.

## Current Release

The current implemented release is `updev v0.5.7`. `updev version`,
`updev --version`, and `updev -v` report this command contract.

`updev v0.5.7` is the current dogfood UX/action polish patch on the `v0.5.x`
CLI scope. It keeps the macOS/Homebrew/mise update workflow stable while
improving the interactive dashboard, installed inventory, manual review plan,
backend convergence, security review, and cached-report re-entry. It also
includes the `v0.5.6` Japanese description-translation UX: Japanese TTY `updev`
/ `updev list` can refresh cached list descriptions through the optional Codex
CLI, `[ui].description_translation` can choose `auto`, `manual`, or `off`, and
dependency diagnostics report the optional Codex backend.

The `v0.5.x` UX/action gate is still tracked in [UX.md](UX.md). Do not treat it
as fully closed until the action-review checklist there is green in real TTY
dogfood. Keep regression commands in local test plans, not as a permanent
release log here.

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
8. **Main human entry point**: interactive `updev`, `updev list`, and
   `updev last` are the human-facing TTY entry points; `--plain` and
   `--format json` are the stable agent/script outputs. `updev` can still
   route to the manual plan when manual actions need review, but manual/vendor
   rows remain outside the default package update table.
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

## Current Patch: v0.5.7 Dogfood Polish

`updev v0.5.7` is the active dogfood polish line. The detailed UX contract and
remaining tracking checklist live in [UX.md](UX.md).

### v0.5.7 Target Checklist

- [x] **Action-rich detailed list**: detailed `updev list` / update views make
  updated, deferred, held, skipped, errored, and review-needed rows visually
  distinct, with collapsed badges for action count, update/defer counts,
  security decision, release asset status, and backend applyability. Update
  views include item-level updated/deferred rows in addition to provider log
  rows, with enough expanded evidence to decide what changed and why.
- [x] **Detail row actions foundation**: expanded rows can show numbered actions
  and execute them from that context where a safe write path exists: security
  allow/hold with default or custom reason/expiry, security allow plus provider
  rerun, manual override accept/edit/ignore, cask/MAS/vendor evidence review,
  safe backend rewrite, covered old mise-entry removal, and Brewfile ownership
  removal when mise already owns the tool.
- [x] **Manual plan operability after `updev`**: the post-`updev` manual review
  plan must not feel like a dead-end Back-only screen. Rows that can be acted on
  expose those actions clearly, and rows that are read-only explain the exact
  next command or evidence needed.
- [x] **Provider log formatting**: Homebrew and other provider stdout/stderr
  keep meaningful newlines in expanded update/log detail instead of collapsing
  into one wrapped paragraph.
- [x] **Dashboard-in-dashboard actions**: the `updev` dashboard itself should be
  an actionable detail view where feasible. Focused dashboard rows show their
  `a/1`, `2`, ... action hints before expansion, while the footer selector stays
  available for flows that cannot sensibly live inside dashboard rows.
- [x] **Manual list brew/cask suppression**: `updev list --provider manual`
  should not show Homebrew-managed GUI apps in the default Installed apps review
  bucket. Homebrew cask evidence remains available through explicit status/query
  filters.
- [x] **Backend apply path**: backend convergence should not only show
  suggestions. Safe mise backend rewrites and covered old-entry removals can be
  applied from the `updev` / `updev list` detail browser after confirmation;
  each row explains whether it is applyable or review-only. Brewfile ownership
  removal is available only when the recommended mise entry already exists;
  full brew-to-mise migration from a missing mise entry remains review-only.
- [x] **Backend preference policy**: align default tiers with mise registry
  acceptance guidance (`core`, `aqua`, `github`/`gitlab`, `conda`, then
  language package backends), keep deprecated/legacy backends such as `ubi` and
  `asdf` out of the default recommendation order, and add configurable
  `[backends].preference_order` so future providers can be ranked without code
  changes.
- [x] **Backend GitHub coverage**: infer GitHub backend candidates from scalable
  metadata sources, starting with Homebrew formula URLs and mise `cargo:` /
  `npm:` package repository metadata. Known GitHub-backed tools such as `broot`
  and `rtk` are dogfood fixtures, not one-off special cases.
- [x] **Backend candidate evidence**: metadata-inferred `mise/github` moves are
  review-only candidates, not direct recommendations. When `gh` is available,
  latest-release asset names are sampled and matched against the current
  OS/architecture; npm/cargo entries stay non-applyable until release assets,
  version mapping, and official distribution ownership are verified. Japanese
  TTY copy must label these rows as candidates. `cargo:` rows with missing or
  non-matching release assets must explain that the local cargo build should be
  kept until compatible binary release evidence exists.
- [ ] **Hands-on TTY acceptance pass**: run the actual `updev` and `updev list`
  interactive flows in a real terminal, not only renderer tests or non-TTY
  output, and verify the three dogfood UX promises end to end: focused-row action
  hints are visible before expansion, manual app rows can execute their safe
  actions from details, and backend/security detail actions can be selected,
  confirmed, and either applied or clearly rejected as review-only. When the
  live report has no actionable security finding, use a fixture-backed cached
  report via `updev last --section security` for the security
  action confirmation smoke.

The only remaining `v0.5.7` release gate is the real-terminal hands-on pass.
Automated renderer tests and PTY smoke are useful regression checks, but they do
not close this gate by themselves.

### v0.5.7 Non-Goals

- broad Linux/Windows provider implementation;
- automatic vendor installer execution;
- silent Brewfile removal after backend migration;
- changing JSON reports to execute actions implicitly;
- replacing the `v0.6.0` mise update gate scope.

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
