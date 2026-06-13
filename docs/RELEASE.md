# updev release target

This document tracks the current implemented release and the next release
target. Keep longer-term ordering in [ROADMAP.md](ROADMAP.md), implementation
history in git log, and tag-specific details in
[release-notes](release-notes/).

Version labels use `<tool> vMAJOR.MINOR.PATCH`, while JSON `schema_version`
stays an integer schema contract. `v0.x` releases are public preview releases;
`v1.0.0` is reserved for the first stable public contract.

## Current Release

The current implemented release is `updev v0.7.2`. `updev version`,
`updev --version`, and `updev -v` report this command contract.

`updev v0.7.2` is a public-preview hardening patch release. It keeps the supported
macOS/Homebrew/mise preview path from `v0.6.x`, adds machine-readable support
labels for providers, commands, report families, and inventory sources, and
uses those labels to clarify which surfaces are supported preview,
experimental, compatibility-only, or deferred before any `v1.0.0` contract is
considered. It also makes compatibility-ledger `support_label` output an
unconditional JSON field, matching the documented schema.

Current support promise:

- macOS/Homebrew/mise is the supported preview path.
- Linux and Windows binaries are published, but provider support outside the
  macOS/Homebrew/mise path remains experimental until fixture and real-machine
  dogfood prove the contract.
- Provider command execution stays outside the alternate-screen TUI so brew and
  mise logs remain visible.
- TTY dashboards and detail browsers are review surfaces over structured
  reports; behavior must also be represented in cached reports and JSON/text
  output where relevant.

Released patch notes:

- [v0.7.2](release-notes/v0.7.2.md): compatibility-ledger JSON contract test
  precision for support labels.
- [v0.7.1](release-notes/v0.7.1.md): compatibility-ledger JSON contract polish
  for support labels.
- [v0.7.0](release-notes/v0.7.0.md): public-preview hardening with support
  labels and compatibility-ledger support labels.
- [v0.6.5](release-notes/v0.6.5.md): full-scope v0.6.x consolidation release.
- [v0.6.4](release-notes/v0.6.4.md): text UI badge-boundary maintenance patch.
- [v0.6.3](release-notes/v0.6.3.md): provider-boundary maintenance patch.
- [v0.6.2](release-notes/v0.6.2.md): first architecture cleanup/export patch.
- [v0.6.1](release-notes/v0.6.1.md): provider-gate maintenance patch.
- [v0.6.0](release-notes/v0.6.0.md): first updev-owned Homebrew/mise provider
  gate release.

## v0.6.5 Baseline

`v0.6.5` shipped the broad preview foundations: candidate-scoped Homebrew/mise
gates, grouped TTY dashboards and detail browsers, manual/vendor app inventory
review, backend convergence suggestions, policy override review flows, embedded
agent guidance, provider compatibility ledgers, and experimental portable
inventory evidence. Tag-specific detail stays in
[v0.6.5 release notes](release-notes/v0.6.5.md).

## Current v0.7.0 Scope

`v0.7.0` turns the broad `v0.6.x` foundations into a sharper public preview by
dogfooding the real flows, labeling support levels, and closing visible
correctness and UX gaps before any stable-contract work begins.

### What v0.6.5 Already Proved

- macOS/Homebrew/mise is a usable supported preview path with updev-owned
  update gates, candidate-scoped allow/hold/review decisions, and visible
  brew/mise logs outside the alternate-screen TUI.
- The grouped TTY surfaces for `updev`, `updev last`, and `updev list` are the
  right primary human workflow: high-information summaries, drill-down details,
  action badges, Back/Home restoration, and cached-report parity.
- Manual/vendor app inventory, backend convergence suggestions, and policy
  override review flows are implemented enough to dogfood, but still need
  stricter public labels and fewer environment-specific assumptions.
- Agent guidance, optional agent/manual enrichment boundaries, docs drift
  checks, release notes, public export, and binary releases are now part of the
  release discipline.
- Linux and Windows evidence is fixture-backed and experimental. It is useful
  groundwork, not a support promise.

