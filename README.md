# updev

`updev` is a developer-machine update and inventory CLI. It gives one daily
entry point for package managers and tool runtimes, then explains what changed,
what is unmanaged, and what needs review.

The current public preview is intentionally narrow: macOS, Homebrew, mise, and
manual/vendor app inventory are the supported focus. Linux and Windows binaries
are published for early testing, but broad package-provider support is still
experimental.

## Why updev?

Developer machines often drift because each tool has its own update command,
desired-state file, safety model, and review output. `updev` sits above those
providers and keeps the daily workflow readable:

- run one update/check command instead of remembering provider-specific flows;
- compare desired manifests with live installed state;
- show compact human output for daily use and JSON for automation;
- identify unmanaged or manually installed apps without immediately adopting
  them;
- add safety gates for risky package, cask, and extension updates;
- recommend provider/backend improvements, such as moving suitable CLI tools
  toward mise when appropriate.

`updev` does not replace Homebrew, mise, or other package managers. Provider
tools still perform installation and updates; `updev` orchestrates, validates,
summarizes, and records reviewable decisions.

## Features

- **Daily dashboard**: `updev` and `updev --dry-run` summarize updates, drift,
  skipped work, warnings, and next actions.
- **Grouped inventory**: `updev list` shows desired/live package and runtime
  state by provider, category, and status.
- **Desired-state checks**: `updev check`, `updev sync`, and `updev status`
  find missing, extra, drifted, blocked, and unavailable entries.
- **mise hygiene**: `updev fix mise` previews rewrites for unsafe `latest` pins
  while keeping Node `lts` as the only allowed LTS shortcut.
- **Security review**: `updev security scan`, `gate`, `review`, and `policy`
  provide explicit safety decisions and machine-readable findings.
- **Manual/vendor app inventory**: `updev inventory scan|plan|review
  --provider manual` finds macOS `.app` bundles, Mac App Store evidence,
  Homebrew cask ownership, and local-only apps that need a decision.
- **Interactive or scriptable UX**: TTY runs can open compact selectors and
  detail views; scripts can use `--format json`, `--no-color`, and focused
  filters.

## Install

Quick shell install:

```bash
curl -fsSL https://raw.githubusercontent.com/webkaz-labs/updev/main/scripts/install.sh | sh
```

This installs the latest GitHub release to `~/.local/bin/updev` and verifies the
release checksum before copying the binary. To pin a release or choose another
directory:

```bash
curl -fsSL https://raw.githubusercontent.com/webkaz-labs/updev/main/scripts/install.sh |
  sh -s -- --version vX.Y.Z --install-dir ~/.local/bin
```

Managed with mise:

```bash
mise use -g github:webkaz-labs/updev
updev version
```

This writes the following entry to your global mise config:

```toml
[tools]
"github:webkaz-labs/updev" = "latest"
```

To pin a specific release, add a tag such as
`github:webkaz-labs/updev@vX.Y.Z`.

To try `updev` without changing your mise config:

```bash
mise x github:webkaz-labs/updev -- updev version
```

You can also install from source with Go:

```bash
go install github.com/webkaz-labs/updev@latest
```

Release archives and checksums are available on
[GitHub Releases](https://github.com/webkaz-labs/updev/releases).

## Quick Start

Preview the daily workflow:

```bash
updev --dry-run
```

Inspect inventory:

```bash
updev list
updev list --status attention
updev list --provider mise
```

Run consistency checks:

```bash
updev check
updev sync
updev doctor dependencies
```

Review manual/vendor macOS apps:

```bash
updev inventory scan --provider manual
updev inventory plan --provider manual
updev inventory review --provider manual
```

Use JSON for automation:

```bash
updev list --format json
updev security gate --format json
updev inventory plan --provider manual --format json
```

## Common Commands

| Command | Purpose |
|---------|---------|
| `updev` | Run the daily update workflow. |
| `updev --dry-run` | Preview the daily workflow without applying updates. |
| `updev list` / `updev ls` | Show grouped desired/live inventory. |
| `updev status` / `updev st` | Show compact current state. |
| `updev check` / `updev ck` | Validate manifests and provider consistency. |
| `updev sync` | Compare desired and live state without mutating. |
| `updev last` | Re-open the cached last update report. |
| `updev fix mise` | Preview or apply safe mise manifest pin fixes. |
| `updev security scan` | Run explicit broad security checks. |
| `updev security gate` | Evaluate pending update safety. |
| `updev security review` | Inspect findings that require a decision. |
| `updev security policy` | List or edit safety policy overrides. |
| `updev inventory plan --provider manual` | Group manual app decisions. |
| `updev version` / `updev -v` | Print the current CLI version. |

## Configuration

Normal user policy lives in:

```text
${XDG_CONFIG_HOME:-~/.config}/updev/config.toml
```

Security exception rules live in:

```text
${XDG_CONFIG_HOME:-~/.config}/updev/security-policy.json
```

Environment variables remain one-off overrides for CI, tests, debugging,
endpoints, and secrets. `--config <path>` is the CLI equivalent of
`UPDEV_CONFIG` for one command. Do not put tokens or API secrets in TOML.

Example `config.toml`:

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
language = "auto"      # auto | en | ja
interactive = "auto"   # auto | on | off
progress = true

[inventory]
state_dir = "~/.local/state/updev/inventory"
overrides = "~/.config/updev/inventory-overrides.toml"
```

## Support Status

- Supported preview path: macOS with Homebrew and mise.
- Experimental: Linux/Windows binaries, future Linux package scanners, broader
  provider adoption suggestions, and Windows package evidence.
- Not managed by `updev`: OS settings, shell/editor configuration, secrets,
  account setup, backups, and device-management policy.

## Development

```bash
go mod verify
go vet ./...
go test ./...
go build ./...
git diff --check
```

If you use mise, the local task runner wraps the same checks:

```bash
mise run check
```

## Documentation

| Document | Purpose |
|----------|---------|
| [docs/DESIGN.md](docs/DESIGN.md) | Mission, product boundaries, v1 completion goal. |
| [docs/CLI.md](docs/CLI.md) | Command surface, text/JSON/TUI behavior, localization. |
| [docs/DATA-MODEL.md](docs/DATA-MODEL.md) | Desired state, config, cache, reports, status vocabulary. |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Provider model, package layout, runner/test seams. |
| [docs/SECURITY.md](docs/SECURITY.md) | Security scan/gate/review/policy behavior. |
| [docs/ROADMAP.md](docs/ROADMAP.md) | Current state and later target ordering. |
| [docs/RELEASE.md](docs/RELEASE.md) | Active release scope, non-goals, blockers, and release-ready criteria. |
| [docs/EXTERNAL-MANAGEMENT.md](docs/EXTERNAL-MANAGEMENT.md) | Manual/external app and installer direction. |
