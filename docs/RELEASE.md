# updev release target

This document tracks the current implemented release and the next release
target. Keep longer-term ordering in [ROADMAP.md](ROADMAP.md), implementation
history in git log, and detailed design in [DESIGN.md](DESIGN.md).

Version labels use `<tool> vMAJOR.MINOR.PATCH`, while JSON `schema_version`
stays an integer schema contract. `v0.x` releases are public preview releases;
`v1.0.0` is reserved for the first stable public contract.

## Current Release

The current implemented release is `updev v0.6.3`. `updev version`,
`updev --version`, and `updev -v` report this command contract.

`updev v0.6.3` is a small architecture-maintenance patch before the next
provider expansion. It keeps the `v0.6.2` macOS/Homebrew/mise public preview
contract unchanged, then makes command handlers thinner by moving more
provider-owned command and evidence boundaries into their owner packages.

`updev v0.6.2` was the follow-up patch on the `v0.6.0`/`v0.6.1`
provider-gate line. It kept the same macOS/Homebrew/mise scope, finished the
first architecture cleanup slice, and closed the release/export gaps found
after the `v0.6.1` tag.

`updev v0.6.0` completed the first updev-owned provider gate model for the
macOS/Homebrew/mise preview. It keeps the accepted routed TTY UX from
`v0.5.7` and the performance/portability bridge from `v0.5.8`, then adds
backend-specific release-age/advisory evidence for mise and candidate-scoped
strict update execution for both mise and Homebrew.

Provider command execution remains outside the alternate-screen TUI so
brew/mise logs stay visible. Post-provider review domains may refresh inside the
dashboard only when canceled or partial results cannot be mistaken for completed
cached reports. Keep detailed implementation history in git log and tag-specific
notes in [release-notes](release-notes/).

### v0.6.3 Patch Scope

- Keep the `v0.6.2` macOS/Homebrew/mise public preview contract unchanged.
- Move Homebrew and mise update command argv builders into their provider
  packages. `cmd` keeps findings-to-target selection, report mutation, and TTY
  routing, but does not rebuild provider command chains inline.
- Move Homebrew trust command argv builders, including local-metadata
  `brew trust --json=v1`, into `internal/brew`.
- Move VS Code installed-extension command, parsing, and error normalization
  into `internal/vscode`.
- Move manual inventory live cask and MAS provider probes into
  `internal/manualinventory`; `cmd` only turns scanned app records into report
  rows and actions.
- Keep `SOURCE-STRUCTURE.md` as the refactor ledger and guardrail for package
  counts, direct subprocess exceptions, and command-boundary ownership.
- Keep `ROADMAP.md` and `docs/tooling-roadmap.md` current-state focused:
  implementation history belongs in git log, and future work belongs in this
  release plan or the long-term roadmap.

### v0.6.2 Patch Scope

- `v0.6.2` completes the first P1 architecture cleanup slice while preserving
  the v0.6.0 macOS/Homebrew/mise preview contract.
- Extract the backend recommendation engine into `internal/backend` so
  provider evidence, preference policy, action planning, and rendering can
  evolve without command-local branches.
- Centralize command/path/security-gate support code that had started to grow
  in `internal/cmd`: direct subprocess detection, root/config/cache path
  resolution, the common security gate model, and update-safety cache storage.
- Move inventory annotation logic that combines provider evidence with
  root/profile policy into `internal/inventoryannotate` so command files do not
  keep accumulating report mutation helpers.
- Move manual inventory source parsing, draft/override rendering, agent
  request construction, review models, and row classification into
  `internal/manualinventory` so command files keep only CLI/TUI routing and
  write-flow wiring.
- Add stable `reason_code` / `reason_args` fields to update steps for
  strict-safety and mise-bump decisions while preserving the compatible
  human-readable `reason` field.
- Add stable `reason_code` / `reason_args` fields to core security gate
  findings for release-age holds, mise opaque backend reviews, mise native
  minimum-release-age holds, and security policy overrides while preserving the
  compatible human-readable `reason` field.
