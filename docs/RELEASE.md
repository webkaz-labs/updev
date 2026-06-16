# updev release target

This document tracks the current implemented release and the next release
target. Keep longer-term ordering in [ROADMAP.md](ROADMAP.md), implementation
history in git log, and tag-specific details in
[release-notes](release-notes/).

Version labels use `<tool> vMAJOR.MINOR.PATCH`, while JSON `schema_version`
stays an integer schema contract. `v0.x` releases are public preview releases;
`v1.0.0` is reserved for the first stable public contract.

## Current Release

The current implemented release is `updev v0.7.10`. `updev version`,
`updev --version`, and `updev -v` report this command contract.

`updev v0.7.10` is the provider evidence quality patch after the v0.7.9
quality-tooling release. It preserves the same supported
macOS/Homebrew/mise preview scope, keeps provider log streaming outside the
alternate-screen TUI, and makes held/review provider decisions easier to trust
from text output, cached reports, and TUI detail rows.

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

Current release validation:

- [x] `mise -C tools/updev run check`
- [x] `mise -C tools/updev run docs-check`
- [x] `git diff --check`
- [x] `chezmoi apply --dry-run`
- [x] Unit coverage proves Homebrew/mise evidence rows include source or cache
  context where available.
- [x] TUI/text detail coverage proves held/review rows expose release age,
  source, cache, and next-action context without duplicating compact-list
  metadata.
- [x] Real-machine plain checks covered cached `last`, `list --details`, and
  `updev --dry-run --plain --no-interactive`.

## Next Release Target: v0.7.11

