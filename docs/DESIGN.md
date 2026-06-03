# updev design

`updev` is the package and developer-tool update and inventory layer. It should
be portable outside this dotfiles repository, but it is intentionally smaller
than a full OS package manager: provider tools still install and update
packages, while `updev` orchestrates daily updates, desired-state validation,
drift review, security policy, and reproducible manifests.

## Document Map

| Document | Role |
|----------|------|
| [README.md](../README.md) | Human-facing overview, common commands, configuration, and development entrypoint. |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Ownership boundaries, provider model, package layout, shared-internal extraction rules. |
| [CLI.md](CLI.md) | Command surface, text/JSON/TUI contract, localization, list/update UX. |
| [DATA-MODEL.md](DATA-MODEL.md) | Desired-state sources, inventory/cache/report model, config locations, status vocabulary. |
| [SECURITY.md](SECURITY.md) | Security scan/gate/review/policy behavior, evidence model, remaining security backlog. |
| [ROADMAP.md](ROADMAP.md) | Current state and later release ordering. |
| [RELEASE.md](RELEASE.md) | Active release scope, non-goals, blockers, and release-ready criteria. |
| [VALIDATION.md](VALIDATION.md) | Smoke and regression checklist. |
| [EXTERNAL-MANAGEMENT.md](EXTERNAL-MANAGEMENT.md) | External/manual package and installer direction. |

## Mission

`updev` owns package, runtime, command-line tool, package-like app, and
provider inventory state. Its primary daily workflow is:

1. run one command to update development tools and packages;
2. apply provider-specific safety gates;
3. show compact update, drift, skipped, and warning summaries;
4. keep desired package/tool state reproducible through chezmoi-managed
   manifests.

The command must stay useful for humans who want a simple daily update and a
guided review surface, and for agents that need stable JSON and explicit action
boundaries.

## Go v1 Completion Goal

The first complete Go `updev` milestone is the point where the normal
package/tool workflow no longer depends on the legacy Python command.

The public v1 contract should stay narrower than the full internal command
surface. The product goal is a general-purpose package/tool orchestration CLI,
but the first stable scope is the daily package/tool workflow around Homebrew,
Brewfile-derived desired state, mise, focused security gates, and reproducible
inventory/check reports. Experimental provider expansion, manual/vendor app
scans, backend migration advice, and agent-assisted review may exist before v1,
but they do not become part of the public stable contract until their data model
and failure modes are proven.

The v1 core surface is:

- bare `updev` and `updev update` run the daily update workflow in Go, apply
  provider update/security policy, support dry-run and JSON output, and finish
  with grouped inventory.
- `updev list`, `updev inventory`, `updev status`, `updev check`, and
  `updev plan` provide one stable read-only view of desired and live state for
  the current Homebrew/Brewfile and mise providers.
- `updev security scan`, `updev security gate`, `updev security review`, and
  `updev security policy` meet the security v1 contract in
  [SECURITY.md](SECURITY.md).
- `updev sync` runs in Go for the current providers and explains
  desired-but-missing, installed-but-unmanaged, provider mismatch, profile
  mismatch, and intentionally skipped entries. It is read-only by default;
  mutation requires an explicit apply path or follow-up command.
- top-level `updev add` and `updev remove` guide writes to the current desired
  manifests, choose or explain the provider/backend, validate after writing,
  and show the resulting diff/state. Lower-level `updev brewfile ...` commands
  remain compatibility/internal surfaces.
- `updev edit` snapshots first, opens the relevant manifest, validates after
  exit, and leaves enough state for `updev rollback` to restore the latest
  package/tool manifest snapshot.

The v1 stretch surface is:

- `updev backends doctor` and `updev backends plan` report Homebrew-to-mise and
  mise backend convergence opportunities without performing automated
  migrations. It is read-only and uses conservative curated recommendations.
- read-only manual/vendor app inventory can remain opt-in or experimental if it
  is useful but not mature enough for the public stable contract.

Done for v1 means:

- documented smoke commands for update, list, sync, add/remove,
  edit/rollback, security, and backend convergence run on this repository with
  expected exit-code semantics;
- text output is compact enough for daily use, and JSON output is stable enough
  for agents;
- every manifest mutation has a previewable diff, validation result, and
  snapshot/rollback path;