- Add source evidence to backend recommendations so JSON/detail views can
  explain why a provider rewrite is suggested.
- Replace Homebrew/mise provider mutation shell chains with runner-backed
  command plans. Update reports keep the legacy primary `command` field and add
  structured `commands` entries for multi-step provider actions such as
  Homebrew metadata/update/cleanup and mise bump preflight/apply.
- Make Homebrew 6 trust findings actionable from security detail views with
  confirmed item-scoped `brew trust --formula` / `brew trust --cask` actions.
  Whole-tap trust remains a separate confirmation-only action and update never
  auto-trusts taps. Trust findings keep the compatible `trust_command` string
  and also expose `trust_command_argv` for agents and JSON consumers.
- Harden public export checks so the split public repository does not retain
  dotfiles-only documentation links and exported Markdown local links are
  checked before release.

### v0.6.0 Scope

1. **Complete mise release-age gate**: evaluate GitHub and registry-backed mise
   candidates with updev config/env thresholds, cache keys, text/JSON evidence,
   and `allow|hold|review|block` decisions aligned with Homebrew. Supported
   evidence sources include mise core/plugin metadata where cheap, GitHub
   release/tag/ref dates for `github:` and repository-backed entries, and
   registry publish dates for high-confidence `npm:` / `cargo:` / `pipx:`
   identities.
2. **Provider-native policy evidence**: report mise `minimum_release_age` as
   evidence, but keep updev-owned gate decisions independent and explainable.
   If mise's own policy already held an update, show that as provider evidence
   rather than silently treating it as an updev decision. Detect native holds
   with two batch provider probes, not per-tool `mise latest` calls: compare
   normal `mise outdated --json --cd <root>` with
   `MISE_MINIMUM_RELEASE_AGE=0d mise outdated --json --cd <root>`. Rows present
   only in the age-disabled result, or rows whose age-disabled `latest` is
   newer than the normal `latest`, are shown as mise-native release-age holds.
3. **Provider-wide gate contract**: document the common fields every provider
   gate should expose: candidate identity, release date/age, min age, evidence,
   policy source, decision, confidence, and remediation.
4. **Unsupported/opaque candidate behavior**: keep unsupported, opaque, or
   low-confidence mise candidates as `review` in strict mode. Do not allow a
   candidate only because evidence is unavailable, and do not guess release age
   from a version string.
5. **Security review UX parity**: make mise held/review candidates visible in
   the same dashboard/list/detail action surfaces as Homebrew findings. Rows
   should show compact badges, expanded evidence, and safe temporary policy
   actions with confirmation.
6. **Pinned-version visibility**: expose read-only mise `--bump` candidates
   separately from the normal update gate. Exact pins can make
   `mise outdated --json` and `mise upgrade --dry-run` report no mutation while
   `mise outdated --json --bump` has newer versions. These rows are not
   automatic update candidates, but `updev list`, backend convergence, and
   security detail should show them as pinned-version update opportunities,
   using mise's JSON `bump` field as the source of truth. Rows with
   `bump: null`, such as `node = "lts"`, remain desired state aliases and are
   not rewritten by the bump gate. Major/minor prefix selectors such as
   `node = "24"` or `node = "24.16"` follow mise's own `--bump` semantics,
   including mise-native `minimum_release_age` holds detected by comparing
   normal and age-disabled `--bump` output. `[update.mise_bump].mode` controls
   how far updev goes: `off` hides the normal workflow integration, `manual`
   shows item-scoped confirmed actions, `safe` also offers a confirmed safe
   batch action, and `auto` automatically applies only safe bump candidates
   during the normal update workflow. Safe item rows should expose a confirmed
   bump action that previews and then runs the scoped provider command
   (`mise upgrade --bump <tool>`). Safe batch and automatic modes must run an
   explicit scoped tool list (`mise upgrade --bump <tool...>`), never a broad
   unscoped bump. Held/review rows should route through security policy review
   before any write action is enabled, and automatic mode must skip them.
