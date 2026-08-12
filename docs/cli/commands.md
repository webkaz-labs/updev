# Commands And Defaults

Detailed command, flag, portable-default, backend, and package-apply contract.
Return to the [CLI index](../CLI.md).

## Command Surface

Human-facing commands should stay small:

```bash
updev --config file --no-color ...
updev           # update workflow, then compact review dashboard/selector on TTY
updev update    # explicit default update workflow
updev list      # grouped inventory browser and focused review actions on TTY
updev hub       # domain switcher for inventory/manual/backend/update/security review
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
mise key after confirmation. Homebrew formulae and CLI-only casks are resolved
through one bounded `mise registry --json` snapshot before generic GitHub
inference. A registry recommendation preserves the installed Homebrew version
and becomes directly actionable only after an isolated temporary mise config
can resolve its URL/checksum with
`mise lock --platform <current-platform>`. This catches unsupported platform
metadata that `mise install --dry-run` does not validate.
The plan checks at most 32 registry-backed candidates per run in stable
kind/name order and reports a warning when additional candidates are deferred.
The focused detail action adds that pinned mise desired entry only; it does not
install, uninstall, or remove Brewfile ownership. After mise ownership is
verified, Brewfile ownership removal appears as a separate confirmed action.
`[backends].keep_homebrew` suppresses registry migration for explicit
bootstrap/native/dependency policy exceptions without making those names public
tool defaults.
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
The post-update summary also supports global one-key jumps: `i` opens installed
inventory, `m` opens manual app inventory, `b` opens backend convergence, `s`
opens security review, and `u` opens update logs. The same shortcuts are
available from `updev last` because it reuses the routed update dashboard.
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
`updev list --interactive` installed inventory rows are the primary inventory
browser. Row actions can route the focused item to the relevant review domain
and can execute safe desired-state writes after confirmation, including
category-explicit Homebrew extra adoption into Brewfile when Brewfile mutation
is enabled. `updev hub` is a domain switcher for inventory/manual/backend/
update/security/support views rather than a duplicate provider/kind/status
filter menu; use the list browser search/filter controls or CLI flags for
inventory slicing. The full target contract is tracked in [UX.md](../UX.md).

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
  implemented release contract, currently `updev v0.7.19`. JSON output from
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
  root and inspect provider-native live/config evidence without assuming a fixed
  dotfiles checkout path.
- `UPDEV_ROOT`, `[sources].root`, or `CHEZMOI_SOURCE_DIR` can select a different
  source root. `CHEZMOI_SOURCE_DIR` is kept as compatibility for dotfiles-hosted
  workflows.
- `[brewfile].desired` controls whether Homebrew desired state comes from
  `auto`, `home`, `root`, `template`, or is `disabled`. Write behavior is
  separate and remains controlled by `[brewfile].write_mode`.
- `updev apply brewfile` is the compatibility-named package apply surface. It
  reviews missing resolved Homebrew desired items from the active Brewfile and
  mise `[bootstrap.packages]`, joins each candidate with the package executor
  plan, and applies only gate-approved candidates through one selected
  item-scoped executor. Native Homebrew uses
  `brew tap`, `brew install`, or `brew install --cask`; an available mise
  executor uses one explicit `mise bootstrap packages apply <manager:package>`
  target. Chezmoi Brewfile hooks stay warning-only by default and do not run
  `brew bundle`; use `updev apply brewfile --safe-only
  --dry-run`, `updev check --dependencies`, `updev list --status missing
  --plain`, and `updev update` for daily review.
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
