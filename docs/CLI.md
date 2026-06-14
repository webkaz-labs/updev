# updev CLI

This document defines the command and human/agent output contract. Product
mission lives in [DESIGN.md](DESIGN.md); data shape lives in
[DATA-MODEL.md](DATA-MODEL.md).

## Command Surface

Human-facing commands should stay small:

```bash
updev --config file --no-color ...
updev           # update workflow, then compact review dashboard/selector on TTY
updev update    # explicit default update workflow
updev list      # read-only grouped inventory browser on TTY
updev hub       # full selector menu for every list view/filter
updev ls        # alias for list
updev inventory # alias for fast/filterable inventory
updev inventory scan # scan opt-in manual/vendor inventory evidence without writing
updev inventory plan # group manual/vendor app evidence into review actions without writing
updev inventory check # alias for inventory plan
updev inventory review # preview review-needed inventory overrides without writing
updev inventory render # preview generated inventory reports without writing
updev status    # compact current package/tool state
updev st        # alias for status
updev check     # desired manifests vs installed state
updev ck        # alias for check
updev check --dependencies # dependency CLI/JSON contract checks
updev plan      # install/remove/update plan where available
updev sync      # reconcile desired manifests and live state
updev add       # guided desired-manifest add
updev remove    # guided desired-manifest remove
updev edit      # direct manifest editing with validation
updev rollback  # restore latest manifest snapshot
updev fix mise  # dry-run-first mise manifest hygiene fixer
updev last      # reopen cached last update dashboard/report
updev --plain   # stable text update output without TUI
updev version   # current implemented tool release
updev support   # support-level labels for providers, commands, reports, and inventory sources
updev --version # short version check; -v is an alias
updev skill     # print the installable agent skill guidance
updev help agent # print the detailed agent workflow guide
```

Advanced or internal surfaces may exist, but should not be the main human
journey:

```bash
updev brewfile ...
updev doctor dependencies
updev doctor support
updev security ...
updev trial ...
```

Backend convergence is an intentional human workflow, not only an advanced
escape hatch. `updev backends plan/doctor` remain the stable non-TTY/JSON entry
points, while the `updev` selector, dashboard rows, and `updev list` TTY flow
offer visible routes into the same findings when backend recommendations exist.
Backend findings expose `recommended_tier` and `preference_rank`: reliable
`mise` core backends first, then mise registry acceptance tiers (`mise/aqua`,
`mise/github`, `mise/gitlab`, `mise/conda`, then language package backends such
as `mise/pipx`, `mise/npm`, `mise/gem`, `mise/go`, `mise/cargo`, and
`mise/dotnet`), followed by store/native package evidence, native package
managers, and vendor/manual ownership when those are stronger for the item.
Deprecated or legacy mise backends such as `mise/ubi` and `mise/asdf` are
recognized for existing state and explicit overrides, but they are not default
recommendation tiers.
`[backends].preference_order` can reorder tier labels without creating a
default-only config file. The plan command itself is read-only; the interactive
`updev` and `updev list` backend detail views show an `applyability` line for
each row and expose write actions only when the path is safe. If the preferred
mise backend entry does not exist, a safe rewrite renames the current key after
confirmation and preserves the current spec. If the preferred entry already
covers the current entry's OS selectors, the detail view can remove the old
mise key after confirmation. Brewfile ownership removal is available only when
the target mise entry already exists; adding a missing mise entry and removing
Homebrew ownership remains review-only.
Metadata-inferred Homebrew or language-package moves to `mise/github` are
reported as candidates, not direct recommendations: `npm`/`cargo` package
metadata can identify the upstream repository, but reviewers must still verify
GitHub release assets, version mapping, and official distribution ownership.
When `gh` is available, `updev backends plan` samples latest-release asset names
and records whether they appear compatible with the current OS/architecture.
Japanese TTY output uses candidate-oriented labels and action copy for these
rows so they are not confused with safe rewrite actions.
For `cargo:` entries, `mise/github` is preferred only when a compatible GitHub
release asset is visible or an explicit safe rule has equivalent evidence. If
latest release assets are missing or do not match the current platform, the row
keeps the current `cargo:` local-build path and explains the missing evidence.