7. **Homebrew greedy gate parity**: align Homebrew safety evidence with the
   actual update command. Because `updev` runs `brew upgrade --greedy`, the
   safety gate must either use `brew outdated --json=v2 --greedy` or compare
   normal and greedy JSON output so casks with `version :latest` or
   `auto_updates true` are not updated without safety visibility. Keep
   `HOMEBREW_NO_INSTALL_FROM_API=1` on Homebrew JSON probes so Homebrew 6
   safety discovery reads local tap metadata instead of failing on unavailable
   internal package JSON endpoints such as `packages.dunno_sequoia.jws.json`.
8. **Candidate-scoped strict updates**: strict mode must not treat one
   too-new candidate as a provider-wide block when safe candidates remain.
   If the installed version is `1`, provider metadata says `2` is old enough,
   and a newer `3` is still inside the release-age window, updev should apply
   the safe `1 -> 2` candidate where the provider can do that and keep `3`
   visible as held. For mise, scoped
   `mise upgrade --minimum-release-age <Nd> <tool...>` uses the updev-configured
   age policy so it does not depend on global mise native settings; native
   `minimum_release_age` holds are still surfaced when present. For Homebrew,
   normal `brew upgrade` cannot generally install an older intermediate release
   for the same formula/cask, so updev
   must upgrade only gated local-metadata candidates with
   `HOMEBREW_NO_AUTO_UPDATE=1`, refresh metadata only after scoped upgrades or
   as a metadata-only no-candidate step, immediately re-run the Homebrew gate
   after metadata-only refresh without stale outdated caches, skip held Homebrew
   packages while continuing scoped upgrades for other allowed Homebrew
   candidates, and explain the provider limitation in the skipped evidence.
9. **Documentation drift checks**: add a focused local/CI check for high-value
   mirrors such as release-note presence for tags, embedded agent skill output,
   README links, and mise/CI validation parity. Expand later only where drift
   has caused real maintenance risk.
10. **Evidence quality**: improve source URLs, ownership confidence, and
   provider-native metadata for manual/vendor decisions.

### v0.6.0 Non-Goals

- default update inclusion for manual/vendor rows;
- automatic external installer execution;
- requiring a Windows environment or runner;
- claiming Linux/Windows provider support as stable;
- silently mutating mise provider settings;
- public `v1.0.0` stability promise.

### v0.6.0 Release Criteria

These criteria mirror the full v0.6.0 scope. They must remain checked before
tagging or mirroring the release.

- [x] `updev security gate --provider mise --format json` reports mise
  `minimum_release_age` provider evidence even when no candidate is pending.
- [x] mise-native `minimum_release_age` holds are detected with batch
  `mise outdated --json` comparison against an age-disabled provider probe, so
  item-level holds remain visible without per-tool `mise latest` probes.
- [x] Provider gate docs define the shared candidate identity, release
  date/age, minimum age, evidence, policy source, decision, confidence, and
  remediation contract for current and future providers.
- [x] Mise held/review candidates are visible from the same dashboard, list,
  detail, and security action surfaces as Homebrew findings, including compact
  badges, expanded evidence, and confirmed temporary policy actions.
- [x] Read-only mise pinned-version opportunities from `mise outdated --bump`
  are visible outside the mutation gate, including age-gated vs age-disabled
  `--bump` differences.
- [x] Safe mise pinned-version rows can be bumped from TTY detail/action flows
  with scoped dry-run preview, confirmation, and post-action report refresh.
- [x] `[update.mise_bump].mode = "auto"` runs only safe mise bump candidates
  from the normal `updev` workflow with a dry-run preflight, scoped provider
  command, release-age/security gates still active, and skipped held/review
  candidates visible in the final report.
- [x] Scoped mise bumps for `npm:*` tools avoid npm's `--before` /
  `min-release-age` conflict by using a temporary npm user config that keeps
  registry/auth entries while removing npm release-age keys for that command.
- [x] Homebrew update safety covers the same candidate class as
  `brew upgrade --greedy`, including greedy-only cask candidates, while keeping
  the local-tap JSON fallback that avoids Homebrew package API 404s.