- legacy Python remains only for comparison or explicitly named legacy
  commands, not for the default daily workflow.

Stable JSON reports include `schema_version`. Report names include
`syncReport`, `mutationReport`, `rollbackReport`, and `backendPlanReport`.
Human text may evolve for usability; JSON field names and status meanings
should remain stable once introduced.

## Public Release Readiness

`updev v1.0.0` is reserved for the first public-ready release. Reaching it
requires more than the Go migration being functionally useful in this repository:

1. **Scope freeze**: document which commands, config keys, cache/report files,
   and JSON report fields are stable, and mark the rest as experimental,
   provider-specific, or repository-local integration.
2. **Install path**: provide a reproducible install/update path such as a
   release binary, Homebrew formula, or `go install` command, plus uninstall
   guidance.
3. **Repository model**: choose an in-repo, split/mirror, or standalone
   repository model using the cross-tool
   [repository publication model](../../../docs/tooling-roadmap.md#repository-publication-model).
   If a standalone repo is created, keep this dotfiles repository as an
   integration smoke consumer so real Homebrew/mise dogfood remains cheap.
4. **Repo assumptions**: make chezmoi integration, macOS, Homebrew, mise, cache
   paths, and desired-state root assumptions explicit; avoid hidden hardcoded
   paths in the portable mode.
5. **Docs for first use**: provide a public README path from installation to
   dry-run, daily update, list/check, configuration, and rollback/recovery.
6. **Privacy and security**: document what metadata is scanned, where reports
   are written, how secrets are redacted, and which network calls or external
   scanners are optional.
7. **Compatibility tests**: keep focused tests for stable JSON shape, config
   parsing, no-color/non-TTY output, exit codes, and provider command parsing.
8. **Platform promise**: state the supported platform set. A narrow macOS-first
   promise is acceptable; broad Linux/Windows package management should stay
   out of scope until explicitly implemented.
9. **Legacy boundary**: remove or clearly label legacy Python and compatibility
   wrappers so public users do not depend on private migration surfaces.

## Product Boundaries

| Layer | Owner | Rule |
|-------|-------|------|
| Dotfiles and file-backed app config | chezmoi | Keep normal files as source of truth. Do not move dotfiles into `updev`. |
| Packages, runtimes, global CLIs, package-like apps | `updev` | Daily update, inventory, desired manifests, provider policy, security gates, backend convergence. |
| macOS OS settings | `macos-settings` / `macset` | macOS defaults, network preferences, Wi-Fi metadata, previews, rollback, gated apply. |
| Organization device management | Apple Business built-in MDM / external MDM | Device enrollment, managed accounts, enforced profiles, app assignment, certificates, OS update/security policy, fleet compliance. |
| Non-dotfile machine state | adjacent docs/tools | Secrets, auth, services, backups, hardware, project bootstrap, app runtime state. Track explicitly, but do not fold into `updev` without a narrow provider. |

## Operating Principles

- Bare `updev` remains the one-command daily update workflow.
- Mutation is allowed, but it must be policy-checked, reported, and reversible
  where manifests are changed.
- Manifest rewrites, provider migration, trial promotion, and direct-edit saves
  need focused diffs and snapshots.
- Provider-native tools own install/update mechanics; `updev` owns orchestration
  and explanation.
- Prefer a documented provider/backend preference policy over ad hoc
  recommendation tables. For reliable CLI developer tools, prefer `mise` core
  backends when available, then well-supported mise external backends, and keep
  native package managers for bootstrap tools, GUI apps, platform integration,
  stronger provider evidence, or packages intentionally owned by that provider.
- Trial/local-only state must not leak into `work` / `personal` deployment.
- Every important human flow needs a stable non-TTY/JSON path.
- Do not absorb all machine state: MDM, app runtime state, secrets, backups,
  hardware, privacy permissions, and project bootstrap stay adjacent.

## Current State

Go `updev` owns the default daily workflow, rich list/inventory, read-only
sync, guided add/remove/edit, rollback, security v1, and read-only backend
convergence reports. `updev brewfile ...`, `brewfile`, and legacy Python remain
compatibility or comparison surfaces only.

Long-term ordering lives in [ROADMAP.md](ROADMAP.md). The current and next
release targets live in [RELEASE.md](RELEASE.md).
