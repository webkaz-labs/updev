# updev

`updev` is a control plane for developer-machine updates, inventory, and
supply-chain review. It keeps Homebrew, mise, and future provider tools native,
but puts one command, one safety gate, and one review queue around machine
changes.

The current public preview is intentionally narrow: macOS, Homebrew, mise, and
manual/vendor app inventory are the supported focus. Linux and Windows binaries
are published for early testing, but broad package-provider support is still
experimental.

![updev workflow overview](docs/assets/readme-workflow.svg)

`updev` is built for machines that already use real provider tools. Providers
still do the installing; `updev` gives you the operator view around them:

- **One operator surface**: preview updates, list installed state, inspect
  drift, and run provider actions without memorizing every provider command.
- **Human-first review**: compact text and TTY selectors keep routine checks
  readable, while detail views and JSON stay available when you need evidence.
- **Safety before mutation**: release-age holds, advisory checks, provenance
  review, and strict mode make risky updates visible before they change the
  machine.
- **Native-provider friendly**: Homebrew and mise stay the source of execution;
  `updev` coordinates, explains, and records decisions around them.
- **Low setup pressure**: built-in defaults work without writing a config file;
  TOML is only needed when you want policy or UI overrides.

## Why updev?

Developer machines drift because each tool has its own update command,
desired-state file, safety model, and review output. `updev` sits above those
providers and keeps maintenance readable whether you run it on demand, on a
schedule, or before/after a machine change:

- run one update/check command instead of remembering provider-specific flows;
- compare desired manifests with live installed state;
- browse package state through a readable `updev list` view with filters,
  details, and JSON for automation;
- identify unmanaged or manually installed apps without immediately adopting
  them;
- add safety gates for risky package, cask, and extension updates;
- recommend provider/backend improvements, such as moving suitable CLI tools
  toward mise when appropriate.

The result is a quieter update workflow: package managers keep their native
behavior, while humans get a consistent report, explicit holds, a focused
review queue, and JSON output for automation.

## What stands out

- `updev list` is designed for scanning: grouped sections, concise status text,
  focused filters, and `--details` when a row needs investigation.
- Japanese terminals can get cached Japanese tool descriptions in `updev list`.
  The cache updates automatically during TTY `updev`/`updev list` when
  Codex is available, and missing Codex never breaks the main workflow.
- Safety gates explain *why* an update is allowed, held, blocked, or needs
  review instead of only hiding it behind a generic failure.
- Manual/vendor app inventory is review-first. It can surface local-only apps,
  cask/Mac App Store ownership evidence, and adoption candidates without
  automatically taking over the machine.
- The same commands work in terminals and automation: TTY-first dashboards for
  humans, `--plain` for stable text logs, and JSON for scripts.
- Configuration is additive. A default-only `config.toml` is not created or
  required, which keeps first-run setup small.

## Features

- **Main update workflow**: `updev` and `updev --dry-run` summarize updates,
  drift, skipped work, warnings, holds, and next actions.
- **Supply-chain safety gates**: hold Homebrew, mise, or extension updates that
  are too new, policy-blocked, advisory-matched, or missing release-age/
  provenance evidence. Strict mode is the default and stops mutation until
  evidence is good enough.
- **Readable inventory**: `updev list` is a first-class review surface for
  desired/live package and runtime state. TTY runs open the grouped inventory
  list first, with drill-down tables grouped by provider, category, and status;
  `Tab` switches to manual app inventory without leaving the browser, and
  `updev hub` opens the review-domain switcher when you want manual/backend/
  update/security/support views first.
- **Desired-state checks**: `updev check`, `updev sync`, and `updev status`
  find missing, extra, drifted, blocked, and unavailable entries.
- **mise hygiene**: `updev fix mise` previews rewrites for unsafe `latest` pins
  while keeping Node `lts` as the only allowed LTS shortcut.
- **Security review**: `updev security scan`, `gate`, `review`, and `policy`
  turn findings into allow/hold/review/block decisions and machine-readable
  evidence.
- **Manual/vendor app inventory**: `updev inventory scan|plan|review
  --provider manual` finds macOS `.app` bundles, Mac App Store evidence,
  Homebrew cask ownership, and local-only apps that need a decision.
