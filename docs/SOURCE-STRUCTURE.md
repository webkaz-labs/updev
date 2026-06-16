# updev source structure

This document tracks package-size budgets, current source counts, and the
refactor ledger for this module's `internal/` packages. Architecture boundaries
live in [ARCHITECTURE.md](ARCHITECTURE.md); this file is the operational
checklist used to keep package folders searchable as updev grows.

## Folder Budgets

Budgets are guardrails, not architecture goals by themselves. A package can stay
larger only when this document records why and the check script has an explicit
temporary ceiling.

| Package | Current Go files | Target | Temporary ceiling | Status | Next action |
|---------|------------------|--------|-------------------|--------|-------------|
| `internal/cmd` production files | 20 | <= 20 | 20 | within target | `cmd` is now limited to command parsing, adapters, rendering, route wiring, and local write boundaries. Do not add new command production files without moving behavior to an owner package in the same change. |
| `internal/cmd` test files | 14 | <= 14 | 14 | within target | Keep command tests focused on option parsing, top-level routing, and integration contracts; owner-specific behavior tests belong with the owner package. |
| all other `internal/*` packages, including nested owner packages | <= 20 each | <= 20 | 20 | within target | Split by domain before adding the 21st Go source file. |

Current largest non-`cmd` packages:

| Package | Current Go files | Status |
|---------|------------------|--------|
| `internal/brew` | 16 | ok |
| `internal/manualinventory` | 14 | ok |
| `internal/mise` | 13 | ok |
| `internal/reviewui` | 12 | ok |
| `internal/registryaudit` | 9 | ok |
| `internal/securitygate` | 9 | ok |
| `internal/securityscanner` | 8 | ok |
| `internal/githubrepo` | 6 | ok |
| `internal/manualinventory/platform` | 6 | ok |
| `internal/nativeaudit` | 6 | ok |
| `internal/securityadvisory` | 6 | ok |
| `internal/backend` | 5 | ok |
| `internal/plan` | 5 | ok |
| `internal/textui` | 4 | ok |

## Placement Rules

- `internal/cmd` owns command parsing, command-local adapters, human/JSON
  rendering, and TTY route/action wiring only.
- Provider-specific evidence, policy, mutation, parsing, and metadata contracts
  belong in provider or security packages such as `brew`, `mise`,
  `githubrepo`, `registryaudit`, `securitygate`, `securityscanner`,
  `nativeaudit`, and `securityadvisory`.
- Reusable TTY behavior belongs in `reviewui`; table/width/color behavior
  belongs in `textui`.
- Do not reduce a folder count by creating nested command subpackages that still
  own business logic. In Go, directories are package boundaries; useful cleanup
  means domain extraction plus thinner command wiring.
- If a package crosses its target, update this file in the same change with a
  concrete split plan or reduce the count before merging.
- Large colocated contract test files are acceptable only as a stopgap to avoid
  sidecar test sprawl. Do not add unrelated scenarios to them; when a contract
  grows further, extract the production owner package first and move the tests
  with that owner.

## Agent-Quality Audit Ledger

`mise run agent-quality` runs pinned `aislop@0.12.0` as non-blocking audit
evidence. The current baseline is intentionally tracked here instead of making
all `aislop` findings release-blocking immediately.

Current `aislop` baseline:

| Finding class | Current count | Decision | Next action |
|---------------|---------------|----------|-------------|
| ignored return values | 0 errors | blocking candidate for new regressions | The initial 8 findings were resolved by validating security actions, surfacing TUI errors, degrading OSV detail fetch failures explicitly, and checking config scanner errors. Promote this class to blocking only after a few releases confirm low false positives for new findings. |
| possible hardcoded secret | 1 known false positive | documented allow | `internal/securityreason/reason.go` defines the stable reason code `scanner_secret`; it is not a secret. `check-agent-quality.sh` filters this known false positive from active errors while still reporting the known-false-positive count. Revisit if `aislop` adds precise line/rule suppressions. |
| hardcoded URL | 5 warnings | documented allow, not blocking | Package-page URLs now live behind provider helper functions. Remaining hits are env-overrideable default API endpoints or stable public detail URL bases. Do not block this class until the rule can distinguish configurable endpoints from durable public reference URLs. |
| too many parameters | 12 warnings | refactor backlog | Prefer options structs when touching route/browser constructors or provider helper APIs. Do not churn stable call sites just to satisfy the metric. |
| function too long | 16 warnings | refactor backlog | Continue extracting report assembly, localization, and route construction into owner packages during planned scalability refactors. |
| file too large | 13 warnings | refactor backlog | Keep the existing package/file budgets as the primary guardrail; use the `aislop` file-size output as a prioritization signal for future splits. |

