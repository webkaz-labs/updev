# updev data model

This document defines desired-state, inventory, cache, config, and report
concepts. Command behavior lives in [CLI.md](CLI.md); provider boundaries live
in [ARCHITECTURE.md](ARCHITECTURE.md).

## Desired State Dimensions

Do not overload one field with multiple meanings. Model these independent
dimensions:

| Dimension | Examples | Purpose |
|-----------|----------|---------|
| Scope/profile | environment labels, OS/arch selectors | Where a desired entry may deploy. |
| Lifecycle | `adopted`, `trial`, `candidate`, `local-only`, `deprecated` | Whether an entry is reproducible desired state or being evaluated. |
| Provider/distribution | `mise`, `brew`, `manual`, `mas`, `external`, `vendor` | How the tool/app is installed or tracked. |
| Safety decision | `allow`, `hold`, `review`, `block`, `unknown` | Whether a candidate can be installed/updated now. |
| Management state | `managed`, `missing`, `extra`, `drift`, `unavailable` | Relationship between live state and desired manifests. |

## Desired Sources

| Source | Owner |
|--------|-------|
| `Brewfile` or `Brewfile.tmpl` | Homebrew formulae, casks, taps, and opt-in VS Code extension entries. |
| rendered `~/Brewfile` | Homebrew desired state when a rendered user Brewfile is the active source. |
| `${XDG_CONFIG_HOME:-~/.config}/mise/config.toml` | Global runtime/tool definitions, interpreted through `mise` for inventory desired state. |
| `mise.toml`, `.mise.toml` | Project-local runtime/tool definitions. Inventory uses `mise ls --current --json --cd <dir>` for the source root and manifest directories so parent-directory config inheritance matches mise behavior. |
| `/Applications`, `~/Applications` | macOS-only opt-in manual/vendor live app evidence for `updev list --provider manual`. App bundle paths are read-only evidence and do not become desired state by themselves. |
| configured manual inventory sources such as `~/.config/updev/manual-apps.toml` | User-reviewed manual/vendor app desired metadata. These sources are opt-in and portable; public defaults must not assume a repository-local `docs/apps.md`. |
| future `~/.config/updev/manifests/*` | Language/global package snapshots. |
| future config such as `~/.config/updev/inventory.toml` | Enabled scan sources, override paths, generated report destinations, provider render settings. |
| `${XDG_STATE_HOME:-~/.local/state}/updev/` | Local-only trial inventory, snapshots, cached reports, security decisions, raw evidence caches. |
| generated app reports | Optional manual/non-provider app report bridge. Markdown reports are generated views, not the canonical input for public defaults. |

For temporary or alternate roots, providers read source files under that root
instead of the user's live rendered files. This keeps smoke tests and agent
workflows isolated from the real machine.

The public command default should discover or configure a portable source root;
it should not require `~/.local/share/chezmoi`. Dotfiles-specific root fallback
and `Brewfile.tmpl` mutation remain compatibility behavior until the public
source model supports explicit desired-source paths.

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

## Config Files

Normal persistent user settings are optional and live in:

```text
${XDG_CONFIG_HOME:-~/.config}/updev/config.toml
```

When this file is missing, `updev` uses built-in defaults and should not create
the file just to materialize those defaults. Write only non-default user
choices.

Security exception rules remain in:

```text
${XDG_CONFIG_HOME:-~/.config}/updev/security-policy.json
```

Use TOML for normal policy and UI settings that should persist. Keep endpoint
URLs, API tokens, test fixtures, and secrets in environment overrides rather
than TOML.

Example non-default config:

```toml
[providers]
include_vscode = true

[update]
security = "strict"

[ui]
language = "ja"
description_translation = "manual"

[inventory]
overrides = "~/.config/updev/manual-overrides.local.toml"

[sources]
root = "auto"

[brewfile]
desired = "auto"
write_mode = "disabled"

[inventory.manual]
sources = ["~/.config/updev/manual-apps.toml"]

[inventory.agent]
enabled = false
command = ["codex", "exec"]
batch = true

[backends]
preference_order = [
  "mise/core",
  "mise/aqua",
  "mise/github",
  "mise/gitlab",
  "mise/conda",
  "mise/pipx",
  "mise/npm",
  "mise/gem",
  "mise/go",
  "mise/cargo",
  "mise/dotnet",
  "store/native",
  "package-manager/native",
  "vendor/manual",
]

[[inventory.reports]]
name = "manual-apps"
providers = ["manual", "mas", "vendor", "external"]
format = "markdown"
path = "docs/apps.md"
```

The corresponding `UPDEV_*` variables remain temporary overrides for CI, tests,
debugging, and secrets.

`[ui].description_translation` accepts `auto`, `manual`, or `off`. It controls
only the human `updev list` description cache: `auto` may call the optional
Codex CLI in Japanese TTY text mode, `manual` requires explicit translate flags, and
`off` prevents translation attempts.

## Status Vocabulary

