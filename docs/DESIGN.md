# updev design

`updev` is the package and developer-tool update and inventory layer. It should
stay smaller than a full OS package manager: provider tools still install and
update packages, while `updev` orchestrates routine updates, desired-state
validation, drift review, security policy, and reproducible manifests.

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
| [EXTERNAL-MANAGEMENT.md](EXTERNAL-MANAGEMENT.md) | External/manual package and installer direction. |
| `docs/agent/` | Source of truth for agent-facing usage, skill text, and CLI workflow recipes. |

## Documentation Source Of Truth

Keep public docs useful without making users, maintainers, and agents update the
same fact in five places. Each durable fact should have one owner; other docs
may summarize it, link to it, or embed it from the owner, but should not carry a
second long-form copy.

| Information | Owner | Mirrors and limits |
|-------------|-------|--------------------|
| Product boundary, stable scope, and source-of-truth policy | `DESIGN.md` | README may summarize the value proposition; release docs may point back here. |
| Command surface, flags, exit codes, text/JSON/TUI behavior | live `updev help` and `CLI.md` | README and agent docs may show short recipes only. Avoid duplicated flag tables. |
| Data model, config keys, cache/report paths, status vocabulary | `DATA-MODEL.md` | README may show minimal config examples; command docs should link back for details. |
| Security gates, policy decisions, scanners, trust boundaries | `SECURITY.md` | README and agent docs may describe safe defaults and link here for detail. |
| Current release target and release criteria | `RELEASE.md` | ROADMAP keeps ordering only; release notes describe one shipped tag only. |
| Tag-specific release notes | `docs/release-notes/<tag>.md` | GitHub Release bodies are generated from these files by the release workflow. |
| Agent usage, skills, and agent-assisted updev development | `docs/agent/` | `updev skill`, `updev help agent`, and README discovery text should embed or link to this tree. |

Apply these rules when changing docs or CLI behavior:

- Do not add a new long-form explanation until the owning document is clear.
- README is the first-use path and product pitch, not the command reference,
  release ledger, security spec, or validation log.
- ROADMAP orders future work; RELEASE defines the active/next release; git log
  remains the implementation history.
- Version-specific text belongs in release notes or the current-release section,
  not scattered through install snippets unless the command genuinely requires
  an exact tag.
- Generated or embedded surfaces should be derived from canonical files. Future
  `updev skill`, GitHub Release notes, and similar outputs should avoid separate
  Go string copies.
- When a change affects a fact with multiple mirrors, the release checklist
  should name every mirror that must be reviewed, even when no edit is needed.

Current drift checks verify release-note presence, agent docs, README/local
Markdown links, CI/mise validation parity, and direct subprocess exceptions.
Broader checks can later cover command-help snapshots, JSON schema examples,
and provider compatibility ledgers.

## Design Scope

README.md is the user-facing overview, install path, and quick start. This
document defines the product boundaries and stability criteria that keep the CLI
small enough to be predictable.

`updev` owns package, runtime, command-line tool, package-like app, and provider
inventory state. Provider-native tools still own installation mechanics;
`updev` owns orchestration, policy checks, explanations, and reproducible
desired-state review.

## v1 Completion Goal

The public v1 contract should stay narrower than the full command surface. The
first stable scope is the main package/tool workflow around Homebrew,
Brewfile-derived desired state, mise, focused security gates, and reproducible
inventory/check reports. Experimental provider expansion, manual/vendor app
scans, backend migration advice, and agent-assisted review may exist before v1,
but they do not become part of the stable contract until their data model and
failure modes are proven.

The v1 core surface is:

- bare `updev` and `updev update` run the default update workflow in Go, apply
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
  remain compatibility surfaces.
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
  edit/rollback, security, and backend convergence run with expected exit-code
  semantics;
- text output is compact enough for routine use, and JSON output is stable enough
  for agents;
- every manifest mutation has a previewable diff, validation result, and
  snapshot/rollback path;
- compatibility commands remain clearly labeled and do not replace the default
  update workflow.

Stable JSON reports include `schema_version`. Report names include
`syncReport`, `mutationReport`, `rollbackReport`, and `backendPlanReport`.
Human text may evolve for usability; JSON field names and status meanings
should remain stable once introduced.

## Agent Guidance Design

