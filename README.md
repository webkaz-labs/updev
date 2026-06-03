# updev

`updev` is the package and developer-tool update and inventory command. It owns
Homebrew, mise, global developer tools, package-like apps, update safety gates,
and reproducible package/tool manifests.

`updev` is currently pre-stable. The product direction is a portable CLI, while
public `v1.0.0` readiness is tracked separately from the current implemented
version. The first public stable scope is expected to stay narrow around the
macOS/Homebrew/mise daily workflow before broader providers are promised. A
macOS-only public preview can ship earlier if installation, privacy boundaries,
and experimental provider labels are explicit.

It does not manage OS settings or normal dotfiles. macOS settings live in
[`../macos-settings`](../macos-settings), and file-backed app configuration
stays with chezmoi.

## Common Commands

```bash
updev                         # daily update workflow
updev --dry-run               # preview daily update behavior
updev -h                      # help
updev --config ./updev.toml check --format json
updev --no-color list
updev list                    # inventory hub / grouped package list
updev ls                      # list alias
updev list --provider mise
updev list --status attention
updev last                    # inspect the cached last update report
updev sync                    # read-only desired vs live reconciliation
updev check                   # package/tool consistency checks
updev ck                      # check alias
updev st                      # status alias
updev fix mise                # dry-run rewrite plan for mise latest pins
updev check --dependencies    # local dependency CLI/JSON contract checks
updev doctor dependencies     # focused dependency contract report
updev security scan           # explicit broad security scan
updev security gate           # pending update safety gate
updev version                 # current implemented tool release
updev -v
updev version --format json
```

Human TTY output opens compact review surfaces where useful. Agent and script
paths should use `--format json` or focused flags.

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

Common `config.toml` settings:

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

## Development

```bash
mise -C tools/updev run check
git diff --check
```

Run `chezmoi apply --dry-run` from the repository root when wrapper, config, or
dotfiles integration changes.

## Documentation

| Document | Purpose |
|----------|---------|
| [docs/DESIGN.md](docs/DESIGN.md) | Mission, product boundaries, v1 completion goal. |
| [docs/CLI.md](docs/CLI.md) | Command surface, text/JSON/TUI behavior, localization. |
| [docs/DATA-MODEL.md](docs/DATA-MODEL.md) | Desired state, config, cache, reports, status vocabulary. |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Provider model, package layout, runner/test seams. |
| [docs/SECURITY.md](docs/SECURITY.md) | Security scan/gate/review/policy behavior. |
| [docs/PUBLISHING.md](docs/PUBLISHING.md) | Public preview naming, export, repository creation, and release gate. |
| [docs/ROADMAP.md](docs/ROADMAP.md) | Current state and later target ordering. |
| [docs/RELEASE.md](docs/RELEASE.md) | Active release scope, non-goals, blockers, and release-ready criteria. |
| [docs/VALIDATION.md](docs/VALIDATION.md) | Smoke and regression checklist. |
| [docs/EXTERNAL-MANAGEMENT.md](docs/EXTERNAL-MANAGEMENT.md) | Manual/external app and installer direction. |