Bare `updev` is not a read-only hub. The valuable existing behavior is the
no-argument update command, so preserve it unless the user explicitly opts into
another mode.

The interactive dashboard and shared detail browser are the primary human UX for
post-update review. `updev` and `updev last` open the dashboard automatically
on TTY text output; use `--plain`, `--no-interactive`, or `--format json` when
an agent/script needs deterministic non-TUI output. Dashboard rows can open
inventory, manual app review,
backend convergence, security, update-step filters, and logs without dropping
back to a separate footer-only selector when the action fits the focused row.
Collapsed rows surface compact badges such as action count, updated/deferred
counts, security decision, release asset status, and backend applyability, and
the focused row always shows its `a/1`, `2`, ... action hints before expansion,
so the user can scan the dashboard and act without falling back to a separate
footer-only selector. Expanded detail rows separate `details`, `evidence`, and
`actions`, include status-colored summaries, individual updated/deferred item
rows, and numbered actions. Safe actions can be executed from the focused or
expanded row: manual override accept/edit/ignore,
cask/MAS/vendor evidence review, temporary security policy allow/hold decisions,
security allow with custom reason/expiry, temporary security allow plus provider
rerun, safe backend rewrites, covered old mise-entry removals, and safe Brewfile
ownership removal when mise already owns the tool. Read-only rows explain the
missing evidence or next command instead of exposing a write action.
`updev list --interactive` installed inventory rows do not run write actions
directly, but row actions can route the focused item to the relevant review
domain, starting with manual app review and backend convergence. The full target
contract is tracked in [UX.md](UX.md).

Global flags:

- `--config <path>` loads normal TOML policy from a non-default path for this
  invocation. It is equivalent to setting `UPDEV_CONFIG` before running updev.
- `--no-color` disables ANSI color for human text output by setting the
  standard `NO_COLOR` behavior at the CLI boundary.
- `updev v0.x` does not add global `--verbose` or `--quiet`. Current diagnostic
  affordances are command-specific `--details`, `updev last --section ...`,
  and `--format json`; add global verbosity only when a concrete cross-command
  diagnostic need appears.
- `updev version`, `updev --version`, and `updev -v` report the current
  implemented release contract, currently `updev v0.7.4`. JSON output from
  `updev version --format json` includes SemVer parts and the stable/pre-stable
  contract label.
- `updev support` and `updev doctor support` print the current support-level
  catalog. Use `--surface provider|command|report|inventory_source|all`,
  `--label supported_preview|experimental|compatibility|deferred`, and
  `--format json` when an agent or CI job needs machine-readable support
  boundaries.
- `updev skill` prints the embedded `docs/agent/SKILL.md`. `updev skill
  --full` appends the detailed `docs/agent/USAGE.md` guide. `updev help agent`
  prints the detailed guide only. These commands are read-only and are intended
  for agents and users configuring agent skills.
- Read-only aliases are supported for common commands: `ls` for `list`,
  `st` for `status`, and `ck` for `check`.

Portable defaults:

- With no config, `updev` should use the current working directory as the source
  root and inspect provider-native live/config evidence without assuming
  `~/.local/share/chezmoi`.
- `UPDEV_ROOT`, `[sources].root`, or `CHEZMOI_SOURCE_DIR` can select a different
  source root. `CHEZMOI_SOURCE_DIR` is kept as compatibility for dotfiles-hosted
  workflows.
- `[brewfile].desired` controls whether Homebrew desired state comes from
  `auto`, `home`, `root`, `template`, or is `disabled`. Write behavior is
  separate and remains controlled by `[brewfile].write_mode`.
- Repository-local manual Markdown such as `docs/apps.md` is ignored unless
  `[inventory.manual].markdown_compat = true` or an explicit Markdown source is
  configured.

