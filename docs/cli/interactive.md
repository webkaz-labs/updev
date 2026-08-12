# Interactive Flows

Detailed TTY contract for update, list, inventory, and routed review actions.
Return to the [CLI index](../CLI.md) and use [UX.md](../UX.md) for navigation
state ownership.

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
Homebrew candidate discovery runs `brew outdated --json=v2 --greedy` with
`HOMEBREW_NO_AUTO_UPDATE=1` but keeps Homebrew's API metadata enabled, because
Homebrew 6 can hide many outdated cask candidates when install-from-API is
disabled. Tap trust diagnostics are separate and may still use local tap
metadata where Homebrew exposes a trust-specific JSON contract.
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

Manual, safe-batch, and automatic writes all use version-scoped provider
commands such as `mise upgrade --bump <tool@version>` for one row or
`mise upgrade --bump <tool@version...>` for the safe set. updev must not call an
unscoped `mise upgrade --bump` from a routine workflow. Version-scoped targets
matter when a newer candidate appears after safety review: if `0.7.0` has aged
in and `0.7.1` appears before apply, updev still applies the reviewed `0.7.0`
target instead of drifting to `0.7.1`. Actual writes may add `--yes` after
updev has already shown its own confirmation, so provider prompts cannot freeze
the review flow.
Automatic mode keeps mise native `minimum_release_age` and updev-owned
release-age/security checks active; it must not use `MISE_MINIMUM_RELEASE_AGE=0d`
for mutation, and it must skip held, review, blocked, unsupported, opaque,
major-version, or otherwise uncertain rows. Skipped rows remain visible in the
final dashboard/report with their reason and the route to security or manual
review. `UPDEV_MISE_BUMP_MODE` can override the TOML mode for one invocation.
If a planned candidate is no longer reported by `mise outdated --bump`, auto
mode holds that item instead of guessing. If mise reports a newer candidate for
the same item, auto mode keeps the planned age-reviewed version by passing the
explicit `tool@version` target.

When a scoped mise bump includes `npm:*` tools, updev runs that mise command
with a temporary npm user config that preserves registry/auth entries but drops
npm `min-release-age` settings. This avoids npm's `--before` /
`min-release-age` conflict while keeping npm's standalone config unchanged;
mise and updev remain the release-age gate for that provider command.

## List And Inventory Flow

