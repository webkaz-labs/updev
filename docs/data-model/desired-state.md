# Desired State And Sources

Desired-state dimensions, deployment scope, mise bootstrap projection, package
parity, and metadata sidecars. Return to the [data-model index](../DATA-MODEL.md).

## Desired State Dimensions

Do not overload one field with multiple meanings. Model these independent
dimensions:

| Dimension | Examples | Purpose |
|-----------|----------|---------|
| Deployment scope | environment labels, OS/arch selectors | Where a desired entry may deploy. Scope names are project data, not fixed public categories. |
| Lifecycle | `adopted`, `trial`, `candidate`, `local-only`, `deprecated` | Whether an entry is reproducible desired state or being evaluated. |
| Provider/distribution | `mise`, `brew`, `manual`, `mas`, `external`, `vendor` | How the tool/app is installed or tracked. |
| Safety decision | `allow`, `hold`, `review`, `block`, `unknown` | Whether a candidate can be installed/updated now. |
| Management state | `managed`, `missing`, `extra`, `drift`, `unavailable` | Relationship between live state and desired manifests. |

## Desired Sources

| Source | Owner |
|--------|-------|
| `Brewfile` or `Brewfile.tmpl` | Homebrew formulae, casks, taps, and opt-in VS Code extension entries. |
| rendered `~/Brewfile` | Homebrew desired state when a rendered user Brewfile is the active source. |
| `${XDG_CONFIG_HOME:-~/.config}/mise/config.toml` | Current writable global runtime/tool definitions, interpreted through `mise` for inventory desired state. Updev add/remove/rename and automatic bump must not silently write a different file from the one that owns the active entry. |
| `mise.toml`, `.mise.toml` | Project-local runtime/tool definitions. Inventory uses `mise ls --current --json --cd <dir>` for the source root and manifest directories so parent-directory config inheritance matches mise behavior. |
| active mise environment config files | Future deployment-scope differences discovered from `mise config ls --json`. They are read-only to updev until source-aware mutation can identify and preserve the owning file. Every loaded path remains visible in source evidence even when it declares no tools. Environment names are inferred only from mise's standard filenames and are never matched against hardcoded public scope names. No live environment file exists yet. |
| `/Applications`, `~/Applications` | macOS-only opt-in manual/vendor live app evidence for `updev list --provider manual`. App bundle paths are read-only evidence and do not become desired state by themselves. |
| configured manual inventory sources such as `~/.config/updev/manual-apps.toml` | User-reviewed manual/vendor app desired metadata. These sources are opt-in and portable; public defaults must not assume a repository-local `docs/apps.md`. |
| future `~/.config/updev/manifests/*` | Language/global package snapshots. |
| future config such as `~/.config/updev/inventory.toml` | Enabled scan sources, override paths, generated report destinations, provider render settings. |
| `${XDG_CACHE_HOME:-~/.cache}/updev/` | Rebuildable local caches: inventory cache when no state dir is configured, description/metadata compatibility cache, cached update reports, update-safety evidence, and provider/security metadata. |
| `${XDG_STATE_HOME:-~/.local/state}/updev/` | Future non-rebuildable local state such as accepted trial inventory or snapshots. Do not put rebuildable provider evidence here. |
| generated app reports | Optional manual/non-provider app report bridge. Markdown reports are generated views, not the canonical input for public defaults. |

For temporary or alternate roots, providers read source files under that root
instead of the user's live rendered files. This keeps smoke tests and agent
workflows isolated from the real machine.

## Source, Rendered State, And Scope Drift

`source state` is the unrendered desired source, such as `Brewfile.tmpl` or
tool TOML. `rendered state` is the desired state that is active for the current
machine, such as rendered `~/Brewfile` when `[brewfile].desired = "auto"`.

`profile-mismatch` is retained as the inventory status name for compatibility,
but it means deployment-scope mismatch: an item exists in source state for some
scope and is installed locally, while the active rendered state does not include
it. This is not a fresh adoption candidate, so Brewfile adoption actions must
not be shown for that row. The user can render the matching deployment scope,
remove the local install, or intentionally promote the item into the active
scope.

The public command default should discover or configure a portable source root;
it should not require a fixed dotfiles checkout path. Dotfiles-specific root
fallback and `Brewfile.tmpl` mutation remain compatibility behavior until the
public source model supports explicit desired-source paths.

Public defaults and advanced integrations have different responsibilities:

- Defaults must be safe, read-mostly, and broadly portable. A fresh install can
  inspect live evidence and provider manifests, but it must not assume a
  personal dotfiles layout, create config just to store defaults, or mutate a
  desired-state file that was not explicitly selected.
- Auto-detection may identify useful candidates such as `~/Brewfile`,
  `Brewfile`, mise config, Homebrew, Mac App Store receipts, and future Linux or
  Windows provider metadata. Detection is evidence until a write target is
  configured or confirmed.