- **Interactive or scriptable UX**: `updev`, `updev list`, and `updev last`
  open review dashboards/browsers on TTY; scripts can use `--plain`,
  `--format json`, `--no-color`, and focused filters.

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
mise use -g github:webkaz-labs/updev@vX.Y.Z
updev version
```

This writes a pinned release entry to your global mise config:

```toml
[tools]
"github:webkaz-labs/updev" = "vX.Y.Z"
```

Replace `vX.Y.Z` with the resolved latest release tag. Avoid storing `latest`
for this tool: `updev` treats unpinned mise versions as unsafe in managed
manifests.

To try `updev` without changing your mise config:

```bash
mise x github:webkaz-labs/updev -- updev version
```

You can also install from source with Go:

```bash
go install github.com/webkaz-labs/updev@latest
```

Release archives and checksums are available on
[GitHub Releases](https://github.com/webkaz-labs/updev/releases). Tag-specific
release notes are kept in [`docs/release-notes`](docs/release-notes).

## Provider Assumptions

`updev` does not replace provider CLIs. It shells out to the provider tools you
already use, and provider support assumes those commands are installed,
authenticated where needed, and visible on `PATH`.

Support levels are explicit in v0.7:

| Label | Meaning |
|-------|---------|
| `supported_preview` | Public preview path that is intended to work and receive compatibility fixes before v1. |
| `experimental` | Available for dogfood or early evidence, but not a support promise. |
| `compatibility` | Kept for migration or low-level workflows, not the main human path. |
| `deferred` | Deliberately outside the current release scope. |

Inspect the current catalog with:

```bash
updev support
updev doctor support --format json
```

| Area | Requirement |
|------|-------------|
| macOS preview | Homebrew and mise installed locally. |
| Homebrew provider | `brew` must support JSON output such as `brew outdated --json=v2` and, on Homebrew 6, `brew trust --json=v1`; package mutation still runs through Homebrew. updev uses local tap metadata for safety discovery, reports non-official tap trust gaps, and exposes confirmed item-scoped `brew trust --formula` / `--cask` actions from security details. |
| mise provider | `mise` must support `mise ls --current --json --cd <dir>`, `mise outdated --json --cd <dir>`, and scoped `mise upgrade --minimum-release-age <duration> <tool...>`. `updev` validates exact pins, rejects unsafe `latest` entries, and enforces its own age gate without requiring a global mise-native age setting. |
| Description translation | Optional. `codex` on `PATH` enables Japanese description-cache updates for `updev list`; without it, `updev` keeps running with English descriptions. |
| Manual app inventory | macOS `.app` bundle metadata is read locally; Mac App Store evidence is used only when receipts or `mas` evidence are available. |
| Security gates | Network evidence is best-effort and provider-scoped. Strict mode can hold Homebrew updates, mise updates, and opt-in VS Code extension updates when required evidence is missing. Homebrew 6 trust remains a human security decision; whole-tap trust is confirmation-only and never runs automatically during update. |

Known-good validation for the current release gate:

| Tool | Verified version/path |
|------|-----------------------|
| macOS | primary supported preview platform. |
| Homebrew | `6.0.0-2-g1cd9e81` locally; CI uses mocked provider output. |
| mise | `2026.6.2 macos-x64`; GitHub backend install smoke is covered separately. |
| Go | module and repository mise config use `go1.26.4` for local validation. |
| GitHub Actions | CI uses GitHub-maintained Node 24 action majors; release uses the official GoReleaser action plus GitHub artifact attestations. |

These are validation anchors, not permanent minimum versions. If a newer
provider changes output shape or command behavior, `updev` should mark that
provider/version unsupported until the parser, tests, and docs are updated.

## Quick Start

Preview the workflow safely, then run the main command:

```bash
updev --dry-run
updev
```

Review what needs attention:

```bash
updev list
updev list --status attention
updev list --provider brew --details
updev security review
```

Run consistency checks:

```bash
updev check
updev sync
updev doctor dependencies
updev doctor dependencies --ledger ./updev-compatibility-ledger.json
updev support
updev support --surface provider --format json
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

## Supply-Chain Review

`updev` can evaluate pending updates before mutation. It can hold or require
review for:

- Homebrew releases and VS Code extension updates whose release age is below
  the configured minimum-age threshold;
- mise GitHub-backed entries, explicit and registry `aqua` entries, selected
  core runtimes, high-confidence `npm:` / `cargo:` / `pipx:` entries, and
  data-driven vfox entries whose release/publish/upload age is below the
  configured minimum-age threshold;
- Homebrew casks, URL casks, and non-official taps that need provenance review;
- advisory matches from OSV or GitHub Advisory evidence where package identity
  is reliable;
- unsupported or opaque mise backends that do not expose enough release-age
  evidence for updev to allow automatically;
- VS Code extension updates when marketplace age/posture checks are enabled;
- local policy rules such as temporary allow, hold, review, or block decisions.

Use `warn` mode for visibility and `strict` mode when missing or risky evidence
should hold the update. `strict` is the default. mise inventory, manifest
hygiene, native `minimum_release_age` diagnostics, and pending update
candidates are checked today. A temporary policy allow can unblock an accepted
candidate when strict mode is intentionally holding it.

## Common Commands