`updev v0.7.11` is not scoped yet. Pick the next small patch from
[ROADMAP.md](ROADMAP.md#near-term-order) after dogfooding v0.7.10.

Scope:

- Choose one cohesive patch-sized slice from provider evidence quality,
  scanner hardening, policy ergonomics, or support-label dogfood.
- Preserve the v0.7.10 evidence/detail parity and provider log streaming
  invariants.

Non-goals:

- stable `v1.0.0` promises;
- broad scanner default expansion;
- large architecture refactors without a focused release plan.

Release-ready criteria:

- [ ] `mise -C tools/updev run check`
- [ ] `mise -C tools/updev run docs-check`
- [ ] `git diff --check`
- [ ] `chezmoi apply --dry-run`
- [ ] Scope-specific tests and release notes exist.

Released patch notes:

- [v0.7.10](release-notes/v0.7.10.md): provider evidence quality patch with
  richer Homebrew/mise release age, source, cache, and localized detail
  evidence.
- [v0.7.9](release-notes/v0.7.9.md): agent-friendly quality tooling patch with
  promoted Go CLI standard gates, slower audit checks, and non-blocking
  agent-quality evidence.
- [v0.7.8](release-notes/v0.7.8.md): large refactor with sync/config owner
  packages, consolidated routed adapters, and stricter command package budgets.
- [v0.7.7](release-notes/v0.7.7.md): command production-file budget hardening
  before provider/scanner/policy feature growth.
- [v0.7.6](release-notes/v0.7.6.md): Japanese TUI help text consistency fix
  for the support-catalog label filter.
- [v0.7.5](release-notes/v0.7.5.md): scalability refactor completion with
  package-count gates, manual inventory platform split, owner-package
  extractions, and route-state helper consolidation.
- [v0.7.4](release-notes/v0.7.4.md): list evidence projection extraction,
  tighter evidence key matching, and v0.7.x workstream foundation checks.
- [v0.7.3](release-notes/v0.7.3.md): support-label TUI visibility, provider
  support badges, and dependency support-label text output.
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

### v0.7.3 Scope

`v0.7.3` is the support-label TUI visibility patch. It does not add new support
labels or promote experimental providers; it makes the existing v0.7 support
catalog visible in the routed TUI where it changes a human decision.

Included work:

1. Add a support-catalog routed TUI view reachable from `updev hub` and the list
   hub, with surface, label, and query filters plus expandable details.
2. Show provider support labels in dense TUI rows only when they are
   non-default: `experimental`, `compatibility`, or `deferred`.
3. Keep `supported_preview` detail-only by default so supported paths do not
   add visual noise to every row.
4. Show exact compatibility-ledger `support_label` values in
   `doctor dependencies` text/detail rows.
5. Show inventory-source support labels only in manual inventory evidence
   details, not as an always-visible list column.
6. Preserve existing routed TUI invariants: Back/Home restoration, compact
   badges, grouped high-information lists, deterministic `--plain`/JSON output,
   and provider log streaming outside the alternate-screen TUI.

Policy decisions for `v0.7.3`:

- No provider support promotion is included.
- No new support-label vocabulary is included.
- No command/report label duplication on unrelated update or inventory
  dashboards.
- CLI/JSON support catalog output remains the source of truth; TUI display is a
  human-review projection of that catalog.

## v0.7.4 Scope

`v0.7.4` was the `v0.7.x` foundation patch that started the four workstreams
below without turning the preview into a broad feature release: first reduce
structural pressure, then improve evidence and policy surfaces where the model
could already prove the behavior.

Policy decisions for `v0.7.4`:

- **Release shape**: patch release, not `v1.0.0` readiness and not provider
  expansion.
- **Primary axis**: scalability refactor first. Provider evidence, scanner, and
  policy work must fit around that without adding new command-package sprawl.
- **Package pressure**: do not add new `internal/cmd` files. If a new command
  surface is unavoidable, extract an owning package in the same change and keep
  `internal/cmd` at or below its current ceiling.
- **Provider support**: keep macOS/Homebrew/mise as supported preview.
  Linux/Windows, vfox-generic metadata, agent enrichment, and external/vendor
  installer execution remain experimental or deferred unless a later release
  explicitly promotes them.
- **Scanner defaults**: no new slow or broad scanner becomes part of the
  default update gate in this patch.
- **Policy posting**: local and CI artifacts are allowed; public issue creation
  or external posting remains opt-in only.

Included work:

1. **Scalability refactor kickoff**
   - Extracted list evidence index, item projection, key matching, and evidence
     counts into `internal/plan` so `internal/cmd` keeps report/TUI adapter
     behavior instead of owning the evidence projection domain.
   - Keep report assembly and TUI routing separated: reports expose decisions
     and evidence; TUI views only render and route those reports.
   - Update `SOURCE-STRUCTURE.md` whenever a boundary moves.

2. **Provider evidence quality audit**
   - Tightened update/security evidence key generation so version-delta rows
     and provider-prefix rows map to the intended installed item without
     carrying long raw log fragments as identity keys.
   - Kept explanations and JSON/TUI parity on the existing report surfaces. Do
     not weaken strict mode or promote opaque backends.

3. **Scanner hardening baseline**
   - Preserve existing unavailable/error/review semantics for explicit scanner
     and native-audit evidence while avoiding new default scanner gates in this
     patch.
   - Keep optional scanners non-blocking for normal update flows unless a
     provider identity is reliable enough for an item-scoped gate.

4. **Policy ergonomics foundation**
   - Preserve existing policy cleanup and review diagnostics for broad,
     duplicate, expired, shadowed, missing-reason, and missing-expiry cases from
     CLI/JSON and routed review details.
   - Keep guided edit/renew/narrow/remove expansion for later `v0.7.x` patches
     unless it can preserve route context and report parity.

`v0.7.4` release criteria:

- [x] `mise -C tools/updev run check`
- [x] `mise -C tools/updev run docs-check`
- [x] `git diff --check`
- [x] `chezmoi apply --dry-run`
- [x] Fast TTY smoke for `updev --dry-run --interactive`, `updev last`, and
  `updev list` confirms Back/Home focus restoration and visible provider logs.
- [x] `internal/cmd` file count stays at or below the documented ceiling and no
  new command-local domain logic is added without an owning-package extraction.
- [x] Held/review evidence remains item-scoped for Homebrew/mise where the
  provider can apply individual candidates.
- [x] README, `RELEASE.md`, `ROADMAP.md`, `SOURCE-STRUCTURE.md`, and release
  notes are current-state focused before tagging.

## Next v0.7.x Workstream Plan

The next `v0.7.x` cycle should keep the supported preview scope narrow while
turning the current dogfood findings into durable structure. `v0.7.8` is the
large refactor release that must perform real code movement before feature
growth continues. After that, the major workstreams continue in dependency
order: provider evidence quality, scanner hardening, and policy ergonomics.

### v0.7.5 Scope: Scalability Refactor Completion

`v0.7.5` finishes the `v0.7.x` P1 structural scalability refactor. This does
not mean every future provider is implemented; it means the codebase is no
longer blocked by `internal/cmd` or `manualinventory` file-count pressure, and
future provider, scanner, policy, and TUI work has clear owning packages.

Policy decisions for `v0.7.5`:

- **Release shape**: maintenance/refactor patch with behavior-preserving
  extraction. Do not bundle broad provider evidence expansion, scanner defaults,
  or policy feature growth into this release.
- **Definition of done**: finish the structural P1 so later `v0.7.x` work can
  add provider evidence quality, scanner hardening, and policy ergonomics
  without adding command-package sprawl.
- **Package budgets**: reduce `internal/cmd` below its current ceiling and
  split `internal/manualinventory` before adding more manual/portable scanner
  logic.
- **Provider logs**: keep provider-native command output outside the
  alternate-screen TUI. Refactors must not hide Homebrew/mise logs or delay the
  first useful screen unnecessarily.
- **TUI foreground process boundary**: evaluate Bubble Tea `ExecProcess` as the
  future common mechanism only for foreground external processes launched from a
  TUI action, such as `$EDITOR`, policy/override editing, or explicit
  agent-enrichment review. Do not move provider update, scan, or audit execution
  away from the runner/log-streaming path in this release.
- **External CLI contracts**: provider-native command construction, structured
  parsing, and provider-specific error normalization must live with the provider
  owner package or an explicit scanner/native-audit owner. Command handlers may
  adapt reports and routes, but must not parse provider output or build
  provider command chains inline.

Included work:

1. **Manual inventory package split**
   - Split `internal/manualinventory` by source/platform so the core package
     moves below its temporary ceiling.
   - Kept macOS, Linux fixture-backed, Windows fixture-backed, structured
     source parsing, override draft, and agent-enrichment contracts data-backed
     and tested under the owning package.
   - Does not add repository-local app prose, machine-local defaults, or
     tool-name branches as public defaults.

2. **Routed TTY state extraction**
   - Moved reusable route-state cache helpers into `reviewui` where behavior is
     not domain-specific.
   - Keeps `cmd` route adapters thin: they map report routes to domain views and
     final write operations only.
   - Documents where Bubble Tea `ExecProcess` should be used later for
     foreground TUI actions, but avoid replacing existing provider update/log
     execution during the structural refactor.
   - Preserves accepted behavior for `updev`, `updev last`, `updev list`,
     manual inventory, backend convergence, security details, and policy
     review.

3. **Update/security report assembly extraction**
   - Moved inventory collection/cache/sort and support report construction out
     of `cmd` into `inventoryrun` and `support`.
   - Keeps final report mutation, security route construction, and write
     actions in `cmd` until a broader report model owns them.
   - Preserves JSON/text/TUI parity for held, review, allow, updated, skipped,
     and error decisions.

4. **Badge and evidence display boundary cleanup**
   - Keeps badge priority, compact labels, width truncation, color, and Nerd Font
     marker decisions in `textui`.
   - Moved Homebrew advisory mapping and posture review counts into the `brew`
     owner package.
   - Ensures a new evidence class or badge is not implemented separately in
     multiple command files.

5. **External CLI and parser audit**
   - Re-runs the direct subprocess audit and keeps all direct `exec.Command`
     exceptions documented in `ARCHITECTURE.md` and enforced by
     `scripts/check-direct-subprocesses.sh`.
   - Prefers structured provider output and library parsers over ad hoc text
     parsing: JSON with `encoding/json`, TOML/manifest parsing through the
     existing structured source helpers or a deliberate parser package, and
     terminal width/color through `textui`.
   - For every provider/scanner command touched by the refactor, verifies argv/env
     construction, stdout/stderr parsing, timeout/error normalization, and tests
     are owned by the provider/scanner package rather than `cmd`.

`v0.7.5` exit criteria:

- `internal/cmd` has fewer than 50 Go files, with a target of `<= 45`.
- `internal/manualinventory` core has fewer than 20 Go files, with a target of
  `<= 15` for the remaining core package after platform/source split.
- No new provider, scanner, policy, or parser business logic is added to
  `internal/cmd`.
- Direct subprocess exceptions remain synced between `ARCHITECTURE.md` and
  `scripts/check-direct-subprocesses.sh`.
- External CLI results touched by the release are parsed through structured
  provider/scanner owners, not route/render handlers.
- Fast validation and smoke checks pass: `mise -C tools/updev run check`,
  `mise -C tools/updev run docs-check`, `git diff --check`,
  `chezmoi apply --dry-run`, and fast TTY smoke for `updev --dry-run
  --interactive`, `updev last`, and `updev list`.
- `SOURCE-STRUCTURE.md`, `ARCHITECTURE.md`, and `ROADMAP.md` reflect the final
  ownership boundaries before tagging.

### v0.7.7 Scope: Architecture Hardening Before Feature Growth

`v0.7.7` deliberately pauses feature growth and hardens the structural guardrail
that prevents the next provider/scanner/policy work from refilling
`internal/cmd`. This release is a guardrail patch, not the large code-movement
refactor itself. The goal is to make the next feature slices cheaper by
enforcing the production command-file budget before more evidence and TUI
behavior is added.

Policy decisions for `v0.7.7`:

- **Release shape**: behavior-preserving architecture/refactor patch. Do not
  bundle provider evidence expansion, scanner defaults, or policy UX features
  into this release.
- **Definition of done**: future provider evidence quality, scanner hardening,
  and policy ergonomics work can land without adding command-package sprawl or
  duplicating TUI route/report assembly code.
- **Package budgets**: enforce `internal/cmd` production files at `<= 30` and
  track command tests as a separate `<= 20` budget. The current production count
  is 27; future feature work must not exceed the production budget.
- **Report ownership**: move update/security/list/manual/backend report
  assembly and provider-neutral grouping into owner packages. `cmd` should keep
  command parsing, option adaptation, final command-specific route wiring, and
  write boundaries only.
- **TUI route ownership**: move reusable dashboard/table/detail route stack,
  focused-action stability, filter state, confirmation state, pending writes,
  and Back/Home restoration behavior into `reviewui` or a small review-route
  owner. Domain adapters map reports to rows; they should not own generic route
  mechanics.
- **Provider execution boundary**: preserve provider log streaming through the
  existing runner/update path. Do not replace Homebrew/mise update, scan, or
  audit execution with Bubble Tea `ExecProcess`; reserve `ExecProcess` for
  future foreground TUI actions such as editor or explicit agent review flows.
- **External CLI contracts**: keep command construction, timeout/error
  normalization, and structured output parsing in provider/scanner packages.
  Any direct subprocess exception must remain documented and checked.

Included work:

1. **Command package thinning**
   - Split the remaining command-local update/security/list/manual/backend
     assembly into domain packages with tests.
   - Remove command-local helpers that compute provider-neutral decisions,
     evidence grouping, badge presence, reason derivation, or support labels.
   - Keep localized text and final CLI/TUI route labels at the boundary.

2. **Common TUI routing core**
   - Extract shared route stack and state restoration semantics used by
     `updev`, `updev last`, `updev list`, manual inventory, backend
     convergence, security details, and policy review.
   - Ensure returning from child views restores the parent row/filter/focused
     action without flicker, disappearing focused actions, or accidental
     navigation on the next keypress.
   - Keep existing grouped high-density list design; do not redesign the UX in
     this refactor.

3. **Report assembly boundaries**
   - Make cached reports, JSON/text summaries, and TUI route rows consume the
     same structured assembly results.
   - Move provider-neutral reason grouping and summary preparation into
     packages that can be tested without TUI imports.
   - Keep write/apply decisions command-scoped until policy/write flows get a
     dedicated owner.

4. **Placement checks**
   - Tighten `SOURCE-STRUCTURE.md` and `scripts/check-source-structure.sh`
     after the split lands so `internal/cmd` cannot drift back above the new
     target.
   - Add focused contract tests for the extracted report/TUI owners rather than
     adding more broad command tests.

`v0.7.7` exit criteria:

- `internal/cmd` production files are at or below 30, and command test files are
  checked separately.
- `scripts/check-source-structure.sh` enforces the production command budget
  separately from broad command regression tests.
- Provider logs remain visible outside the alternate-screen TUI.
- `mise -C tools/updev run check`, `mise -C tools/updev run docs-check`,
  `git diff --check`, `chezmoi apply --dry-run`, and public export validation
  pass before tagging.

### v0.7.8 Scope: Required Large Refactor

`v0.7.8` is the required large refactor release. It must include meaningful
production Go code movement and must not be satisfied by docs-only,
guardrail-only, or cosmetic cleanup. The release exists because `v0.7.7`
proved the command-package budget but did not complete the deeper extraction.

Policy decisions for `v0.7.8`:

- **Release shape**: behavior-preserving refactor release. Do not bundle
  provider evidence expansion, scanner defaults, policy UX features, or support
  promotions.
- **Definition of done**: command code is visibly thinner and future provider,
  scanner, policy, and TUI work can land without adding new command-local
  report assembly or route mechanics.
- **Required code movement**: move complete owner contracts, with tests, out of
  `internal/cmd`. A release with only docs/check changes is invalid.
- **Package budgets**: reduce `internal/cmd` production files from 27 to
  `<= 20` and command test files from 18 to `<= 14`. Tighten
  `scripts/check-source-structure.sh` and `SOURCE-STRUCTURE.md` only after the
  split lands.
- **Report ownership**: update/security/list/manual/backend summaries and
  provider-neutral grouping must be assembled by owner packages. Command files
  may adapt command options, localization labels, and final write boundaries,
  but must not compute provider-neutral decisions, evidence grouping, badge
  presence, or reason derivation.
- **TUI route ownership**: generic route stack, Back/Home restoration, filter
  state, focused-action stability, confirmation state, pending writes, and
  action consumption must live in `reviewui` or a small route owner package.
  Domain packages expose rows/actions; command code wires routes to commands.
- **External CLI boundary**: provider command construction, timeout/error
  normalization, streaming decisions, and structured output parsing stay in
  provider/scanner packages or documented direct-subprocess exceptions.
- **Performance and logs**: preserve existing provider log streaming. Do not
  move Homebrew/mise update, scan, or audit execution into the alternate-screen
  TUI or Bubble Tea foreground execution.

Included work:

1. **Report assembly extraction**
   - Extract provider-neutral update/security summary assembly into owner
     packages that can be tested without importing command/TUI route files.
   - Make cached reports, JSON/text summaries, and TUI route rows consume the
     same structured summary data.
   - Move owner-specific tests with the extracted packages instead of growing
     broad command tests.

2. **List/manual/backend view-model extraction**
   - Move list, manual inventory, and backend convergence row grouping,
     evidence-to-action projection, and compact badge presence calculation out
     of command files.
   - Keep Japanese/localized display strings at the CLI/TUI boundary while
     reason codes and classification remain owner-owned.

3. **Common routed TUI core**
   - Consolidate routed dashboard/table/detail behavior that is shared by
     `updev`, `updev last`, `updev list`, manual inventory, backend
     convergence, security details, and policy review.
   - Preserve parent row, filter, scroll, and focused action when returning
     from child views.
   - Prevent the known class of regressions where a return from a child view
     clears the top summary, drops focused actions, or causes the next down-key
     to navigate unexpectedly.

4. **Command test relocation**
   - Move owner-specific behavior tests from `internal/cmd` to the extracted
     owner packages.
   - Leave only command parsing, CLI option precedence, top-level routing, and
     integration-style contract tests in `internal/cmd`.

5. **Guardrail tightening after extraction**
   - Update `SOURCE-STRUCTURE.md` with final package counts and owner
     boundaries.
   - Tighten `scripts/check-source-structure.sh` to the new `internal/cmd`
     production/test budgets after the code movement is complete.
   - Keep direct subprocess and docs drift checks passing.

6. **Agent-friendly lint gates**
   - Keep `mise run lint` wired into the standard local `check` so agents catch
     formatting, bug-class static analysis, and shell script issues before a
     large refactor accumulates noise.
   - Keep full Staticcheck adoption incremental: `SA*` bug-class analyzers are
     release-blocking now; broader style/unused-code findings are cleanup input
     for the refactor, not a noisy all-at-once gate.

`v0.7.8` exit criteria:

- [x] `internal/cmd` production files are `<= 20`; command test files are
  `<= 14`.
- [x] At least seven current production command files are removed, merged, or
  reduced to thin adapters because their behavior moved to owner packages.
- [x] The diff contains meaningful non-doc production Go movement. Docs-only,
  release-note-only, or guardrail-only changes cannot satisfy this release.
- [x] No new provider/scanner/policy/parser business logic is added to
  `internal/cmd`.
- [x] Generic TUI route mechanics and focused-action restoration remain owned
  by `reviewui`; command-specific files keep only route wiring.
- [x] Cached reports, JSON/text output, and TUI detail views still agree on
  held, review, allow, updated, skipped, and error decisions through existing
  command regression tests.
- [x] Provider logs remain visible outside the alternate-screen TUI.
- [x] `mise -C tools/updev run check`, `mise -C tools/updev run docs-check`,
  `git diff --check`, `chezmoi apply --dry-run`, and public export validation
  pass before tagging.

### 1. Scalability Refactor

Goal: make future provider and TUI work cheaper without changing the public
workflow.

- Keep `internal/cmd` as command parsing, command-local adapters, rendering, and
  route wiring only.
- Move complete domain contracts, not single helper functions, into owning
  packages such as `backend`, `securitygate`, `manualinventory`, `mise`, `brew`,
  `registryaudit`, `nativeaudit`, `reviewui`, and `textui`.
- Keep report builders independent from terminal interaction; TUI views consume
  reports rather than computing behavior that JSON/text cannot see.
- Preserve provider log streaming and routed Back/Home state while extracting
  code.
- Add or extend placement checks only where they catch real drift without
  forcing artificial package splits.
- Audit external CLI execution and parsing during extraction: provider commands
  must go through `runner` or a documented exception, structured provider output
  should be parsed by the provider/scanner owner package, and command handlers
  should not own ad hoc parser logic.

Exit criteria:

- New broad provider/security work can be added without growing
  `internal/cmd` file count or duplicating render/reason helpers.
- The standard validation suite and fast PTY smoke still pass after each slice.
- Source placement decisions are reflected in `ARCHITECTURE.md` /
  `SOURCE-STRUCTURE.md` when boundaries change.

### 2. Provider Evidence Quality

Goal: reduce noisy `review` decisions only where updev can prove candidate
identity, age, ownership, and source confidence.

- Improve Homebrew evidence for cask URL/homepage provenance, tap trust,
  disabled/deprecated metadata, release-age timestamps, and item-scoped trust
  commands.
- Improve mise evidence for registry-backed backends, core runtime aliases,
  vfox provider metadata, npm/cargo/pipx publish dates, and GitHub asset/source
  confidence.
- Keep unsupported or opaque backends as `review`; do not weaken strict mode to
  reduce noise.
- Show source URLs, cache age, release dates, support labels, and reason codes
  in both cached reports and routed detail views.
- Maintain item-scoped allow/hold/review. One risky package must not block
  unrelated safe candidates when the provider can apply candidates individually.

Exit criteria:

- A dogfood run can explain every held/review package with stable reason codes
  and source evidence.
- Safe Homebrew/mise candidates apply in scoped commands when other candidates
  remain held.
- Opaque cases remain visible, actionable, and review-only.

### 3. Agent-Friendly Quality Tooling

Goal: make agent-produced changes easier to review by catching deterministic
format, bug-class, supply-chain, and "looks correct but is sloppy" problems
before release review.

Adopt the tooling in phases so `check` stays useful for daily work and noisier
security/style tools can prove themselves before becoming release-blocking:

1. **P1: low-noise local gates**
   - Add `golangci-lint v2` with a curated, low-noise config. Start with
     linters that are actionable for agents: `govet`, `staticcheck`,
     `misspell`, `nolintlint`, `noctx`, `bodyclose`, `tparallel`, and
     whitespace/format checks.
   - Add stricter formatting through `gofumpt` plus import grouping through
     `goimports` or `gci`. Keep this compatible with public export, because
     module path rewriting can change import layout.
   - Keep `gosec`, broad `errcheck`, and broad `unused` out of the default
     blocking set until their signal/noise ratio is proven on this codebase.

2. **P2: vulnerability and public-repo supply-chain checks**
   - Add `govulncheck` as `mise run vuln` and include it in a slower
     `mise run audit` path. Start as a release/scheduled/manual gate before
     deciding whether it belongs in every PR check.
   - Add public-repo security hygiene checks where useful: CodeQL,
     Dependabot for Go modules and GitHub Actions, GitHub Actions SHA pinning
     review, and release artifact attestation/verification.
   - If SARIF upload is enabled, keep it public-repo-only and do not require
     local developer credentials.

3. **P3: agent-code quality audits**
   - Trial deterministic "AI slop" style audits such as `aislop` as
     non-blocking `audit` evidence. Target patterns that ordinary tests miss:
     narrative comments, dead scaffolding, oversized functions, duplicated
     helpers, and generic placeholder names.
   - Promote a finding class to blocking only after repeated dogfood shows it
     is accurate, actionable, and not duplicating existing lint/test coverage.

Operational rule:

- Keep `mise run check` fast and low-noise for ordinary agent iterations.
- Keep `mise run audit` / scheduled CI for slower vulnerability, SAST, and
  AI-quality checks.
- The proven subset is now promoted into the shared Go CLI standard in
  `docs/go-cli/`: fast low-noise local gates plus slower release/scheduled
  audit gates. Keep broad SAST, broad complexity, and generic AI-quality
  findings non-blocking unless a later release promotes a narrow finding class.

Current implementation status:

- P1 local gates are implemented for daily use: `fmt-check` uses pinned
  `gofumpt` plus `goimports`, `golangci-lint` runs a curated v2 low-noise
  linter set, and `lint` remains part of the normal `check` task.
- `golangci-lint` is run through a pinned script instead of a mise tool entry
  because mise's GitHub artifact-attestation install path can fail in
  restricted agent sandboxes before the linter starts.
- P2/P3 are implemented as slower audit surfaces: `vuln` runs pinned
  `govulncheck`, `audit` combines vulnerability, GitHub Actions supply-chain,
  and non-blocking agent-code-quality checks, `agent-quality` runs pinned
  `aislop` when Node/npm are available plus built-in heuristics, and scheduled
  public-repo workflows cover CodeQL, Dependency Review, Dependabot updates,
  and release asset provenance attestation.
- The current `aislop` finding baseline and blocking-promotion policy live in
  [SOURCE-STRUCTURE.md](SOURCE-STRUCTURE.md#agent-quality-audit-ledger).
- Action refs intentionally remain on trusted version tags, not immutable SHAs,
  during preview. `supply-chain` reports this as review evidence so the project
  can decide later whether SHA pinning is worth the maintenance cost.

Exit criteria:

- Agent changes fail early for formatting/import drift, obvious bug-class
  static analysis, and docs/source-structure drift.
- Release review can cite one command for fast checks and one command for
  deeper audit checks.
- The Go CLI standard records only tools that have proven low-noise behavior
  in updev, not aspirational one-off experiments.

### 4. Scanner Hardening

Goal: make explicit scanner paths more useful without turning routine updates
into a slow full security platform.

- Keep update gates focused on pending candidates; scanners remain explicit
  scan/review evidence unless a provider identity is reliable and cheap enough
  for the gate.
- Continue OSV-Scanner, gitleaks, zizmor, Trivy, and Grype hardening with stable
  unavailable/error semantics and bounded runtime.
- Add provider-native audit evidence where package identity is reliable before
  adding broader scanner defaults.
- Keep Syft and Prowler future explicit commands, not default update gates.
- Surface scanner coverage, unavailable tools, and remediation through
  `doctor dependencies`, `security scan`, and TUI detail rows.

Exit criteria:

- Optional missing scanners do not fail normal updates.
- Scanner findings carry enough package/source identity to avoid misleading
  holds.
- Slow or broad scans remain opt-in and documented.

### 5. Policy Ergonomics

Goal: make local allow/review/hold/block rules understandable and maintainable
from both human TUI flows and agent/CI JSON flows.

- Improve policy add/edit/list/cleanup flows with guided actions from security
  details and backend/manual review rows.
- Keep reasons and expiries mandatory for temporary allow rules; show broad,
  duplicate, shadowed, expired, and missing-reason diagnostics.
- Add diagnostic indexes or filters so users can answer "why is this held?" and
  "which rule applies?" without reading raw JSON.
- Keep public issue creation and external posting opt-in; local/CI artifacts can
  be generated without network posting.
- Ensure agent-oriented JSON exposes the same decision, reason, rule id, expiry,
  and remediation that the TUI shows.

Exit criteria:

- A user can approve, renew, narrow, or remove a policy decision from the
  relevant review detail without losing route context.
- `security policy cleanup` and TUI policy details agree on invalid, expired,
  duplicate, and shadowed rules.
- JSON reports contain enough structured policy evidence for an agent to propose
  a safe edit without parsing prose.

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
