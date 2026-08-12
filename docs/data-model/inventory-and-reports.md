# Inventory, Cache, And Reports

Inventory records, cache behavior, report schemas, and evidence projection.
Return to the [data-model index](../DATA-MODEL.md).

## Inventory And Cache

`updev list` should prefer the last inventory cache, even when stale, because
provider discovery can be slow and the human TTY must open quickly. Text output
should show cache age when relevant; `--refresh` forces a live read.
Mutation-oriented commands should collect fresh inventory after mutation.

Machine cache files should be compact JSON unless intended for direct human
editing. Migration-only cache readers must stay clearly labeled and should not
become part of the stable public contract.

Rebuildable caches live under `${XDG_CACHE_HOME:-~/.cache}/updev/`.
Path construction belongs to `internal/updevpath`, not command handlers.
Current cache subtrees include:

| Cache | Owner | Contents |
|-------|-------|----------|
| `inventory-v1.json` | `cmd` through `updevpath.InventoryCacheFile` | Last inventory report when no configured inventory state dir is set. |
| `reports/` | `cmd` through `updevpath.ReportCacheDir` | Last update/dry-run reports and timestamped update report snapshots. |
| `update-safety-v1/` | `internal/securitygate` | Provider update-safety evidence keyed by candidate inputs. |
| `security-metadata-v1/` | provider/security metadata collectors through `updevpath.SecurityMetadataCacheFile` | GitHub repository metadata, registry metadata, and other bounded provider evidence. |
| legacy description files | `cmd` compatibility readers | `desc_ja.tsv`, `meta.json`, `rows_cache.json`, and Homebrew version cache used to keep list output responsive. |

Update safety evidence uses `update-safety-v1` cache entries. Homebrew
`brew outdated --json=v2 --greedy` success output is cached for a short TTL to keep
repeated `updev --dry-run` / `updev update` review paths responsive, while
unavailable evidence is cached separately with an error status and shown as
stale/unavailable evidence instead of silently freezing. Security metadata and
scanner/API failures use longer TTLs only when the candidate set is unchanged.
Homebrew safety probes avoid mutating tap state and always set
`HOMEBREW_NO_AUTO_UPDATE=1` for `brew outdated --json=v2 --greedy`, so
candidate discovery does not trigger an implicit metadata refresh. It does not
disable Homebrew install-from-API because Homebrew 6 may otherwise omit many
outdated cask candidates from the JSON contract.
Cached provider-log failures such as auto-update or tap-clone output are
discarded instead of being reused as stable unavailable evidence.

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
- `compatibility_ledger` with generated time, root, and entries for each check:
  `tool`, `feature`, `required`, `version`, `status`, `supported`,
  `support_label`, command evidence, and remediation;
- required Homebrew/mise contract drift changes the report status to `drift`;
- missing optional scanners stay `unavailable` at the check level without
  failing the whole report.

`supportReport` is the read-only catalog returned by `updev support` and
`updev doctor support`. It has `schema_version`, `status`, `tool`, `version`,
`summary`, and `entries[]`. Each entry records:

- `surface`: `provider`, `command`, `report`, or `inventory_source`;
- `name`;
- `label`: `supported_preview`, `experimental`, `compatibility`, or
  `deferred`;
- `summary`;
- optional `scope`, `evidence`, `limitations`, and `next`.

The support catalog is release-owned static metadata. It is not user policy and
does not change runtime decisions by itself; it makes public preview boundaries
visible to humans, agents, and CI.

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
source_url = "https://apps.apple.com/app/motion/id434290957"
owner = "Apple"
update_owner = "mas"
provider_metadata = "mac app store receipt"
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
Evidence should preserve source quality fields when available:
`review_url`, `source_url`, `owner`, `managed_by`, `update_owner`,
`ownership_confidence`, and `provider_metadata`. macOS bundle scans populate
low-confidence `Info.plist` ownership evidence by default, MAS receipts and
`mas list` populate high-confidence `mas` ownership evidence, and Homebrew cask
evidence populates high-confidence `brew` ownership evidence.
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

`[backends].keep_homebrew` is an optional list of canonical Homebrew desired
identities in `brew/<name>` or `cask/<name>` form. It records local bootstrap,
native integration, duplicate dependency, or other ownership reasons that are
not portable enough to hardcode in the public recommendation engine. Matching
items remain Homebrew-owned and are excluded before mise registry or generic
GitHub inference. Invalid or duplicate identities are ignored by the current
config compatibility parser; validation and user-facing diagnostics are a
future config-schema hardening concern.

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