Exit codes:

| Code | Meaning |
|------|---------|
| `0` | command completed with no blocking drift or finding |
| `1` | runtime error |
| `2` | read-only report found drift, findings, held, or review-needed state |
| `64` | usage, config, or flag error |

## Human TTY Contract

The TTY experience should feel like a review tool, not a long log:

- show progress while providers run so Homebrew/mise do not look frozen;
- end with a compact dashboard containing outcome, attention counts, changed
  packages, held/skipped/error counts, and report path;
- let the user drill down by selection instead of remembering follow-up
  commands;
- keep color/weight cues for headings, providers, counts, statuses, active
  versions, requested versions, inactive rows, and risky decisions;
- make truncated rows expandable for descriptions, reasons, remediation,
  evidence, and logs;
- keep selectors read-only unless a separate confirmed mutation flow is opened;
- never enter a selector for non-TTY, CI, `--format json`, or explicit
  non-interactive modes.

Navigation uses a predictable review stack:

- maintain a stack for `hub -> filter menu -> section -> detail`;
- preserve focus, scroll position, expanded rows, and active filters when
  returning;
- `Back` removes the most recent filter in filtered views before leaving the
  section;
- `Home` returns to the top dashboard without discarding the cached report;
- `Esc` / `q` exits or backs out according to the current depth;
- `?` opens a help overlay;
- `/` starts text filtering, and `x` or `Esc` clears an active text filter.

Mouse behavior is opt-in because many terminals cannot both send app-level
mouse events and allow normal drag text selection:

- default mode is mouse off;
- `m` cycles `off -> wheel -> click`;
- wheel mode scrolls without moving the selected row;
- click mode expands on press/release on the same row and ignores drag
  releases on another row.

## Update Flow

Bare `updev` and `updev update`:

1. discover update candidates;
2. run only the minimum candidate-scoped safety work needed for the pending
   mutation set;
3. stream provider logs for real updates;
4. collect post-update inventory;
5. write a full structured report to cache;
6. render a compact final dashboard and TTY selector.

Default human update output shows meaningful outcomes only: updated packages,
deferred/held/skipped items, errors, and drift. Raw provider commands and full
logs belong in JSON/report logs and `updev last --section logs`. Homebrew
auto-update progress is treated as provider log noise; actual Homebrew updates,
such as updated taps, are still listed as update outcomes.

`strict` is the default mode and holds mutation when required evidence is
missing, stale, low-confidence, or policy-blocked. `warn` mode may proceed
while showing safety gaps. `off` skips mutation gates and must be visible in the
report. The default update gate covers Homebrew candidates and mise candidates
from `mise outdated --json --cd <root>`; Brewfile-managed VS Code extension
updates remain opt-in. The updev-owned mise gate release-age checks
GitHub-backed entries, explicit `aqua:<owner>/<repo>` entries, registry
`aqua` entries, selected core runtimes such as `go`, `node`, and `rust`,
high-confidence `npm:` / `cargo:` / `pipx:` entries, and data-driven vfox
metadata entries when an exact candidate release-date resolver exists.
Unsupported or opaque mise backends stay review-held in strict mode.
`[security.mise].min_release_age_days` and
`UPDEV_MISE_MIN_RELEASE_AGE_DAYS` control the mise threshold without changing
the Homebrew threshold. `[security.homebrew].outdated_timeout_seconds` and
`UPDEV_BREW_OUTDATED_TIMEOUT_SECONDS` control the bounded Homebrew outdated
probe timeout. mise native `minimum_release_age` holds are surfaced by comparing
normal `mise outdated --json` output with a single age-disabled
`MISE_MINIMUM_RELEASE_AGE=0d mise outdated --json` provider probe, avoiding
per-tool `mise latest` calls in the normal gate path.
When updev launches mise subprocesses, it passes an available GitHub token from
updev/gh environment sources as `MISE_GITHUB_TOKEN` without recording the token
in the command line.