Blocking promotion policy:

- `agent-quality` remains non-blocking by default. `UPDEV_AGENT_QUALITY_STRICT=1`
  is only for experiments or focused cleanup branches.
- A class can become release-blocking only after the baseline is cleared or
  documented, the rule has low false positives for at least several updev
  releases, and the remediation is mechanical enough for agents to apply
  consistently.
- Do not promote broad complexity findings directly. Use them to choose
  refactor slices, then enforce durable package/source-count budgets through
  `check-source-structure.sh`.

## Refactor Ledger

Completed P1 foundation slices:

- Placement rules are frozen in docs and enforced by release reviews.
- Advisory/feed query and finding merge/enrichment logic lives in
  `internal/securityadvisory`.
- Homebrew and mise provider mutations use runner-backed command plans with
  argv/env and item-scoped semantics.
- Inventory report annotations that depend on provider evidence and root/profile
  policy live in `internal/inventoryannotate`.
- Shared inventory/provider status and item-query helpers live in
  `internal/plan`; cmd code no longer reimplements provider status, attention
  status ordering, or basic item search semantics.
- Installed-list evidence indexes, item evidence projection, evidence-key
  aliases, and evidence counts live in `internal/plan`; cmd code keeps only the
  cached-report adapters, localized text, and TUI route/action wiring.
- Update-step reasons for strict-safety and mise-bump flows expose stable
  `reason_code` plus `reason_args` through `internal/updatereason`.
- Security gate findings expose stable `reason_code` plus `reason_args` for the
  core strict-safety paths through `internal/securityreason`.
- Scanner vulnerability findings from OSV-Scanner, Trivy, and Grype expose
  structured reason codes and args.
- Scanner non-vulnerability findings from gitleaks, zizmor, and Trivy expose
  structured reason codes and args.
- GitHub repository posture evidence exposes structured reason codes and args.
- Native audit vulnerability evidence exposes structured reason codes and args.
- Native audit target discovery for cargo binaries and mise pipx site-packages
  lives in `internal/nativeaudit`; command code passes package evidence in and
  does not own environment-specific audit path construction.
- Native audit JSON report conversion and provider-native command error
  classification live in `internal/nativeaudit`; command code runs tools and
  delegates report semantics to the native audit owner package.
- NPM, Cargo, and PyPI registry posture evidence exposes structured reason
  codes and args.
- Homebrew and VS Code provider posture/update-safety reasons expose structured
  reason codes and args for allow/review/hold paths; command renderers localize
  from those codes before falling back to legacy prose.
- VS Code installed-extension command, parsing, and error normalization live in
  `internal/vscode`; command code only turns unavailable provider evidence into
  gate warnings.
- NPM, Cargo, and PyPI registry logic lives in `internal/registryaudit`; command
  code imports that owner package directly and keeps only endpoint resolution
  and display-count helpers local to the CLI/report surface.
- Registry posture review-count helpers live in `internal/registryaudit`; cmd
  renderers ask the provider owner for counts instead of reimplementing
  decision semantics.
- mise minimum_release_age diagnostics live in `internal/mise`, so command code
  consumes provider evidence instead of owning mise settings JSON parsing.
- mise registry provider metadata release-age helpers are colocated with the
  mise safety adapter instead of living in a sidecar command file.