| Command | Purpose |
|---------|---------|
| `updev` | Run the update workflow. |
| `updev --dry-run` | Preview the update workflow without applying updates. |
| `updev list` / `updev ls` | Show grouped desired/live inventory. |
| `updev hub` | Open the review-domain switcher for inventory/manual/backend/update/security/support. |
| `updev --plain` / `updev list --plain` | Print stable text output without opening TUI. |
| `updev status` / `updev st` | Show compact current state. |
| `updev check` / `updev ck` | Validate manifests and provider consistency. |
| `updev sync` | Compare desired and live state without mutating. |
| `updev last` | Re-open the cached last update dashboard on TTY. |
| `updev fix mise` | Preview or apply safe mise manifest pin fixes. |
| `updev security scan` | Run explicit broad security checks. |
| `updev security gate` | Evaluate pending update safety. |
| `updev security review` | Inspect findings that require a decision. |
| `updev security policy` | List or edit safety policy overrides. |
| `updev inventory plan --provider manual` | Group manual app decisions. |
| `updev version` / `updev -v` | Print the current CLI version. |

## Configuration

Most users do not need an `updev` config file. Built-in defaults are used when
the file is missing, and `updev` does not create `config.toml` just to write
default values.

Create a config only when you want to change behavior. Normal user policy is
read from:

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

Minimal examples:

```toml
[providers]
include_vscode = true

[update]
security = "strict"

[update.mise_bump]
mode = "manual" # off | manual | safe | auto

[security.mise]
min_release_age_days = 3

[ui]
language = "ja"
description_translation = "manual" # auto | manual | off

[sources]
root = "auto"

[brewfile]
desired = "auto"      # auto | home | root | template | disabled
write_mode = "disabled" # disabled | direct | template | chezmoi-template

[inventory.manual]
sources = ["~/.config/updev/manual-apps.toml"]
markdown_compat = false

[inventory.agent]
enabled = false
command = ["codex", "exec"]
batch = true

[backends]
preference_order = [
  "mise/core",
  "mise/aqua",
  "mise/github",
  "mise/gitlab",
  "mise/conda",
  "mise/pipx",
  "mise/npm",
  "mise/gem",
  "mise/go",
  "mise/cargo",
  "mise/dotnet",
  "store/native",
  "package-manager/native",
  "vendor/manual",
]
```

Public defaults are conservative: `updev` can inspect live evidence and detected
provider manifests without assuming a dotfiles layout or writing desired state.
Use config when a stronger workflow is intentional, such as a specific source
root, Homebrew desired source, manual app inventory source, Markdown
compatibility bridge, or optional agent enrichment. Agent enrichment is
disabled by default; when enabled, generated manual app metadata remains draft
review data until accepted or edited. Structured manual app rows are treated as
desired state only when `review_status = "accepted"`.
Manual review can call a configured agent with `inventory review --action
enrich` or `enrich-batch`; updev validates the returned TOML and writes only
draft entries.
Agent-facing guidance is available from the binary as well as the docs:
`updev skill` prints the installable skill text, `updev skill --full` appends
the detailed workflow guide, and `updev help agent` prints the detailed guide
only.

`mise_bump.mode` controls fixed-version mise updates. `manual` shows safe
item-level bump actions, `safe` adds a confirmed safe-batch action, and `auto`
runs only safe bump candidates during the normal update workflow. Held,
review-needed, unsupported, opaque, and too-new candidates stay visible for
review instead of being bumped automatically. Use `UPDEV_MISE_BUMP_MODE` for a
one-off override without writing a config file.

`description_translation` controls only `updev list` description-cache updates.
`auto` is the built-in default for Japanese TTY output, `manual` runs
translation only when `updev list --translate-now` or `--retranslate-all` is
used, and `off` disables both automatic and explicit translation attempts.
Codex is optional; when it is missing, `updev` leaves English descriptions in
place and `updev doctor dependencies` reports the optional backend as
unavailable.

`preference_order` is optional. Omit it to use the built-in provider/backend
order. When present, labels listed first receive the highest ranks and omitted
known tiers keep their default relative order after them. Future provider labels
can use the same `provider/backend` shape. Deprecated mise backends such as
`mise/ubi` and legacy plugin backends such as `mise/asdf` are recognized when
already present or explicitly configured, but they are not part of the default
recommendation order.

## Support Status

- Supported preview path: macOS with Homebrew and mise.
- Experimental: Linux/Windows binaries, read-only Linux `.desktop` / Flatpak /
  Snap / AppImage inventory evidence, Windows `winget export` inventory
  evidence, broader provider adoption suggestions, and non-macOS provider
  promotion.
- Not managed by `updev`: OS settings, shell/editor configuration, secrets,
  account setup, backups, and device-management policy.

## Development

```bash
go mod verify
scripts/check-go-format.sh
scripts/check-staticcheck.sh
go vet ./...
go test ./...
go build ./...
shellcheck -S warning scripts/*.sh
scripts/check-docs.sh
mise run audit
git diff --check
```

If you use mise, the local task runner wraps the same checks:

```bash
mise install
mise run lint
mise run check
mise run audit
mise run docs-check
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
| [docs/agent/USAGE.md](docs/agent/USAGE.md) | Agent and automation workflow guidance. |

## License

MIT. See [LICENSE](LICENSE).
