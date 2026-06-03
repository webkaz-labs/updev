# updev data model

This document defines desired-state, inventory, cache, config, and report
concepts. Command behavior lives in [CLI.md](CLI.md); provider boundaries live
in [ARCHITECTURE.md](ARCHITECTURE.md).

## Desired State Dimensions

Do not overload one field with multiple meanings. Model these independent
dimensions:

| Dimension | Examples | Purpose |
|-----------|----------|---------|
| Scope/profile | `work`, `personal`, OS/arch selectors | Where a desired entry may deploy. `personal` includes the `work` baseline and adds personal-only entries. |
| Lifecycle | `adopted`, `trial`, `candidate`, `local-only`, `deprecated` | Whether an entry is reproducible desired state or being evaluated. |
| Provider/distribution | `mise`, `brew`, `manual`, `mas`, `external`, `vendor` | How the tool/app is installed or tracked. |
| Safety decision | `allow`, `hold`, `review`, `block`, `unknown` | Whether a candidate can be installed/updated now. |
| Management state | `managed`, `missing`, `extra`, `drift`, `unavailable` | Relationship between live state and desired manifests. |

## Desired Sources

| Source | Owner |
|--------|-------|
| `Brewfile.tmpl` | Homebrew formulae, casks, taps, and opt-in VS Code extension entries. |
| rendered `~/Brewfile` | Active-profile Homebrew desired state for the default source root. |
| `dot_config/mise/config.toml` | Global runtime/tool definitions, interpreted through `mise` for inventory desired state. |
| `mise.toml`, `.mise.toml` | Project-local runtime/tool definitions. Inventory uses `mise ls --current --json --cd <dir>` for the source root and manifest directories so parent-directory config inheritance matches mise behavior. |
| `/Applications`, `~/Applications` | macOS-only opt-in manual/vendor live app evidence for `updev list --provider manual`. App bundle paths are read-only evidence and do not become desired state by themselves. |
| future `~/.config/updev/manifests/*` | Language/global package snapshots. |
| future config such as `~/.config/updev/inventory.toml` | Enabled scan sources, override paths, generated report destinations, provider render settings. |
| `${XDG_STATE_HOME:-~/.local/state}/updev/` | Local-only trial inventory, snapshots, cached reports, security decisions, raw evidence caches. |
| `docs/apps.md` | Current manual/non-provider app report bridge. Long term it should be generated from scan output plus structured overrides. |

For temporary or alternate roots, providers read source files under that root
instead of the user's live rendered files. This keeps smoke tests and agent
workflows isolated from the real machine.

Mise inventory desired/live state follows mise's native config resolution.
Manifest hygiene still reads the TOML files directly because it needs file,
line, and raw version evidence. Mise desired entries must be pinned in global
and project-local manifests. `latest` is not allowed, object-style entries must
include `version`, and `lts` is allowed only for `node`. `updev status` /
`updev check` expose violations as blocked `mise` manifest items so cached
inventory does not hide current manifest drift.

## Config Files

Normal persistent user settings live in:

```text
${XDG_CONFIG_HOME:-~/.config}/updev/config.toml
```

Security exception rules remain in:

```text
${XDG_CONFIG_HOME:-~/.config}/updev/security-policy.json
```

Use TOML for normal policy and UI settings. Keep endpoint URLs, API tokens,
test fixtures, and secrets in environment overrides rather than TOML.

Config surface:

```toml
[security.homebrew]
min_release_age_days = 3
min_tap_age_days = 30

[security.vscode]
min_install_count = 1000
min_average_rating = 2.0
min_extension_age_days = 14
min_update_age_days = 3

[providers]
include_vscode = false

[update]
security = "warn" # warn | strict | off

[ui]
language = "auto"
interactive = "auto"
progress = true

[inventory]
state_dir = "~/.local/state/updev/inventory"
overrides = "~/.config/updev/inventory-overrides.toml"

[[inventory.reports]]
name = "manual-apps"
providers = ["manual", "mas", "vendor", "external"]
format = "markdown"
path = "docs/apps.md"
```

The corresponding `UPDEV_*` variables remain temporary overrides for CI, tests,
debugging, and secrets.

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

Keep meanings aligned with `macos-settings` where possible so a future
dotfiles-wide review can combine package/tool state and OS-setting state.

## Inventory And Cache

`updev list` and related commands should default to cached inventory when fresh
enough, because provider discovery can be slow. Text output should show cache
age when relevant; `--refresh` forces a live read and mutation-oriented commands
should collect fresh inventory after mutation.

During migration, reuse legacy cache files such as `desc_ja.tsv`, `meta.json`,
`rows_cache.json`, and `brew_state.json` so accumulated descriptions and
Japanese translations are not lost. New machine cache files should be compact
JSON unless intended for direct human editing.

Update safety evidence uses `update-safety-v1` cache entries. Homebrew
`brew outdated --json=v2` success output is cached for a short TTL to keep
repeated `updev --dry-run` / `updev update` review paths responsive, while
unavailable evidence is cached separately with an error status and shown as
stale/unavailable evidence instead of silently freezing. Security metadata and
scanner/API failures use longer TTLs only when the candidate set is unchanged.

## Reports

Stable JSON reports include `schema_version`. Report names include
`syncReport`, `mutationReport`, `rollbackReport`, `backendPlanReport`, and
`dependencyContractReport`. `miseManifestFixReport` is the `updev v0.3.0` dry-run/apply
report for `updev fix mise`.

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
follow the macset pattern of structured embedded resources with English and
Japanese text side by side.

`dependencyContractReport` records local dependency checks as structured
evidence:

- `schema_version`, `status`, `command`, and `root`;
- `checks[]` with `tool`, `feature`, `required`, `command`, `status`,
  optional `version`, `reason`, `remediation`, and JSON
  `required_fields` / `missing_fields`;
- required Homebrew/mise contract drift changes the report status to `drift`;
- missing optional scanners stay `unavailable` at the check level without
  failing the whole report.

`backendPlanReport` findings include read-only migration evidence. In addition
to current and recommended provider names, findings record candidate command
names, whether those commands are currently on PATH, the current/recommended
mise specs when known, and OS selectors from those specs. This lets reviewers
preserve `os = [...]` conditions before moving a Homebrew entry to mise or
rewriting a mise backend.

Inventory config is intentionally narrow in `updev v0.3.0`. `state_dir` changes where
updev stores inventory scan/cache state, and `overrides` points to a structured
TOML file for non-obvious manual/vendor app identity and alias metadata. In
`updev v0.4.0`, this model expands into read-only manual/vendor app scans, identity
reconciliation, review candidates, and generated report previews.

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

Repo-relative paths resolve from the chezmoi source root. Absolute and `~`
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
