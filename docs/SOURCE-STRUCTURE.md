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
| `internal/cmd` | 50 | <= 50 | 50 | within target | At ceiling; do not add another cmd file before extracting provider, security, review UI, support catalog, or inventory logic. |
| all other `internal/*` packages | <= 20 each | <= 20 | 20 | within target | Split by domain before adding the 21st Go source file. |

Current largest non-`cmd` packages:

| Package | Current Go files | Status |
|---------|------------------|--------|
| `internal/manualinventory` | 20 | at ceiling |
| `internal/brew` | 14 | ok |
| `internal/mise` | 13 | ok |
| `internal/reviewui` | 12 | ok |
| `internal/registryaudit` | 9 | ok |
| `internal/securitygate` | 9 | ok |
| `internal/securityscanner` | 8 | ok |
| `internal/githubrepo` | 6 | ok |
| `internal/nativeaudit` | 6 | ok |
| `internal/securityadvisory` | 6 | ok |
| `internal/backend` | 5 | ok |
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

Current v0.7.0 guardrails:

- Keep contract drift checks wired into local/CI validation while provider
  packages own their command contracts.
- Continue shrinking command-local adapters only when the target package can
  own the full domain behavior without importing TUI/report types.
- Treat TTY/report regression behavior as part of the contract: route state,
  focused actions, grouped rows, and item-visible safety/update evidence should
  be tested or manually accepted before another UX refactor lands.

Next extraction candidates after v0.7.0:

| Area | Current pressure | Preferred destination | Done when |
|------|------------------|-----------------------|-----------|
| Update/security report assembly | `cmd` still owns much of the stitching between provider findings, update steps, and TTY routes. | Keep final report mutation in `cmd`, but move provider-neutral decision grouping or reason derivation into `securitygate`, `updatereason`, or `securityreason` when it can be tested without TUI imports. | JSON/report fields stay stable, localized text still renders at the boundary, and update dashboard tests keep passing. |
| List/detail evidence badges | Inventory, manual, backend, and security views must keep consistent compact markers. | `textui` for badge rendering and width behavior; `plan` or provider packages for evidence classification. | A new badge or evidence class is not implemented separately in multiple command files. |
| Routed TTY return behavior | `updev`, `last`, and `list` share route-stack expectations but still have command-specific adapters. | `reviewui` for state stack, action consumption, confirmation state, and focus/scroll restoration; `cmd` only maps routes to domain views. | Returning from child views restores the expected parent row/filter without clearing focused actions. |
| Portable manual inventory sources | `manualinventory` is at its temporary ceiling after adding Linux/Windows fixture-backed scanners. | Split platform scanners into subpackages or a scanner registry package before adding distro package, registry, Start Menu, scoop, or choco evidence. | No repository-local app prose or machine-local path assumption is required by default, and package count stays within budget. |
| Provider metadata resolvers | vfox/asdf-style backend evidence must scale beyond one tool. | Provider-owned data registries and bounded resolver contracts, especially under `mise`, `githubrepo`, and `registryaudit`. | Adding a known resolver path is data-backed and tested, not a tool-name branch in `cmd`. |

Do not add new tool-name-only fixes, direct provider command calls, implicit
repository-local defaults, or TUI-only behavior that is missing from the report
model.