- The macOS manual app scanner and MAS section adapters are colocated with the
  manual app section owner instead of living in sidecar command files.
- Manual inventory `scan` and `render` command adapters are colocated because
  they share the same provider, parser, and markdown-preview boundary.
- Read-only/list report tests are colocated with list report tests, so cached
  read behavior and list rendering expectations live with the list report
  contract.
- VS Code posture and update-safety tests are colocated, so Marketplace
  posture, update gating, and advisory evidence share one provider test
  contract.
- Last-update cache/report tests are colocated with update tests, so update
  execution, outcome rendering, and cached `last` views share one report
  contract.
- List evidence/badge tests are colocated with list report tests, so list
  filters, read-only output, evidence counts, and row badges share one display
  contract.
- Manual inventory plan tests are colocated with manual inventory review tests,
  so review candidates, grouped plan actions, and override application share
  one manual inventory command contract.
- Scanner runner tests are colocated with scanner security tests, so scanner
  parser, runner, evidence ordering, and security text expectations share one
  scanner contract.
- Config and interactive-entrypoint tests are colocated with CLI option tests,
  so root/config precedence, usage aliases, and TTY entrypoint decisions share
  one command parsing contract.
- Browser model and detail-row tests are colocated with table browser tests, so
  row expansion, mouse behavior, navigation actions, and focused detail actions
  share one TUI browser contract.
- Sync tests are colocated with mutation tests, so drift classification,
  write-mode gating, snapshots, rollback, and sync guidance share one command
  mutation contract.
- Security policy report tests are colocated with security policy tests, and
  update hub/strict-safety tests are colocated with update tests, so policy
  views and update safety routing each have one test contract.
- Provider/mise safety and posture advisory tests are colocated with security
  safety tests, so release-age gates, provider safety enrichment, and advisory
  package mapping share one safety contract.
- Manual inventory agent/enrichment and annotation tests are colocated with the
  manual inventory report tests, so manual-source behavior has one test owner.
- List routing and backend convergence tests are colocated with the list hub
  router tests, so hub/table/action routing stays in one contract file.
- Update hub router tests are colocated with update report/log tests, so update
  dashboard navigation stays next to the update evidence views it opens.
- Legacy inventory cache loading, translation-cache persistence, and cached
  package/tool row parsing live in `internal/legacycache`, so `cmd` consumes
  enriched items and display sections instead of owning cache file formats.
- Shared detail-row data, detail-to-table conversion, detail browser model,
  confirm prompts, text-input prompts, startup progress rendering, and progress
  message text live in `internal/reviewui`.
- Shared text display helpers such as compact age formatting and ordered filter
  summaries live in `internal/textui`; command code should not keep duplicate
  display utilities.
- Action badge rendering, stable badge priority, status-aware badge coloring,
  width truncation, and Nerd Font marker detection live in `internal/textui`;
  `reviewui` adapts row actions into badge inputs but does not own badge
  presentation rules.
- Shared action-summary browser behavior for selectable dashboard/summary rows
  lives in `internal/reviewui`; cmd code owns only update-specific line
  construction and route encoding.
- Shared review action merging lives in `internal/reviewui`; cmd surfaces attach
  route-specific actions and delegate de-duplication by action value.
- Update provider stdout/stderr summary parsing and de-duplication lives in
  `internal/updatelog`; cmd code passes captured provider logs in and keeps
  report mutation/rendering local.
- Shared write-flow state-key, default-expiry, and confirmation-description
  helpers live in `internal/reviewui`; cmd routers keep only action parsing,
  return routing, and write application.
- Shared browser action consumption and state-cache persistence lives in
  `internal/reviewui`, so update/list routers do not duplicate stale-action
  clearing around every child browser update.
- Shared write-flow pending state and reason/expiry acceptance lives in
  `internal/reviewui`; cmd routers keep route-specific return behavior and the
  final local write operation.
- Homebrew tap trust target parsing, trust-state interpretation, and trust
  command argv builders live in `internal/brew`.