Strict update execution is candidate-scoped. If mise reports an age-allowed
candidate while a newer age-disabled probe candidate is still too new, updev
runs scoped `mise upgrade --minimum-release-age <Nd> <tool...>` using the
updev-configured threshold and keeps the newer candidate visible as held. This
does not require mise's global native age setting to be configured, though a
native hold is still surfaced when present. Homebrew is also scoped to allowed
package names so held packages do not block unrelated Homebrew updates; however
normal `brew upgrade` cannot generally install an older intermediate release
for the same formula/cask, so a too-new latest Homebrew candidate stays held
until it ages in or is explicitly allowed by policy. To avoid holding
continuously released packages forever, strict Homebrew upgrades do not run
`brew update` before `brew upgrade`; they upgrade gated local-metadata
candidates with `HOMEBREW_NO_AUTO_UPDATE=1`, then refresh metadata afterward.
Homebrew candidate discovery also sets `HOMEBREW_NO_INSTALL_FROM_API=1` so
Homebrew 6 reads local tap metadata for `brew outdated --json=v2 --greedy`
instead of depending on unavailable internal package JSON endpoints.
Metadata-only `brew update` is allowed when no package mutation is pending;
updev immediately re-runs the Homebrew gate after that refresh and applies
newly discovered safe candidates in the same run while keeping unsafe ones
held.

Pinned mise manifests are a separate update class because `mise upgrade` does
not rewrite fixed versions unless bump mode is requested. updev should inspect
`mise outdated --json --bump --cd <root>` so fixed-version opportunities are
visible in `updev`, `updev list`, `updev last`, and JSON reports without
pretending they were part of the normal provider mutation set. updev uses
mise's JSON `bump` field as the source of truth for bump eligibility. Rows with
`bump: null`, such as `node = "lts"`, stay desired state aliases and are
ignored by the bump gate even when mise also reports a newer `latest` value.
Prefix selectors such as `node = "24"` or `node = "24.16"` follow mise's own
`--bump` semantics instead of being reimplemented by updev.
`[update.mise_bump].mode` controls mutation:

| Mode | Behavior |
|------|----------|
| `off` | Do not add pinned-version opportunities to the normal update/list UX. Explicit future bump commands may still exist. |
| `manual` | Show read-only opportunities and item-scoped confirmed actions only. This is the default. |
| `safe` | Add a confirmed safe-batch action for all currently safe bump candidates. |
| `auto` | During the normal `updev` workflow, automatically apply only currently safe bump candidates after a dry-run preflight. |

Manual, safe-batch, and automatic writes all use scoped provider commands:
`mise upgrade --bump <tool>` for one row or `mise upgrade --bump <tool...>` for
the safe set. updev must not call an unscoped `mise upgrade --bump` from a
routine workflow. Actual writes may add `--yes` after updev has already shown
its own confirmation, so provider prompts cannot freeze the review flow.
Automatic mode keeps mise native `minimum_release_age` and updev-owned
release-age/security checks active; it must not use `MISE_MINIMUM_RELEASE_AGE=0d`
for mutation, and it must skip held, review, blocked, unsupported, opaque,
major-version, or otherwise uncertain rows. Skipped rows remain visible in the
final dashboard/report with their reason and the route to security or manual
review. `UPDEV_MISE_BUMP_MODE` can override the TOML mode for one invocation.
If the dry-run preflight candidate set does not match the planned safe set,
auto mode aborts that bump batch and reports it as review-needed.

When a scoped mise bump includes `npm:*` tools, updev runs that mise command
with a temporary npm user config that preserves registry/auth entries but drops
npm `min-release-age` settings. This avoids npm's `--before` /
`min-release-age` conflict while keeping npm's standalone config unchanged;
mise and updev remain the release-age gate for that provider command.

## List And Inventory Flow