### v0.7.0 Policy Decisions

These decisions are fixed for the `v0.7.0` release unless a later dogfood
finding proves them wrong:

- **Release shape**: `v0.7.0` is public-preview hardening, not a broad feature
  expansion and not a stable `v1.0.0` contract.
- **Support labels**: macOS/Homebrew/mise and the core update workflow are
  supported preview; Linux scanners, Windows `winget export` evidence, and
  agent/manual enrichment remain experimental; `brewfile` low-level commands
  remain compatibility surfaces; external/vendor installer execution, dynamic
  provider plugins, and stable Linux/Windows update providers remain deferred.
- **Linux/Windows**: Linux can advance through fixture plus container/VM dogfood
  but does not become supported preview in `v0.7.0`. Windows stays fixture/spike
  unless a real runner or machine validates the assumptions.
- **Agent enrichment**: agent/manual enrichment stays default-off. It may be
  callable from manual-app TUI flows, but output must remain schema-validated,
  draft-only, and explicitly user-reviewed before it affects desired state.
- **Provider strictness**: reduce avoidable review noise, but do not allow weak
  evidence. Item-scoped allow/hold/review is mandatory where the provider can
  apply candidates individually.
- **UX scope**: focus on `updev`, `updev list`, `updev last`, manual inventory,
  backend convergence, security details, policy review, and doctor/dependency
  diagnostics. Do not replace the TUI architecture wholesale.
- **Architecture scope**: continue thinning `internal/cmd` and splitting
  packages at file-count pressure points, but avoid abstraction-only rewrites
  that do not directly support the release criteria.
- **v1 timing**: do not freeze CLI/config/JSON as stable in `v0.7.0`; instead
  document which surfaces look freeze-ready and which remain experimental.

### v0.7.0 Scope

1. **Support-level labeling**
   - Label every provider, command group, report field family, and inventory
     source as supported preview, experimental, compatibility, or deferred.
   - Surface those labels in docs and, where useful, in CLI/TUI diagnostics.
   - Keep `v1.0.0` language out of user-facing promises until the labels have
     been dogfooded.

2. **Daily workflow hardening**
   - Dogfood `updev`, `updev list`, `updev last`, `inventory`, `security`,
     `doctor dependencies`, `policy`, and `skill/help agent` as the public
     preview workflow.
   - Fix navigation, focus restoration, route filtering, badge accuracy,
     Japanese text, and summary/detail consistency regressions found during
     real TTY use.
   - Preserve provider log streaming: provider commands may run before or after
     TUI review, but brew/mise logs must not disappear behind an alternate
     screen.

3. **Provider evidence correctness**
   - Reduce noisy `review` decisions where updev can reliably prove release-age,
     advisory, ownership, and backend metadata.
   - Keep unresolved or opaque evidence visible with actionable reason codes
     instead of broad provider-wide blocks.
   - Continue item-scoped safety: one held cask/tool must not block unrelated
     safe candidates when the provider supports scoped application.

4. **Portable inventory dogfood**
   - Run Linux read-only inventory scanners against at least fixture and
     container/VM evidence before promoting any Linux path beyond experimental.
   - Keep Windows at fixture/spike level unless a real runner or machine proves
     the assumptions.
   - Remove or gate any machine-local prose, hardcoded app knowledge, or
     repository-specific default that would make the public tool behave like one
     person's Mac.

5. **Policy and compatibility ergonomics**
   - Make policy add/edit/list, shadowed-rule diagnostics, and compatibility
     ledger output easier to interpret from both human CLI/TUI flows and agent
     JSON flows.
   - Keep public issue creation opt-in. Local/CI drift detection can produce
     artifacts, but it must not post without explicit repository and credential
     policy.

6. **Architecture cleanup for maintainability**
   - Continue shrinking `internal/cmd` only where the target package can own the
     behavior without importing command/TUI report types.
   - Split packages that hit their file-count ceiling before adding broad new
     provider surfaces.
   - Prefer shared text/rendering and reason-code helpers over one-off prose in
     command handlers.