- [x] Homebrew 6 tap trust gaps are diagnosed and actionable: doctor checks
  `brew trust --json=v1` through local tap metadata, compares it with
  non-official `Brewfile.tmpl` tap/formula/cask entries, and security posture
  rows show preferred item-scoped trust commands. Security detail actions can
  run confirmed item-scoped `brew trust --formula` / `brew trust --cask`
  commands; whole-tap trust requires separate confirmation and update never
  auto-trusts anything.
- [x] Strict update execution is candidate-scoped: safe mise candidates can
  update while newer mise-native age holds remain visible, and Homebrew applies
  allowed packages while skipping held packages instead of blocking the whole
  provider. Homebrew strict upgrades do not run `brew update` before package
  mutation, so continuously released packages can age in using local metadata
  instead of being replaced by a fresh latest candidate every run. If no
  package candidate is pending before metadata refresh, Homebrew is refreshed
  and re-gated in the same run so auto mode is not forced to wait for another
  invocation. If only held/review Homebrew candidates are pending, updev may
  refresh metadata but does not run an unscoped provider-wide upgrade; those
  package names remain visible as item-level skipped rows. Homebrew
  intermediate release installs remain unsupported unless Homebrew itself
  exposes a safe versioned path.
- [x] GitHub-backed, explicit `aqua:`, mise-registry `aqua`, and selected core
  runtime (`go`, `node`, `rust`) candidates can become `allow` or `hold` from
  release/tag/ref age evidence; missing GitHub evidence stays `review`.
- [x] `npm:`, `cargo:`, and `pipx:` mise candidates can become `allow` or
  `hold` from registry publish/upload age evidence plus basic provenance
  checks; deprecated, yanked, missing-version, missing-maintainer, or
  missing-repository candidates stay `review`.
- [x] vfox-backed mise candidates are resolved through a data-driven provider
  metadata registry, not one-off tool branches. The first registry entry maps
  `vfox:mise-plugins/vfox-gcloud` to Google Cloud CLI vendor release notes;
  candidates become `allow` or `hold` only when the configured resolver returns
  a release date for the exact candidate version. Missing resolver entries,
  unavailable vendor metadata, parse failures, or unknown upstream evidence stay
  `review`.
- [x] Unsupported or opaque mise backends stay `review` in strict mode and are
  never allowed by version-string guessing.
- [x] Local policy is reapplied after cache load, and text/JSON output keeps
  candidate identity, release date/age, minimum age, evidence, decision,
  confidence, and remediation.
- [x] Focused docs drift checks cover release-note presence, README links,
  agent guidance files, release workflow expectations, and mise/CI validation
  parity.
- [x] Manual/vendor evidence quality is upgraded for v0.6.0: source URLs,
  ownership confidence, and provider-native metadata are improved in the
  scanner output, review plan, list/detail UX, and docs without relying on
  machine-local `docs/apps.md` assumptions.
- [x] `mise -C tools/updev run check`, `mise -C tools/updev run docs-check`,
  `git diff --check`, and `chezmoi apply --dry-run` pass before release.

### v0.6.3 Release Criteria

- [x] `main` includes the command-boundary cleanup commits:
  Homebrew/mise update command builders, Homebrew trust commands, VS Code
  installed evidence, and manual provider scans.
- [x] `tools/updev/docs/RELEASE.md` lists `v0.6.3` as the current release before
  tagging, and `tools/updev/docs/release-notes/v0.6.3.md` exists.
- [x] `tools/updev/docs/CLI.md` current-version text matches `updev v0.6.3`.
- [x] `updev version`, `updev --version`, and `updev -v` report
  `updev v0.6.3`.
- [x] `mise -C tools/updev run check`, `mise -C tools/updev run docs-check`,
  `git diff --check`, and `chezmoi apply --dry-run` pass on `main`.
- [ ] Public export to `webkaz-labs/updev` passes `scripts/check-docs.sh`,
  `go test ./...`, `go vet ./...`, and `go mod verify`.
- [x] Release notes describe this as a provider-boundary maintenance patch, not
  a new provider-support release.