`updev list` is read-only. The TTY default opens the full grouped installed
inventory browser first because listing inventory is the command's primary job.
From that browser, `Tab` / `Shift+Tab` switches directly between installed
inventory and manual apps while preserving each view's cursor, filter, and
expanded rows. Back/Home returns to the full selector menu, where backend
convergence, cached update/security evidence, compact output, and filter views
are one selection away. `updev hub` opens that full selector menu directly for
users who prefer the old all-menu list of views and filters.

Focused flags keep the non-TTY and agent contract deterministic:

```bash
updev list --provider mise
updev list --kind cask
updev list --category runtime
updev list --status attention
updev list --status profile-mismatch
updev list --query git
updev list --limit 20
updev list --details
updev list --format json
```

`--status attention` means error/blocked/held/missing/extra/drift/unavailable
rows. `--status profile-mismatch` isolates inactive-scope drift. `--provider
manual` is opt-in and reads the manual/non-provider inventory bridge,
structured overrides, and read-only `.app` bundle / Mac App Store receipt
evidence without forcing full Homebrew/mise provider collection. When `mas` is
available, `mas list` is used as read-only Mac App Store evidence with a short
timeout. When fresh inventory context is available, matching Homebrew cask
evidence is reconciled into the manual row instead of rendering a duplicate app
entry; otherwise the default-root manual view may use a short Homebrew cask
ownership probe (`brew list --cask -1`). Homebrew cask-owned rows are hidden
from the default manual view to keep review focused on unmanaged apps; use
`--status brew` or `--query <text>` to inspect that cask ownership evidence
explicitly. Apps documented as Homebrew Tap auto-distributed casks, such as
Intel Mac compatibility builds, are treated as Homebrew evidence and normal
`brew/cask` inventory rows, not as manual desired state.
Manual live-only app rows also emit JSON `review_candidates` with stable
`reason_code`, `remediation_code`, `confidence`, evidence, params, and
suggested override fields. Manual evidence preserves source quality fields
where available: `review_url`, `source_url`, `owner`, `managed_by`,
`update_owner`, `ownership_confidence`, and `provider_metadata`. App bundle
rows use `Info.plist` as low-confidence provider metadata, MAS receipt/list
rows use high-confidence App Store ownership metadata, and Homebrew cask rows
use high-confidence Homebrew ownership metadata.

`updev last` / `updev report` inspect the cached last update report without
rerunning providers. On TTY text output, `updev last` opens the same post-update
hub from the cached report, which is useful when reviewing security/manual/
backend details again without running provider updates. Deterministic
non-interactive sections are available with `updev last --plain --section
summary|updates|security|inventory|logs|full`, `--no-interactive`, or
`--format json`. Plain/no-interactive and JSON modes are cache-only: they do
not run backend convergence, manual review, security, translation, or provider
scans to add detail rows. Extra review evidence is loaded asynchronously only
inside the TTY hub after the cached report is already visible. Dry-run update
reports are cached separately as `last-dry-run.json`, so TUI dogfood does not
erase the last real update evidence used by `updev last` and `updev list`.

`updev inventory scan --provider manual` returns the normalized manual/vendor
app evidence used by the manual inventory view without writing desired state.
Text output is a compact evidence listing. JSON output returns
`schema_version`, `status`, `provider`, `summary`, `sections`, and
`review_candidates`. The command returns exit `2` when live-only apps need
review.

`updev inventory plan --provider manual` and `updev inventory check --provider
manual` group manual/vendor app rows into review actions without writing desired
state. Actions include `keep-manual`, `adopt-brew`, `adopt-mas`,
`ignore-local`, `open-vendor`, and `needs-review`. Text output is a human
review table with next steps; JSON output returns action counts, items, review
candidates, `attention_count`, machine-readable reason/remediation codes,
suggested provider, review URL, install hint, and gated command previews. Text
output shows a compact subset by default; use `--action`, `--query`, or
`--limit 0` to narrow or expand it. The command returns exit `2` when any action
still needs review before changing desired state. Command previews are review
aids only; manual inventory output does not execute vendor installers.
`adopt-brew` previews `brew info --cask <name>`. `adopt-mas` previews
`mas lookup <id>` when `mas list` evidence has an id, or `mas search <name>` for
receipt-only evidence.