`updev list` is primarily a review surface. The TTY default opens the full
grouped installed inventory browser first because listing inventory is the
command's primary job, while focused rows may expose explicit confirmed
desired-state writes when the target and category are unambiguous.
Homebrew drift rows derive Brewfile adoption categories from the target
manifest, not from a built-in deployment-scope list. Generic `# updev:
category <name>` markers, compatible template guards such as `has "<category>"
.profiles`, and uncategorized Brewfiles are supported.
From that browser, `Tab` / `Shift+Tab` switches directly between installed
inventory and manual apps while preserving each view's cursor, filter, and
expanded rows. Back/Home returns to the domain switcher, where backend
convergence, cached update/security evidence, compact output, and support
catalog views are one selection away. `updev hub` opens that domain switcher
directly for users who want the review domains before entering the inventory
browser.

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
rows. When Homebrew desired state comes from a rendered `~/Brewfile`, source
entries that exist only in `Brewfile.tmpl` source state are not treated as
active desired state; if the item is installed, it is shown as
`profile-mismatch` rather than as a fresh adoption candidate.
`profile-mismatch` is the compatibility status name for deployment-scope
mismatch: source state contains the entry, but the rendered state for this
machine does not. `--status profile-mismatch` isolates that inactive
source/rendered-scope drift. `--provider
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
and inventory collection. On mise v2026.8.2 or newer they also require the
machine-readable `mise bootstrap status --json` and `mise bootstrap plan
--json` shapes and a successful non-mutating `mise bootstrap packages apply
--dry-run`. They also normalize `mise config ls --json`, require at least one
active config source, and report every active source path.
The aggregate bootstrap status contract also validates and counts normalized
`[bootstrap.packages]` desired records, including rows whose manager is
unavailable on the current platform.
The mise dependency checks also report
`minimum_release_age`, its source, active/inactive status, and whether the
current `mise latest` command shape advertises release-age filtering. Homebrew
diagnostics also show whether the current shell exported the optional
`UPDEV_BREW_WRAPPER` marker and whether `[brewfile].write_mode` enables
updev-managed Brewfile writes. Inactive wrapper/write-boundary diagnostics are
reported as optional unavailable checks so users can understand drift-prevention
coverage without making public-preview installs fail. Optional scanner checks
cover installed versions of OSV-Scanner, gitleaks, zizmor, Trivy, Grype, and
the optional Codex description-translation backend. Missing optional
integrations are reported as unavailable but do not make the report fail;
changed required JSON contracts return drift. JSON output includes a
`compatibility_ledger` with provider/tool versions, command evidence, support
status, support label, and remediation. `updev doctor dependencies --ledger
<file>` writes the ledger as a local JSON artifact for CI or release review
without posting public issues by default.

`updev doctor package-parity` compares the active Brewfile desired source with
the resolved mise `[bootstrap.packages]` projection without writing either
source. Text and JSON reports classify canonical Homebrew formula, cask, and tap
identities as `match`, `brewfile-only`, or `mise-only`; exit `2` means parity
drift. The report preserves unavailable mise manager evidence, counts
out-of-scope package managers separately, and derives tap identity from both
active `[bootstrap.brew.taps]` declarations and qualified Homebrew package
names. Mise v2026.8.2 does not expose tap declarations in bootstrap status JSON,
so updev reads only the config paths already selected by `mise config ls
--json` and parses their tap tables as TOML. This command is diagnostic only:
it does not select an executor, install packages, remove Brewfile entries, or
change canonical ownership.

`updev doctor package-executors` consumes the same snapshot plus package
metadata and reports `native`, `mise`, or `unsupported` for every active
package identity. Use `--format json` for agents/CI, plain text by default, and
`--interactive` for the grouped detail browser. The planner fails closed for
unannotated Brewfile/mise duplicates and unavailable explicit executor
overrides. Brewfile-only rows remain native, macOS x64 Homebrew rows remain
native even if a partial mise manager reports available, and mise selection
requires both a mise desired declaration and current manager capability.
Linux x86_64/arm64 managers are selected from aggregate mise availability; no
host name, home-directory path, or package-name exception affects the result.
This report remains read-only. Exact command planning and package mutation are
owned by the separate `updev apply brewfile` safe-apply boundary.

`updev brewfile desired-source <kind> <name> [<kind> <name> ...]` reports one
ordered `brewfile`, `mise`, `both`, or `none` line per pair. It reads mise's
resolved bootstrap declaration without probing live package-manager state. The
Homebrew shell wrapper batches all targets through this read-only command to
avoid recreating a migrated Brewfile declaration and to reject an implicit
uninstall of mise-owned desired state before mutation.

`updev security scan` and `updev security review` use the same scanner/native
audit evidence vocabulary. JSON includes `unavailable_reason` for unavailable
checks and `error_kind` for failed checks; text output shows the same value in
the compact `issue` column. This distinguishes missing binaries, unsupported
targets, skipped-by-scope audits, timeouts, rate limits, parse failures,
report-unavailable conditions, and command errors without turning optional
missing scanners into update failures.

On Homebrew 6, doctor also reads `brew trust --json=v1` with
`HOMEBREW_NO_INSTALL_FROM_API=1` and compares it with non-official tap, formula,
and cask entries in the configured `Brewfile.tmpl`. Missing trust is reported as
drift with a tap-group list; formula/cask entries already trusted by Homebrew
are not prompted again. Security detail views in `updev`, `updev last`, and
`updev list` can run confirmed item-scoped `brew trust --formula` /
`brew trust --cask` actions. updev does not auto-run `brew trust`; whole-tap
trust remains a human security decision and requires its own confirmation.