- Homebrew and mise update command argv builders live in their provider
  packages. Command code owns findings-to-target selection, report mutation,
  and TTY routing; it should not rebuild provider command chains inline.
- Curated backend preference rewrite seeds are data-backed registry entries
  with source evidence, so adding a known rewrite path does not require a new
  command-local or tool-name branch.
- Manual inventory scanner orchestration, live cask/MAS provider probes, stable
  app identity keys, and scan dedupe/sort behavior live in
  `internal/manualinventory`; cmd code only turns scanned app records into
  report rows and actions.
- Structured manual inventory source parsing and source-kind detection live in
  `internal/manualinventory`; cmd code uses parsed manual app records for row
  rendering, draft validation, and accepted override flows.
- Structured manual inventory draft block parsing, rendering, selection, and
  append/replace file operations live in `internal/manualinventory`; cmd code
  keeps only UI/editor wiring and manual review candidate matching.
- Manual inventory agent request construction and direct agent subprocess
  execution live in `internal/manualinventory`; cmd code keeps opt-in config
  checks, candidate selection, and structured draft validation/write flow.
- Manual inventory review candidate/evidence/override models and agent draft
  validation live in `internal/manualinventory`; cmd code keeps report-specific
  row classification and CLI/TUI action wiring.
- Manual inventory override preview/block rendering and append file writes live
  in `internal/manualinventory`; cmd code keeps editor invocation and
  route-specific action selection.
- Manual inventory row detail parsing, evidence extraction, ownership
  classification, plan action classification, command previews, and suggested
  override construction live in `internal/manualinventory`; cmd code only
  adapts command-local `toolRow` values and renders CLI/TUI surfaces.
- Local provider command/JSON contract drift checks live behind
  `updev check --dependencies` and `updev doctor dependencies`; required
  Homebrew/mise contract drift is reported as `drift`, while optional scanner
  integrations can be unavailable without failing the report.
- `updev doctor dependencies` includes a provider compatibility ledger in JSON
  output and can write the ledger with `--ledger <file>`, so local/CI drift
  evidence has a portable artifact without posting public issues by default.
- Linux `.desktop` / Flatpak / Snap / AppImage scanner evidence and Windows
  `winget export` fixture evidence live in `internal/manualinventory`, keeping
  portable manual inventory source parsing and live scanner evidence under the
  same owner.
- `updev skill`, `updev skill --full`, and `updev help agent` embed canonical
  root `docs/agent/` files through `main.go`; `cmd` only renders the injected
  docs and keeps fallback text for tests.

Current v0.7.x guardrails:

- Keep contract drift checks wired into local/CI validation while provider
  packages own their command contracts.
- Continue shrinking command-local adapters only when the target package can
  own the full domain behavior without importing TUI/report types.
- Treat TTY/report regression behavior as part of the contract: route state,
  focused actions, grouped rows, and item-visible safety/update evidence should
  be tested or manually accepted before another UX refactor lands.

v0.7.5 extraction baseline:

- `internal/cmd` is at the v0.7.5 ceiling of 45 Go files.
- `internal/manualinventory` core is below its target at 14 Go files.
- Platform manual app scanners moved to `internal/manualinventory/platform`.
- Inventory collection/cache/sort behavior moved to `internal/inventoryrun`.
- Support report construction and text rendering moved to `internal/support`.
- Homebrew advisory package mapping and posture review counting moved to
  `internal/brew`.
- Shared route-state cache helpers live in `internal/reviewui`, and list/update
  routers use them instead of open-coded nil-map and lookup handling.
- The source-structure check now counts nested owner packages and enforces the
  `internal/cmd` v0.7.5 ceiling.

v0.7.5 scalability completion result:

`v0.7.5` finishes the P1 structural scalability refactor. Future provider
evidence, scanner, and policy work should no longer add provider parsing,
scanner parsing, route-state, or badge logic to `internal/cmd`.