In the interactive `updev` hub, the manual review plan opens in the shared
detail browser. Expanded rows show provider evidence, suggested overrides,
command previews, and numbered actions. Write actions reuse
`updev inventory review --provider manual --action accept|edit|ignore`, ask for
confirmation before writing, then rebuild the plan view.

`updev inventory review --provider manual` previews review-needed manual app
override entries from current scan evidence. Cached Homebrew cask inventory is
used when available to avoid duplicate manual/cask candidates without forcing a
fresh provider run. The default action writes nothing and returns exit `2` when
live-only apps need review. Text output includes a TOML snippet for the
configured override file, defaulting to
`~/.config/updev/inventory-overrides.toml`; JSON output returns
`schema_version`, `status`, `provider`, `overrides_path`, `candidates`, and
`override_preview`.

`updev inventory review --provider manual --action enrich --query <text>` runs
the configured `[inventory.agent].command` for exactly one matching candidate.
The command receives minimal candidate JSON on stdin and must return TOML
`[[manual.apps]]` entries on stdout. updev parses the output, forces
`review_status = "draft"`, records `source = "agent"`, checks that each draft
matches the selected candidate, then appends it to the first configured TOML
source in `[inventory.manual].sources`. `--action enrich-batch --query <filter>`
uses the same contract for multiple filtered candidates only when
`[inventory.agent].batch = true`; `--limit` caps the batch and updev also keeps
a built-in maximum.

`updev inventory review --provider manual --action accept --query <text>`
appends the selected suggested override to the configured inventory overrides
TOML. `--action edit` opens the generated snippet in `$VISUAL` / `$EDITOR`
before appending it. `--action ignore` appends a `lifecycle = "local-only"`
override so an installed local app stops returning as review-needed without
becoming desired state. Matching existing overrides block duplicate appends.
Use `--action list|update|remove --query <text>` to inspect, edit, or remove
existing override entries. Write actions require the optional `--query` filter
to match exactly one candidate or override unless only one exists.

`updev inventory render --report manual-apps` previews generated manual app
reports from current scan, docs bridge, override evidence, and cached cask
inventory when available. The command writes nothing; text output is Markdown
preview, and JSON output returns `schema_version`, target path, content, and
review candidates.

`updev status` / `updev check` also validate global and project-local mise
manifests (`dot_config/mise/config.toml`, `mise.toml`, and `.mise.toml`).
Entries using `latest`, entries without a `version` field, and non-Node `lts`
entries are reported as blocked `mise` manifest items with file, line, backend,
and reason. `node = "lts"` remains allowed. `updev add --provider mise`
requires an explicit pinned version and refuses `latest`. `updev check
--manifest-only` runs only the cheap manifest hygiene checks and is suitable
for `mise run dot-review`.

`updev fix mise` is the dry-run-first fixer for resolvable `latest` entries. It
uses `mise latest <tool>` as the source of truth, reports the replacement
version and source line, leaves unresolved entries unchanged, and writes only
when `--apply` is provided. The report includes mise `minimum_release_age`
evidence so reviewers can see whether `mise latest <tool>` resolved under a
provider-native release-age policy. The fixer rewrites each affected manifest in
place.

`updev check --dependencies` and `updev doctor dependencies` run read-only
contract checks for local CLIs that updev depends on. Required checks cover
Homebrew and mise version commands plus the JSON shapes used by update safety
and inventory collection. The mise dependency checks also report
`minimum_release_age`, its source, active/inactive status, and whether the
current `mise latest` command shape advertises release-age filtering. Optional
scanner checks cover installed versions of OSV-Scanner, gitleaks, zizmor,
Trivy, Grype, and the optional Codex description-translation backend. Missing
optional integrations are reported as unavailable but do not make the report
fail; changed required JSON contracts return drift. JSON output includes a
`compatibility_ledger` with provider/tool versions, command evidence, support
status, support label, and remediation. `updev doctor dependencies --ledger
<file>` writes the ledger as a local JSON artifact for CI or release review
without posting public issues by default.