Provider output normalizes to shared status tokens:

```text
ok
drift
missing
extra
candidate
review
held
blocked
error
unavailable
```

Keep meanings stable enough that adjacent tools can combine package/tool state
with OS-setting or project-bootstrap state later.

## Inventory And Cache

`updev list` should prefer the last inventory cache, even when stale, because
provider discovery can be slow and the human TTY must open quickly. Text output
should show cache age when relevant; `--refresh` forces a live read.
Mutation-oriented commands should collect fresh inventory after mutation.

Machine cache files should be compact JSON unless intended for direct human
editing. Migration-only cache readers must stay clearly labeled and should not
become part of the stable public contract.

Update safety evidence uses `update-safety-v1` cache entries. Homebrew
`brew outdated --json=v2` success output is cached for a short TTL to keep
repeated `updev --dry-run` / `updev update` review paths responsive, while
unavailable evidence is cached separately with an error status and shown as
stale/unavailable evidence instead of silently freezing. Security metadata and
scanner/API failures use longer TTLs only when the candidate set is unchanged.
Homebrew safety probes avoid mutating tap state: if `homebrew/core` is already
tapped, the probe can use `HOMEBREW_NO_INSTALL_FROM_API=1`; otherwise it leaves
Homebrew's API path enabled. Cached provider-log failures such as auto-update or
tap-clone output are discarded instead of being reused as stable unavailable
evidence.

## Reports

Stable JSON reports include `schema_version`. Report names include
`syncReport`, `mutationReport`, `rollbackReport`, `backendPlanReport`, and
`dependencyContractReport`. `miseManifestFixReport` is the dry-run/apply report
for `updev fix mise`.

Findings should not use human prose as the only machine key. Safety, posture,
scanner, validation, and review findings should carry stable codes such as
`reason_code` / `remediation_code` plus structured params for variable values
like package name, version, age, threshold, scanner, and advisory id. Existing
English `reason` / `remediation` fields may remain for compatibility and
readable JSON, but localized human text should be derived from the stable code
and params.

Use TOML for user-editable updev configuration and policy. Embedded data that
is shipped with the tool, such as long remediation guidance or generated
catalog-style text, may use JSON when it is easier to validate and test. Short
fixed labels belong in `internal/i18n` Go tables; data-like long guidance should
use structured embedded resources with English and Japanese text side by side.

`dependencyContractReport` records local dependency checks as structured
evidence:

- `schema_version`, `status`, `command`, and `root`;
- `checks[]` with `tool`, `feature`, `required`, `command`, `status`,
  optional `version`, `value`, `source`, `active`,
  `command_shape_supported`, `reason`, `remediation`, and JSON
  `required_fields` / `missing_fields`;
- required Homebrew/mise contract drift changes the report status to `drift`;
- missing optional scanners stay `unavailable` at the check level without
  failing the whole report.

`backendPlanReport` findings include read-only migration evidence. In addition
to current and target provider names, findings record whether a row is a direct
`recommendation` or a review-only `candidate`, candidate command names, whether
those commands are currently on PATH, the current/recommended mise specs when
known, and OS selectors from those specs. GitHub metadata candidates can also
include the current platform, latest-release asset status, matching asset names,
and a limited asset sample when `gh` is available. This lets reviewers preserve
`os = [...]` conditions and check architecture compatibility before moving a
Homebrew entry to mise or rewriting a mise backend. `cargo:` to `mise/github`
rows stay local-build preserving when latest-release assets are missing or do
not match the current platform.

Interactive backend detail rows derive write actions from this evidence instead
of adding separate mutation state to the JSON schema. Safe `mise` rewrites are
available only for direct `mise-backend-rewrite` recommendations with explicit
rewrite permission. When the recommended mise key already exists, the old key is
removed only if the recommended entry's OS selectors cover the current entry;
otherwise the row remains review-only so platform-specific conditions are not
lost. Homebrew ownership removal is available only for direct
`homebrew-to-mise` recommendations where the recommended mise entry already
exists. Candidate rows and missing-target migrations never expose write actions.

Inventory config is intentionally narrow. `state_dir` changes where updev stores
inventory scan/cache state, and `overrides` points to a structured TOML file for
non-obvious manual/vendor app identity and alias metadata. Manual/vendor app
scans use the same model for read-only evidence, identity reconciliation, review
candidates, and generated report previews.

Public defaults must not read a repository-specific `docs/apps.md` as desired
state. Manual inventory should work from live evidence alone unless the user
configures a structured manual source. Repository-local Markdown can remain a
compatibility bridge for this dotfiles workflow only when explicitly configured;
long-term, Markdown is an output generated by `updev inventory render`, not the
portable source of truth.

Structured manual inventory sources use TOML so humans, CLIs, and agents can
edit the same contract without parsing prose:

```toml
[[manual.apps]]
name = "Motion"
aliases = ["Motion.app", "com.apple.motionapp"]
managed_by = "mas"
category = "creative"
description = "Apple motion graphics app for Final Cut workflows."
confidence = "high"
review_status = "accepted"

[manual.apps.identifiers]
bundle_id = "com.apple.motionapp"

[manual.apps.provenance]
source = "human"
evidence = ["mac_app_store_receipt", "app_bundle"]
reviewed_at = "2026-06-08"
```

Agent-assisted enrichment may generate the same shape, but generated entries
start as `review_status = "draft"` and must be displayed as draft evidence until
the user accepts or edits them. Missing or unknown `review_status` values are
treated as draft, not accepted. Draft entries can improve row descriptions and
review suggestions, but they must not silently become desired state, suppress
live-only findings, or authorize provider ownership changes. An entry becomes
normal manual inventory only after it has `review_status = "accepted"`.

Agent enrichment may operate on one row or a bounded batch of ambiguous rows.
Batch mode is allowed so a user can enrich a whole manual review queue in one
agent run, but the safety boundary stays per draft: every generated entry is
schema-validated, remains `draft`, preserves row-level provenance, and needs an
explicit accept/edit/ignore decision before it changes desired state.

Manual inventory JSON can include `review_candidates[]` for live-only or
ambiguous app rows. Candidates carry `provider`, `kind`, `name`, stable
`reason_code`, `remediation_code`, `confidence`, params, evidence, and
`suggested_override` fields. For example, a macOS `.app` bundle that is
installed but not reconciled with docs, overrides, or cask evidence uses
`reason_code=manual_app_live_only`.
Manual inventory plan items add action-oriented fields such as
`suggested_provider`, `review_url`, `install_hint`, and `command_preview` so
agents can show next steps without turning scan evidence into desired state or
executing vendor installers.
`updev inventory review --provider manual` renders those candidates as a
read-only TOML preview for the configured overrides file and returns exit `2`
while review is needed. Write actions are explicit: `--action accept` appends
the suggested override, `--action edit` opens the snippet first, and
`--action ignore` appends a local-only override.

`[backends].preference_order` is a rank override for provider/backend tier
labels. It accepts known labels such as `mise/core`, `mise/aqua`,
`mise/github`, `mise/gitlab`, `mise/conda`, `mise/pipx`, `mise/npm`,
`mise/gem`, `mise/go`, `mise/cargo`, `mise/dotnet`, `store/native`,
`package-manager/native`, and `vendor/manual`; future labels may use the same
`provider/backend` shape. Deprecated or legacy mise labels such as `mise/ubi`,
`mise/vfox`, and `mise/asdf` are recognized when already present or explicitly
configured, but they are not appended by the default order. The setting is
intentionally order-only: backend findings still need provider evidence, command
evidence, and explicit safe write paths before any mutation is offered.

Override entries currently support manual app identity reconciliation:

```toml
[[manual.apps]]
name = "Example App"
aliases = ["example-cask", "Example.app"]
category = "Vendor"
detail = "vendor updater"
managed_by = "vendor"
```

`lifecycle = "local-only"` marks installed apps that should remain local and
suppresses repeated live-only review without counting them as desired state.

Repo-relative paths resolve from the configured source root. Absolute and `~`
paths are allowed for local-only files. Generated files should be clearly
marked or checked by `updev` so manual edits do not silently become canonical.

## Trial And Manual State

`adopted` entries are deployment candidates. `trial` entries are visible in
status/review but are not installed on other machines. `local-only` entries
live in local state. `candidate` entries can exist in a catalog without being
applied. Promotion from trial to adopted needs an explicit command and focused
diff.

Manual apps are part of reproducibility, but "listed as required" is not the
same as "safe to install automatically". UI and reports must preserve that
distinction.

Manual/vendor live discovery is platform-specific. The shared inventory model
should stay OS-neutral, but scanners are per platform:

- macOS scanner evidence comes from `.app` bundles, bundle `Info.plist`, Mac App
  Store receipts / `mas list`, and Homebrew cask metadata.
- Linux scanners should be added separately from `.desktop` files, Flatpak,
  Snap, AppImage, and distro package metadata.
- Windows scanners should be added separately from installed-app registry
  entries, Start Menu shortcuts, MSIX/Appx metadata, winget, scoop, or choco.

All platform scanners should normalize into the same identity/evidence/review
shape:

| Layer | Examples | Rule |
|-------|----------|------|
| Provider | `manual`, `mas`, `flatpak`, `winget`, `vendor` | Installation/update owner or distribution channel. Keep this separate from OS-specific scanner names. |
| Identity | display name, normalized name, app id, bundle id, MAS id, desktop id, package id | Use the strongest stable id available; names are fallback and matching hints. |
| Evidence | source path, scanner name, confidence, version, owner/update provider | Preserve where the fact came from so generated reports and review candidates are explainable. |
| Review | `reason_code`, `remediation_code`, confidence, params, suggested override fields | Ambiguous or unsafe rows become review candidates instead of silent desired state. |
