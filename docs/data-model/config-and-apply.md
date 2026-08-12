# Config, Apply, And Status

Config precedence, Brewfile/apply mutation boundaries, and normalized status
vocabulary. Return to the [data-model index](../DATA-MODEL.md).

## Config Files

Normal persistent user settings are optional and live in:

```text
${XDG_CONFIG_HOME:-~/.config}/updev/config.toml
```

When this file is missing, `updev` uses built-in defaults and should not create
the file just to materialize those defaults. Write only non-default user
choices.

Security exception rules remain in:

```text
${XDG_CONFIG_HOME:-~/.config}/updev/security-policy.json
```

Use TOML for normal policy and UI settings that should persist. Keep endpoint
URLs, API tokens, test fixtures, and secrets in environment overrides rather
than TOML.

Example non-default config:

```toml
[providers]
include_vscode = true

[update]
security = "strict"

[update.mise_bump]
mode = "manual" # off | manual | safe | auto

[ui]
language = "ja"
description_translation = "manual"

[inventory]
overrides = "~/.config/updev/manual-overrides.local.toml"

[sources]
root = "auto"

[brewfile]
desired = "auto"
write_mode = "disabled"

[chezmoi_hooks.brewfile]
mode = "warn" # planned: off | warn | apply-safe

[mise_bootstrap]
package_metadata = "~/.config/updev/package-metadata.toml"

[inventory.manual]
sources = ["~/.config/updev/manual-apps.toml"]

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
keep_homebrew = ["brew/chezmoi", "brew/ripgrep", "brew/podman"]

[[inventory.reports]]
name = "manual-apps"
providers = ["manual", "mas", "vendor", "external"]
format = "markdown"
path = "docs/apps.md"
```

The corresponding `UPDEV_*` variables remain temporary overrides for CI, tests,
debugging, and secrets.

`[ui].description_translation` accepts `auto`, `manual`, or `off`. It controls
only the human `updev list` description cache: `auto` may call the optional
Codex CLI in Japanese TTY text mode, `manual` requires explicit translate flags, and
`off` prevents translation attempts.

## Brewfile Apply Bridge

`updev apply brewfile` is the compatibility-named resolved Homebrew
desired-state application surface. It reads both the active Brewfile and active
mise `brew:` / `brew-cask:` bootstrap package declarations.
Chezmoi daily hooks must not call `brew bundle`; they should only detect
rendered `~/Brewfile` changes, run lightweight checks, and point users to updev
review commands.

Semantics:

- the Brewfile portion follows `[brewfile].desired`; active mise package
  declarations are merged by canonical Homebrew identity;
- only missing desired items become item-scoped install candidates;
- extra live items are never uninstalled by this flow;
- outdated updates remain owned by `updev update`;
- every install candidate passes release-age, tap trust, provenance, and local
  security policy gates before mutation when metadata is available;
- every candidate records canonical identity, desired source, selected
  executor, decision, status, reason code, and exact argv when executable;
- safe native items apply through item-scoped `brew tap`, `brew install`, or
  `brew install --cask`; safe mise items use one explicit
  `mise bootstrap packages apply <manager:package>` target;
- review, hold, block, unavailable, and unsupported candidates expose no
  executable argv;
- `brew bundle` remains a compatibility/bootstrap fallback, not the normal
  daily hook path.

Hook mode is:

```toml
[chezmoi_hooks.brewfile]
mode = "warn" # off | warn | apply-safe
```

The default mode is `warn`. `off` suppresses hook guidance. `apply-safe` is
reserved for installations that deliberately opt into hook-triggered safe apply;
the default daily hook remains warning-only.

## Status Vocabulary

Provider output normalizes to shared status tokens:

```text
ok
drift
missing
extra
candidate
review
held
blocked
error
unavailable
```

Keep meanings stable enough that adjacent tools can combine package/tool state
with OS-setting or project-bootstrap state later.