- Advanced workflows opt in through config. Chezmoi source roots,
  `Brewfile.tmpl` writes, repository-local Markdown reports, manual inventory
  structured sources, and agent enrichment are integration features, not public
  defaults.
- Compatibility modes can preserve this dotfiles workflow, but they should be
  named and visible in diagnostics so public users can tell which assumptions
  are active.

Mise inventory desired/live state follows mise's native config resolution.
Manifest hygiene still reads the TOML files directly because it needs file,
line, and raw version evidence. Mise desired entries must be pinned in global
and project-local manifests. `latest` is not allowed, object-style entries must
include `version`, and `lts` is allowed only for `node`. `updev status` /
`updev check` expose violations as blocked `mise` manifest items so cached
inventory does not hide current manifest drift.

Resolved reads and source writes are separate contracts. A successful
`mise config ls` or `mise ls --current` parity check proves what mise sees; it
does not prove that updev add/remove/rename, `mise upgrade --bump`, a shell
wrapper, or a chezmoi onchange hash will update or observe the same source.
Global `[tools]` therefore stays in one writable `config.toml` until source-aware
mutation and watcher tests cover every active file. Package-source migration
must switch the Brewfile writer/wrapper and mise package writer atomically; a
generated daily Brewfile is not an intermediate canonical source.

### Mise Bootstrap Package Projection

The read-only mise package adapter combines two stable JSON contracts:

- `mise config ls --json --cd <root>` supplies every active config source;
- aggregate `mise bootstrap status --json --cd <root>` supplies desired package
  rows grouped by manager.

Do not use `mise bootstrap packages status --json` as the desired-state input.
On an unsupported platform it can omit package rows for an unavailable manager,
while aggregate status preserves them with `available = false` and a reason.
This matters on Intel macOS, where a `brew` declaration must remain visible even
when mise cannot execute it natively.

The normalized read model is `BootstrapPackageSet`. `sources` contains every
non-empty active config path and does not assume chezmoi or one fixed filename.
Each package record contains stable `manager:name` identity, manager, package
name, requested version, current state, optional installed version, manager
availability, and an optional unavailability reason. Managers are data, not a
hardcoded allowlist. Records sort by manager/name/version so JSON fixtures are
deterministic.

This projection is evidence only in the current release. It does not change the
active Brewfile authority, install packages, or select a native/mise executor.

### Brewfile / Mise Package Parity

`updev doctor package-parity` joins the active Brewfile desired source with the
read-only mise bootstrap package projection. Comparison uses canonical
`brew/formula/<name>`, `brew/cask/<name>`, and `brew/tap/<name>` identities and
returns `match`, `brewfile-only`, or `mise-only`. Qualified package names also
contribute their implicit tap identity. Managers outside Homebrew formula/cask
scope are counted but do not become false Brewfile drift rows.

Package desired rows come from aggregate `mise bootstrap status --json`. Mise
v2026.8.2 omits `[bootstrap.brew.taps]` from status and plan JSON, so tap parity
parses only the active config paths already reported by `mise config ls --json`.
This is a bounded exception to the structured-output preference; TOML parsing
is used rather than human-table parsing. The parity report is read-only and is
not an executor plan or source-migration action.

The executor report consumes this parity data but does not change it.
Each executor row records canonical identity, desired source (`brewfile`,
`mise`, or `both`), platform, manager availability, native capability,
sidecar preference, selected executor, status, and a stable reason code. An
unannotated `both` row and an unavailable explicit executor fail closed as
`unsupported-executor`; they are not silently routed to another provider.

### Package Metadata Sidecar

Package metadata that is not represented by a desired provider declaration is
loaded from the optional strict TOML sidecar:

```text
${XDG_CONFIG_HOME:-~/.config}/updev/package-metadata.toml
```

`[mise_bootstrap].package_metadata` in the normal updev config selects a
non-default path. Relative values resolve from the directory containing the
active updev config, so standalone `--config` and `UPDEV_CONFIG` use do not
depend on a dotfiles checkout.

The sidecar uses `schema_version = 1` and package tables keyed by canonical
`provider/kind/name` identity. It contains rationale, lifecycle, preferred
executor annotations, intentional duplicate annotations, and bounded provider
options; it never declares package existence or version. Unknown fields,
duplicate tables, invalid canonical IDs, and empty metadata entries fail
closed. Missing files are valid empty metadata sets. Entries absent from the
caller-supplied active desired-ID set produce sorted
`stale-package-metadata` diagnostics and never become desired state.

The sidecar remains a read-only metadata layer. Executor selection is owned by
the package-executor report, and `updev apply brewfile` consumes that decision
only for missing, gate-approved items. Desired-source writes, extra removal,
and outdated updates remain outside this metadata boundary. The completed
slice and its rollback contract are defined in the
mise machine-management plan.