| Area | Current pressure | Preferred destination | Done when |
|------|------------------|-----------------------|-----------|
| Update/security report assembly | `cmd` still owns much of the stitching between provider findings, update steps, and TTY routes. | Keep final report mutation in `cmd`, but move provider-neutral decision grouping or reason derivation into `securitygate`, `updatereason`, or `securityreason` when it can be tested without TUI imports. | JSON/report fields stay stable, localized text still renders at the boundary, and update dashboard tests keep passing. |
| List/detail evidence badges | Inventory, manual, backend, and security views must keep consistent compact markers. | `textui` for badge rendering and width behavior; `plan` or provider packages for evidence classification. | A new badge or evidence class is not implemented separately in multiple command files. |
| Routed TTY return behavior | `updev`, `last`, and `list` share route-stack expectations but still have command-specific adapters. | `reviewui` for state stack, action consumption, confirmation state, and focus/scroll restoration; `cmd` only maps routes to domain views. | Returning from child views restores the expected parent row/filter without clearing focused actions. |
| TUI foreground external process | TUI actions may later need to open an editor or explicit agent command and return to the same route. | Future shared `reviewui` wrapper around Bubble Tea `ExecProcess`; provider update/scan/audit execution stays on `runner`. | The release records the boundary, but does not replace provider log-streaming commands with `ExecProcess`. |
| Portable manual inventory sources | `manualinventory` is at its temporary ceiling after adding Linux/Windows fixture-backed scanners. | Split platform scanners into subpackages or a scanner registry package before adding distro package, registry, Start Menu, scoop, or choco evidence. | No repository-local app prose or machine-local path assumption is required by default, and package count stays within budget. |
| External CLI and parser ownership | Provider/scanner command outputs are already mostly owned by provider packages, but refactors can accidentally add ad hoc parsing to `cmd`. | `runner` for subprocess execution; provider/scanner packages for argv/env construction, structured output parsing, and provider-specific error normalization; `textui` for terminal width/color. | Direct subprocess exceptions stay documented and checked, and every touched provider/scanner command has parser tests in the owning package. |

Targets for the completed P1 refactor:

- `internal/cmd` is at the v0.7.5 target, `<= 45` Go files.
- `internal/manualinventory` core is below the target, `<= 15` Go files after
  platform/source split.
- No new provider/scanner/policy/parser business logic is added to
  `internal/cmd`.
- New provider metadata resolvers, including future vfox/asdf-style work, use
  provider-owned data registries and bounded resolver contracts rather than
  command-local or tool-name branches.

Implementation order:

1. Split `manualinventory`.
2. Extract shared routed TTY state into `reviewui`.
3. Extract provider-neutral update/security report assembly.
4. Clean up badge/evidence display boundaries.
5. Audit external CLI/parser ownership and update this ledger.

v0.7.7 architecture-hardening result:

`v0.7.7` starts the larger reset before the next feature slice by enforcing the
real command-sprawl budget: production command files. This prevents provider
evidence quality, scanner hardening, and policy ergonomics from growing command
production code again, while keeping command-level regression tests visible as a
separate budget. It is a guardrail release, not the full large refactor.

| Area | Target state | Required guardrail |
|------|--------------|--------------------|
| `internal/cmd` size | Keep production command files at `<= 30`; current production count is 27. | `scripts/check-source-structure.sh` enforces the production ceiling and separately checks command test files. |
| Report assembly | Update/security/list/manual/backend summaries are assembled by domain packages. | Cached reports, JSON/text output, and TUI detail rows consume the same structured assembly result. |
| TUI routing | Generic route stack, focus restoration, filter state, confirmation state, pending writes, and Back/Home behavior live outside command-specific files. | Fast tests or PTY smoke cover `updev`, `last`, `list`, manual inventory, backend, security, and policy routes. |
| Provider/parser ownership | Provider command construction, timeout/error normalization, and structured output parsing stay in provider/scanner owner packages. | Direct subprocess exceptions remain synced between `ARCHITECTURE.md` and `scripts/check-direct-subprocesses.sh`. |
| Provider logs | Homebrew/mise update, scan, and audit logs keep streaming outside alternate-screen TUI. | Do not replace provider execution with Bubble Tea `ExecProcess`; reserve `ExecProcess` for foreground TUI actions only. |

