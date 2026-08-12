# Automation And Localization

Agent-safe output, localization, and progress behavior. Return to the
[CLI index](../CLI.md).

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
