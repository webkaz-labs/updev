# updev CLI

This document is the stable index for the command and output contract. Product
scope lives in [PRODUCT.md](PRODUCT.md), interactive behavior in [UX.md](UX.md),
and structured state in [DATA-MODEL.md](DATA-MODEL.md).

## Primary Commands

| Command | Human purpose |
|---------|---------------|
| `updev` / `updev update` | Run provider updates, stream provider logs, then open the review dashboard on a TTY. |
| `updev list` | Open grouped installed inventory with item-scoped evidence and actions. |
| `updev last` | Reopen the cached update dashboard or deterministic report sections. |
| `updev hub` | Switch between inventory, manual, backend, update, security, and support domains. |
| `updev status` / `updev check` / `updev plan` | Inspect current state, desired-state drift, and planned work. |
| `updev apply brewfile` | Apply only missing, gate-approved desired packages through item-scoped executors. |
| `updev add` / `remove` / `edit` / `rollback` | Mutate desired manifests through snapshot-backed boundaries. |
| `updev security ...` | Inspect security evidence, gates, review findings, and local policy. |
| `updev backends ...` | Inspect provider/backend convergence recommendations. |
| `updev doctor ...` | Inspect dependency, support, parity, and executor contracts. |
| `updev version` / `support` / `skill` / `help agent` | Inspect the current release, support boundary, and agent guidance. |

Read [commands.md](cli/commands.md) for the complete surface, aliases, flags,
portable defaults, and backend/apply behavior.

## Output Modes

| Context | Contract |
|---------|----------|
| TTY text | May open the dashboard, grouped inventory, or a focused detail browser. |
| `--plain` / `--no-interactive` | Stable human-readable text; never opens a TUI. |
| `--format json` | Deterministic machine-readable output; never opens a TUI. |
| non-TTY / CI | Falls back to deterministic output even when interactive mode was requested. |
| `NO_COLOR=1` / `--no-color` | Preserves content and status semantics without ANSI styling. |

Bare `updev` remains the daily update command, not a read-only hub. Provider
stdout/stderr stays visible before the alternate-screen dashboard opens.

## Global Contract

- `--config <path>` selects normal TOML config for one invocation.
- `--lang en|ja` selects human-facing localization.
- `--no-color` implements standard `NO_COLOR` behavior.
- `--plain`, `--no-interactive`, and `--format json` are the script-safe exits
  from interactive behavior.
- No config file is required when defaults are sufficient.
- Source roots are selected by command flags, `UPDEV_ROOT`, `[sources].root`,
  or the compatibility `CHEZMOI_SOURCE_DIR`; a fixed dotfiles path is never a
  public default.

Exit codes:

| Code | Meaning |
|------|---------|
| `0` | Completed with no blocking drift or finding. |
| `1` | Runtime/provider error. |
| `2` | Read-only drift, held work, or review-needed findings. |
| `64` | Usage, config, flag, or unsupported-option error. |

## Detail Documents

| Document | Read when |
|----------|-----------|
| [commands.md](cli/commands.md) | Adding or changing commands, aliases, flags, portable defaults, backend convergence, or Brewfile apply. |
| [interactive.md](cli/interactive.md) | Changing update/list/last TTY flow, navigation, table/detail composition, or policy actions. |
| [automation.md](cli/automation.md) | Changing agent output, localization, progress, or script-safe behavior. |

Keep this index concise and current. Tag-specific history belongs in
[release notes](release-notes/), not in this contract.