Do not start provider evidence expansion, scanner defaults, or policy UX feature
growth if it would increase production command file count above the enforced
budget. Owner-specific tests should move with the owner package instead of
growing broad command tests.

Do not add new tool-name-only fixes, direct provider command calls, implicit
repository-local defaults, or TUI-only behavior that is missing from the report
model.

v0.7.8 large-refactor result:

`v0.7.8` performs the code movement that `v0.7.7` only prepared for. The
release leaves `internal/cmd` at the stricter production/test budgets and keeps
the remaining command code focused on command parsing, option adaptation, final
localization labels, top-level route wiring, rendering, and write boundaries.

Final budgets:

| Package | Current Go files | v0.7.8 target | Guardrail |
|---------|------------------|---------------|-----------|
| `internal/cmd` production files | 20 | <= 20 | `scripts/check-source-structure.sh` now fails above 20 production command files. |
| `internal/cmd` test files | 14 | <= 14 | `scripts/check-source-structure.sh` now fails above 14 command test files. |
| extracted report/view/route owners | <= 20 each | <= 20 each | Owner packages must stay independently searchable and testable. |

Completed extraction:

- `internal/updevconfig` owns updev config defaults, environment overrides,
  TOML parsing, validation helpers, and config path resolution. `cmd` keeps
  compatibility aliases and wrappers only.
- `internal/syncreport` owns read-only sync report construction, drift
  classification, provider mismatch keys, and sync guidance. `cmd` supplies the
  manual-app matching adapter and final localized text/JSON rendering.
- Update and list routed TUI adapters are consolidated into their parent
  command files, removing sidecar route files while keeping shared state and
  action consumption in `reviewui`.
- mise bump and mise release-age safety adapters are consolidated under the
  safety command adapter. Provider evidence and release-age semantics remain in
  `internal/mise`, `registryaudit`, `githubrepo`, and `securitygate`.
- Command tests are consolidated by behavior contract: list/report/router,
  manual inventory, safety/mise bump, and TUI table behavior now live in fewer
  command test files without deleting coverage.

Future extraction order:

1. **Report assembly owners**
   - Extract provider-neutral update/security summary assembly before changing
     downstream TUI routes.
   - Cached reports, JSON/text summaries, and TUI route rows must consume the
     same structured summary result.
   - Reason derivation and evidence grouping must not stay command-local after
     the extraction.

2. **List/manual/backend view models**
   - Move row grouping, evidence-to-action projection, compact badge presence,
     and backend/manual action summaries out of command files.
   - Keep final Japanese/localized labels at the CLI/TUI boundary.
   - Owner packages expose stable row/action inputs that can be tested without
     terminal interaction.

3. **Common routed TUI core**
   - Consolidate shared route stack, Back/Home restoration, parent row/filter
     restoration, focused-action stability, confirmation state, pending writes,
     and action consumption in `reviewui` or a small route owner.
   - The extracted route core must cover `updev`, `updev last`, `updev list`,
     manual inventory, backend convergence, security details, and policy review.
   - Preserve the accepted high-density grouped list design; do not turn this
     refactor into a UX redesign.

4. **Test relocation**
   - Move owner-specific tests with the extracted production owner packages.
   - Keep only command parsing, CLI option precedence, top-level route dispatch,
     and integration-style contracts in `internal/cmd`.
   - Do not reduce command test count by deleting coverage; reduce it by moving
     tests to the package that owns the behavior.

5. **Guardrail tightening**
   - Tighten `scripts/check-source-structure.sh` only after the production and
     test movement lands.
   - Record final counts here before tagging.
   - Keep direct subprocess exceptions synced with `ARCHITECTURE.md` and the
     direct-subprocess check.

Non-goals for `v0.7.8`:

- provider evidence expansion;
- scanner default expansion;
- policy UX feature growth;
- support-label promotion;
- replacing provider log streaming with Bubble Tea foreground process
  execution;
- moving code into nested command subpackages that still own business logic.
