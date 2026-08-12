# updev data model

This document is the stable index for updev state and persistence contracts.
Provider behavior lives in [ARCHITECTURE.md](ARCHITECTURE.md), command behavior
in [CLI.md](CLI.md), and security semantics in [SECURITY.md](SECURITY.md).

## Core Model

Updev keeps these dimensions independent:

| Dimension | Examples | Meaning |
|-----------|----------|---------|
| Deployment scope | environment labels, OS/arch selectors | Where desired state may apply. Scope names are project data. |
| Lifecycle | adopted, trial, candidate, local-only, deprecated | Whether an item is reproducible desired state or under evaluation. |
| Provider | mise, brew, manual, mas, external, vendor | How an item is installed or tracked. |
| Safety decision | allow, hold, review, block, unknown | Whether mutation may proceed now. |
| Management state | managed, missing, extra, drift, unavailable | Relationship between live and desired state. |

Canonical identity is provider-aware and kind-aware. Display names and aliases
may aid reconciliation, but they do not replace the canonical provider/kind/name
identity used by reports, policy, and mutation routes.

## State Layers

| Layer | Contract |
|-------|----------|
| Source state | Editable TOML, Brewfile template, override, or provider source. |
| Rendered state | Desired state active for the current deployment scope. |
| Live state | Provider-native installed/current evidence. |
| Evidence | Update, security, backend, provenance, and scanner observations. |
| Decision | The normalized allow/hold/review/block and next-action result. |
| Report/cache | Deterministic snapshot used by text, JSON, TUI, and `updev last`. |

Source-only entries excluded from rendered state are `profile-mismatch`, not
missing packages and not automatic adoption candidates. Generated reports and
caches are evidence, never a second writable desired-state authority.

## Persistence Boundaries

- Config is optional and follows the documented XDG/default precedence.
- Security policy is separate from ordinary config because it contains
  explicit, expiring decisions.
- One active mise tool entry has one writable owner; updev must not silently
  write another active config file.
- `updev apply brewfile` applies missing desired items only. Extras remain
  read-only drift and outdated items remain owned by `updev update`.
- Mutation reports carry snapshots, diffs, exact targets, and rollback
  guidance where the command owns source writes.

## Detail Documents

| Document | Read when |
|----------|-----------|
| [desired-state.md](data-model/desired-state.md) | Changing desired sources, deployment scope, mise bootstrap projection, package parity, or metadata sidecars. |
| [config-and-apply.md](data-model/config-and-apply.md) | Changing config precedence/schema, Brewfile mutation, package apply, or status vocabulary. |
| [inventory-and-reports.md](data-model/inventory-and-reports.md) | Changing inventory records, caches, report schemas, update/security evidence, or TUI data projection. |
| [manual-state.md](data-model/manual-state.md) | Changing trial/manual app state, override compatibility, or generated manual reports. |

Keep schema and current ownership rules here. Migration history belongs in git
and tag-specific release notes.