### v0.6.2 Release Criteria

- [x] `updev version`, `updev --version`, and `updev -v` report `updev v0.6.2`.
- [x] Backend recommendation code is extracted into `internal/backend`, with
  source evidence carried into JSON/detail output.
- [x] Root/config/cache/security path resolution is centralized in
  `internal/updevpath`; update-safety cache storage and the common gate model
  live in `internal/securitygate`.
- [x] Direct subprocess exceptions are checked by
  `scripts/check-direct-subprocesses.sh` from `scripts/check-docs.sh`.
- [x] Homebrew and mise update mutation uses structured command plans rather
  than provider shell chains; update detail/search output includes the full
  command list while preserving the compatible primary `command` field.
- [x] `mise-bump` reports include the scoped dry-run preflight and apply
  command plans, including release-age bypass and dependency-blocked retry
  paths, without leaking temporary npm config paths.
- [x] Homebrew 6 non-official tap/package trust findings expose confirmed
  item-scoped `brew trust --formula` / `brew trust --cask` detail actions; tap
  trust remains confirmation-only and is never automatic during update. JSON
  findings also expose `trust_command_argv` alongside the compatible
  `trust_command` string.
- [x] Public export removes dotfiles-only documentation rows and
  `scripts/check-docs.sh` checks exported Markdown local links.
- [x] Public export to `webkaz-labs/updev` passes `scripts/check-docs.sh`,
  `go test ./...`, `go vet ./...`, and `go mod verify` before tagging.
- [x] `mise -C tools/updev run check`, `mise -C tools/updev run docs-check`,
  `git diff --check`, and `chezmoi apply --dry-run` pass before release.

## Next Patch: v0.6.x

The next feature patch should choose one narrow provider/inventory/agent surface
at a time:

- keep curated backend rewrite seeds in registry entries with source evidence
  surfaced in JSON/detail views, then move broader ecosystems toward
  provider-metadata resolvers. New tool-specific mappings should not be added
  as inline code branches;
- keep backend recommendation UX parity while refactoring: `updev`, `updev
  list`, `updev last`, installed inventory detail, manual inventory detail, and
  backend review should continue to show compact badges, expanded evidence,
  item-scoped actions, and correct Back/Home return paths;
- keep the common security gate report model and update-safety cache store in
  `internal/securitygate` so provider-specific gate engines can move out of
  `cmd` without changing JSON/cache contracts;
- keep path/env/root resolution centralized in `internal/updevpath`: root
  selection, updev config path, cache dir, security policy path, inventory
  override path, and Brewfile source fallback should not be reimplemented in
  command/provider packages. Continue moving provider/platform-specific path
  probes behind explicit helpers or scanner contracts;
- add `updev skill`, `updev skill --full`, and `updev help agent` only if the
  embedded `docs/agent/` source tree can stay the single source of truth;
- add read-only Linux manual inventory evidence and cross-platform fixtures
  before claiming provider support beyond macOS/Homebrew/mise;
- add a Windows scanner spike only as experimental read-only evidence;
- extend provider-general structured inventory sources after the macOS manual
  source flow remains stable;
- broaden provider-native enrichment sources without changing the agent draft
  safety boundary. New vfox/asdf-style backends must add data entries to the
  provider metadata registry, identify the resolver type (`github_release`,
  `github_tag`, `vendor_release_notes`, `vendor_json`, `package_registry`, or a
  similarly bounded source), and prove candidate-version release dates in tests;
- expand vfox provider metadata beyond the initial Google Cloud CLI fixture only
  when the backend's upstream source, release-date source, and parser contract
  can be verified. Generic vfox remains strict-mode review unless a registry
  entry resolves the exact candidate version;
- add public-repository issue automation for provider contract drift only after
  explicit credentials, repository ownership, and opt-in posting policy exist.

## Later Ordering

Longer-term priorities live in [ROADMAP.md](ROADMAP.md). The short version:

1. Continue provider-general inventory after the `v0.6.3` boundary cleanup with
   deeper read-only Linux/Windows scanners.
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