7. **Public docs and release discipline**
   - Keep README focused on the public problem/strengths/install/workflow, not
     dotfiles-specific history.
   - Keep `RELEASE.md` current-state focused and move tag details to
     `docs/release-notes/<tag>.md`.
   - Keep docs-check coverage for embedded agent docs, release notes, source
     structure, and public export assumptions.

### v0.7.0 Non-Goals

- stable `v1.0.0` command/config/JSON promises;
- stable broad Linux or Windows provider support;
- default execution of agent/manual enrichment;
- dynamic provider plugins;
- external/vendor installer execution by default;
- provider-wide writes when item-scoped evidence is required;
- moving the canonical source out of this dotfiles repository.

### v0.7.0 Release Criteria

- [x] `RELEASE.md`, `ROADMAP.md`, README, and public export docs describe
  `v0.7.0` as preview hardening, not v1 readiness.
- [x] Every provider and major command surface has an explicit support label:
  supported preview, experimental, compatibility, or deferred.
- [x] Real TTY dogfood covers `updev`, `updev list`, `updev last`, manual
  inventory, backend convergence, security details, policy review, and doctor
  dependencies.
- [x] TTY/navigation fixes are backed by package tests or stable e2e/fixture
  checks where practical.
- [x] Provider evidence review noise is reduced where reliable metadata is
  available; remaining opaque paths produce actionable reason codes.
- [x] Candidate-scoped allow/hold/review remains verified for Homebrew and mise
  bump flows.
- [x] Linux inventory remains experimental unless fixture plus container/VM
  dogfood proves the scanner assumptions.
- [x] Windows inventory remains fixture/spike unless real runner or real-machine
  validation is available.
- [x] Machine-local app prose and repository-specific public defaults are either
  removed, gated, or marked local-only.
- [x] Policy UX and compatibility ledger output are documented and readable from
  CLI/TUI/JSON flows.
- [x] Canonical validation passes before export: `mise run check`,
  `mise run docs-check`, `git diff --check`, and `chezmoi apply --dry-run`.
- [x] Public export to `webkaz-labs/updev` passes `scripts/check-docs.sh`,
  `go test ./...`, `go vet ./...`, `go mod verify`, and `go build ./...`.

## Later Ordering

Longer-term priorities live in [ROADMAP.md](ROADMAP.md). After `v0.7.0`, later
work should focus on narrowing and hardening:

1. Decide the stable `v1.0.0` macOS/Homebrew/mise contract only after `v0.7`
   dogfood proves which preview surfaces are stable enough to freeze.
2. Promote Linux providers only after fixture coverage and real
   container/VM/machine dogfood prove the read-only scanner and update-gate
   contracts.
3. Promote Windows providers only after a Windows runner or real machine proves
   the fixture/spike assumptions.
4. Extract shared internals only after updev and another maintained tool need
   the same tested API.

## Public Preview Maintenance

The public preview should stay narrow and incremental:

1. **macOS-first support** remains the stable preview path while Linux/Windows
   binaries and provider expansion are clearly labeled experimental.
2. **Pre-v1 hardening** freezes the stable command/config/JSON surface for the
   macOS/Homebrew/mise update workflow, labels experimental provider surfaces,
   removes or clearly hides compatibility-only paths, and keeps compatibility
   tests for exit codes, JSON shape, no-color/non-TTY output, and config
   parsing.
3. **Distribution maintenance** keeps the mise GitHub backend install path,
   release binaries, checksums, source install path, and
   `docs/release-notes/<tag>.md` in sync. The tag workflow uses that note as the
   GitHub Release body and updates existing release notes on reruns. The
   repository-local `mise.toml` pins the Go toolchain and `mise run check`
   mirrors the public CI's module verify, vet, test, and build gates.
4. **`updev v1.0.0`** is the first stable public release. Its stable promise is
   macOS-first package/tool orchestration for Homebrew, Brewfile-derived
   desired state, mise, focused security gates, inventory, sync,
   add/remove/edit/rollback, and documented JSON reports. Manual/vendor app
   inventory may remain opt-in unless it reaches the same stability bar.
