# updev CLI

This document defines the command and human/agent output contract. Product
mission lives in [DESIGN.md](DESIGN.md); data shape lives in
[DATA-MODEL.md](DATA-MODEL.md).

## Command Surface

Human-facing commands should stay small:

```bash
updev --config file --no-color ...
updev           # daily update workflow, then compact review dashboard/selector on TTY
updev update    # explicit default daily update workflow
updev list      # read-only inventory hub on TTY; rich grouped inventory otherwise
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
updev last      # inspect cached last update report
updev version   # current implemented tool release
updev --version # short version check; -v is an alias
```

Advanced or internal surfaces may exist, but should not be the main human
journey:

```bash
updev brewfile ...
updev doctor dependencies
updev security ...
updev trial ...
```

Backend convergence is an intentional human workflow, not only an advanced
escape hatch. `updev backends plan/doctor` remain the stable non-TTY/JSON entry
points, while the daily `updev` selector and `updev list` TTY flow should offer
a visible route into the same findings when backend recommendations exist.
Backend findings expose `recommended_tier` and `preference_rank`: reliable
`mise` core backends first, registry-backed mise external backends next, then
store/native package evidence, native package managers, and vendor/manual
ownership when those are stronger for the item.

Unlike `macos-settings`, bare `updev` is not a read-only hub. The valuable
existing behavior is the no-argument update command, so preserve it unless the
user explicitly opts into another mode.

Global flags:

- `--config <path>` loads normal TOML policy from a non-default path for this
  invocation. It is equivalent to setting `UPDEV_CONFIG` before running updev.
- `--no-color` disables ANSI color for human text output by setting the
  standard `NO_COLOR` behavior at the CLI boundary.
- `updev v0.5.x` does not add global `--verbose` or `--quiet`. Current diagnostic
  affordances are command-specific `--details`, `updev last --section ...`,
  and `--format json`; add global verbosity only when a concrete cross-command
  diagnostic need appears.
- `updev version`, `updev --version`, and `updev -v` report the current
  implemented release contract, currently `updev v0.5.1`. `updev version
  --format json` includes SemVer parts and the stable/pre-stable contract
  label.
- Read-only aliases are supported for common daily commands: `ls` for `list`,
  `st` for `status`, and `ck` for `check`.

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

Navigation follows the `macset` pattern:

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

`warn` mode may proceed while showing safety gaps. `strict` mode holds mutation
when required evidence is missing, stale, low-confidence, or policy-blocked.
`off` skips mutation gates and must be visible in the report.

## List And Inventory Flow

`updev list` is read-only. The TTY default is an inventory hub: compact provider
summary, attention items, section summaries, and selectable drill-down by
provider/kind/category/status/query/full inventory/manual apps/details/export.

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
evidence without forcing Homebrew/mise live provider collection. When `mas` is
available, `mas list` is used as read-only Mac App Store evidence with a short
timeout. When inventory context is already available, matching Homebrew cask
evidence is reconciled into the manual row instead of rendering a duplicate app
entry.
Manual live-only app rows also emit JSON `review_candidates` with stable
`reason_code`, `remediation_code`, `confidence`, evidence, params, and
suggested override fields.

`updev last` / `updev report` inspect the cached last update report without
rerunning providers. Deterministic sections are `summary`, `updates`,
`security`, `inventory`, `logs`, and `full`.

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

`updev inventory review --provider manual` previews review-needed manual app
override entries from current scan evidence. Cached Homebrew cask inventory is
used when available to avoid duplicate manual/cask candidates without forcing a
fresh provider run. The default action writes nothing and returns exit `2` when
live-only apps need review. Text output includes a TOML snippet for the
configured override file, defaulting to
`~/.config/updev/inventory-overrides.toml`; JSON output returns
`schema_version`, `status`, `provider`, `overrides_path`, `candidates`, and
`override_preview`.

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
when `--apply` is provided. The fixer rewrites each affected manifest in place.

`updev check --dependencies` and `updev doctor dependencies` run read-only
contract checks for local CLIs that updev depends on. Required checks cover
Homebrew and mise version commands plus the JSON shapes used by update safety
and inventory collection. Optional scanner checks cover installed versions of
OSV-Scanner, gitleaks, zizmor, Trivy, and Grype. Missing optional scanners are
reported as unavailable but do not make the report fail; changed required JSON
contracts return drift.

## Agent Contract

- `--format json` returns complete structured data and never enters TTY.
- Non-TTY text remains readable and deterministic.
- Human text may be shortened or recolored for usability, but JSON field names,
  status values, and report names should remain stable once introduced.
- JSON command strings and flags remain English even when human labels are
  localized.

## Localization And Progress

Human text follows the detected language. Detection order is `UPDEV_LANG`,
`~/.config/updev/config.toml` `[ui].language` when it is not `auto`, macOS
`AppleLanguages` / `AppleLocale`, then `LC_ALL`, `LC_MESSAGES`, and `LANG`.
Japanese environments receive Japanese labels and helper text for human
hub/detail surfaces. JSON remains English.

Machine-readable findings should use stable codes and params rather than
English prose as localization keys. Human output can render those codes through
`internal/i18n` tables or embedded guidance data; provider names, package names,
versions, command strings, and JSON tokens stay untranslated. This mirrors the
macset split: short UI labels in Go i18n tables, data-like long guidance in
structured embedded resources, and user-editable configuration in TOML.

Slow human-mode startup work uses a delayed TTY-only spinner on stderr for
provider discovery, update safety checks, security scans/reviews, sync inventory
loading, and mutation validation. `[ui].progress = false`,
`UPDEV_PROGRESS=0`, non-TTY output, and JSON output disable progress UI.
Update safety probes use bounded provider calls; Homebrew outdated evidence is
short-TTL cached so repeated reviews do not look frozen when Homebrew is slow.