On Homebrew 6, doctor also reads `brew trust --json=v1` with
`HOMEBREW_NO_INSTALL_FROM_API=1` and compares it with non-official tap, formula,
and cask entries in the configured `Brewfile.tmpl`. Missing trust is reported as
drift with item-scoped remediation. Security detail views in `updev`, `updev
last`, and `updev list` can run confirmed item-scoped `brew trust --formula` /
`brew trust --cask` actions. updev does not auto-run `brew trust`; whole-tap
trust remains a human security decision and requires its own confirmation.

## Agent Contract

- `--format json` returns complete structured data and never enters TTY.
- Non-TTY text remains readable and deterministic.
- Human text may be shortened or recolored for usability, but JSON field names,
  status values, and report names should remain stable once introduced.
- JSON command strings and flags remain English even when human labels are
  localized.

## Localization And Progress

Human text follows the detected language. Detection order is `UPDEV_LANG`, an
optional `~/.config/updev/config.toml` `[ui].language` when it is not `auto`, macOS
`AppleLanguages` / `AppleLocale`, then `LC_ALL`, `LC_MESSAGES`, and `LANG`.
Japanese environments receive Japanese labels and helper text for human
hub/detail surfaces. JSON remains English.

Machine-readable findings should use stable codes and params rather than
English prose as localization keys. Human output can render those codes through
`internal/i18n` tables or embedded guidance data; provider names, package names,
versions, command strings, and JSON tokens stay untranslated. Keep short UI
labels in Go i18n tables, data-like long guidance in structured embedded
resources, and user-editable configuration in TOML.

`updev list` can maintain a Japanese description cache for provider/tool
descriptions. In Japanese TTY output, `[ui].description_translation = "auto"`
updates missing descriptions with the optional Codex CLI before the list is
printed; `manual` limits translation to explicit `updev list --translate-now` /
`--retranslate-all`, and `off` disables translation attempts. Codex absence is
non-fatal. JSON output and non-TTY text do not trigger automatic translation and
machine-readable fields stay stable.

Slow human-mode startup work uses a delayed TTY-only spinner on stderr for
provider discovery, update safety checks, security scans/reviews, sync inventory
loading, and mutation validation. `[ui].progress = false`,
`UPDEV_PROGRESS=0`, non-TTY output, and JSON output disable progress UI.
Update safety probes use bounded provider calls; Homebrew outdated evidence is
short-TTL cached so repeated reviews do not look frozen when Homebrew is slow.

List table review badges use compact labels such as `▶up 1.7→1.8.1`,
`▶hold`, `▶bak`, `▶sec`, `▶man`, and `▶flt`. Updated rows show the compact
version delta when last-update evidence includes a real version change; symbolic
versions such as `latest`, `nightly`, or `HEAD` collapse to `▶up`.
Held/deferred update evidence and security findings that actually stop a
strict-mode provider update use `hold` instead of the generic update/security
badge. The detail keeps the original decision, for example
`held (decision: review)`, when a review/unknown finding is what stopped the
provider update. These badges come from the latest saved update report, not
from older safety-cache entries; rerun `updev --dry-run --security strict` to
refresh the current hold view.
Multiple row actions
are shown together in priority order, for example `▶sec ▶hold ▶bak`, so
update/security evidence is not hidden behind backend review hints. When common
terminal config files mention a Nerd Font, updev replaces the `▶` marker with
larger per-action emoji markers, such as `✅up 1.7→1.8.1`, `⏸hold`, `🔒sec`,
`📦bak`, `📝man`, and `🔎flt`. Set `UPDEV_NERD_FONT=1` or `NERD_FONT=1` to force
those markers, or `UPDEV_NERD_FONT=0` to force the plain `▶` marker.