Agent-facing guidance is useful both for users who ask AI coding agents to run
`updev` and for agents developing `updev` itself. It must not become another
place where command semantics drift. Keep one canonical agent guidance tree and
make every other surface reference or embed it:

- `docs/agent/USAGE.md` is the canonical guide for agent workflows:
  safe read-only entrypoints, mutation boundaries, exit-code handling, JSON
  usage, security-gate behavior, and `updev list` as the primary review
  surface.
- `docs/agent/SKILL.md` is the installable skill artifact. It stays
  short and procedural, and points to `updev help`, `updev help agent`, and
  `docs/agent/USAGE.md` instead of duplicating full command reference.
- Future `updev skill` should print the embedded `docs/agent/SKILL.md`;
  `updev skill --full` should include the deeper usage guide. The CLI must
  embed these files directly rather than carrying a second copy in Go strings.
- README should only advertise that agent guidance exists and show the minimal
  discovery commands. It should not duplicate the workflow recipes.
- CLI flag and command details remain owned by live command help and
  [CLI.md](CLI.md). Agent docs may name recommended commands, but should avoid
  copying long flag tables.

When command behavior, JSON shape, exit-code semantics, security-gate decisions,
or mutation boundaries change, the release checklist must explicitly include
the agent guidance tree. If a future generated check is added, it should verify
that embedded skill output and repository files match so release artifacts do
not drift from docs.

## Stable Release Readiness

`updev v1.0.0` is reserved for the first stable public contract. Reaching it
requires a stable support promise, not only feature completeness:

1. **Scope freeze**: document which commands, config keys, cache/report files,
   and JSON report fields are stable, and mark the rest as experimental,
   provider-specific, or integration-local.
2. **Install path**: keep reproducible install/update paths such as the shell
   installer, release binaries, mise GitHub backend, or `go install`, plus
   uninstall guidance.
3. **Repository model**: keep development, release tags, and issue tracking in
   the public repository.
4. **Repo assumptions**: make macOS, Homebrew, mise, cache paths, and
   desired-state root assumptions explicit; avoid hidden hardcoded paths.
5. **Docs for first use**: provide a public README path from installation to
   dry-run, update, list/check, configuration, and rollback/recovery.
6. **Privacy and security**: document what metadata is scanned, where reports
   are written, how secrets are redacted, and which network calls or external
   scanners are optional.
7. **Compatibility tests**: keep focused tests for stable JSON shape, config
   parsing, no-color/non-TTY output, exit codes, and provider command parsing.
8. **Platform promise**: state the supported platform set. A narrow macOS-first
   promise is acceptable; broad Linux/Windows package management should stay
   out of scope until explicitly implemented.
9. **Compatibility boundary**: remove or clearly label compatibility wrappers
   so public users do not depend on implementation history.

## Product Boundaries

| Layer | Owner | Rule |
|-------|-------|------|
| Shell/editor/app config files | adjacent config manager | Keep normal configuration files as source of truth. Do not move them into `updev`. |
| Packages, runtimes, global CLIs, package-like apps | `updev` | Update workflow, inventory, desired manifests, provider policy, security gates, backend convergence. |
| OS settings | adjacent OS settings tools | Defaults, network preferences, Wi-Fi metadata, previews, rollback, gated apply. |
| Organization device management | Apple Business built-in MDM / external MDM | Device enrollment, managed accounts, enforced profiles, app assignment, certificates, OS update/security policy, fleet compliance. |
| Other machine state | adjacent docs/tools | Secrets, auth, services, backups, hardware, project bootstrap, app runtime state. Track explicitly, but do not fold into `updev` without a narrow provider. |

## Operating Principles

- Bare `updev` remains the one-command update workflow.
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
- Trial/local-only state must not leak into shared desired-state manifests.
- Every important human flow needs a stable non-TTY/JSON path.
- Do not absorb all machine state: MDM, app runtime state, secrets, backups,
  hardware, privacy permissions, and project bootstrap stay adjacent.

## Current State

Go `updev` owns the default update workflow, rich list/inventory, read-only
sync, guided add/remove/edit, rollback, security v1, and read-only backend
convergence reports. `updev brewfile ...` and `brewfile` remain compatibility or
low-level surfaces only.

Long-term ordering lives in [ROADMAP.md](ROADMAP.md). The current and next
release targets live in [RELEASE.md](RELEASE.md).
