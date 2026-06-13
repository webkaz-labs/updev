# updev architecture

This document holds implementation boundaries and provider structure. Product
mission and completion criteria live in [DESIGN.md](DESIGN.md); command behavior
lives in [CLI.md](CLI.md); data shape lives in [DATA-MODEL.md](DATA-MODEL.md).

## Package Layout

The module root should stay small: `main.go`, `go.mod`, docs, and tool-local
support files such as `mise.toml`. Implementation belongs under `internal/`.

```text
.
  main.go
  mise.toml
  internal/
    cmd/        CLI commands, TTY routing, JSON/text output, cmd-only data
    backend/    backend recommendation reports, preference registry, evidence probes
    provider/   provider interfaces and comparison helpers
    brew/       Homebrew/Brewfile provider plus manifest parsing, metadata, outdated JSON, safety finding, and posture helpers
    githubrepo/ GitHub repository/release URL parsing, release/tag metadata, and posture helpers
    inventoryannotate/ inventory report annotations that combine provider evidence with root/profile policy
    manualinventory/ platform/source scanners and source parsers for manual and external apps
    mise/       mise provider, manifest/outdated JSON helpers, safety finding, registry/provider metadata resolvers, and backend evidence helpers
    nativeaudit/ provider-native audit evidence model, target discovery, summaries, JSON schemas, and parsers
    registryaudit/ package-registry security metadata and posture helpers
    vscode/    VS Code Marketplace metadata and posture helpers
    plan/       updev-specific status/report model
    runner/     subprocess runner and test seam
    snapshot/   manifest snapshots and rollback helpers
    textui/     table, width, color, and non-TTY rendering helpers
    reviewui/   reusable TTY review/detail browser
    updatereason/ update-step reason codes, structured args, compatibility inference, and render-time labels
    securityreason/ security finding reason codes, structured args, compatibility inference, and render-time labels
    securityscanner/ external scanner selection, evidence summaries, and scanner option helpers
    securitygate/ provider gate/finding model, decision helpers, gate finalization, update-safety cache, and registry metadata cache
    securitypolicy/ local security policy JSON schema, store, rule analysis, and matching helpers
    updevpath/  updev root, XDG config/cache/data, policy, and source path resolution
```

Keep new provider-specific code in its provider package when possible. Put
backend/provider recommendation logic in `internal/backend`. Put code in
`internal/cmd/` only when it is command parsing, command-local adapters,
human/JSON rendering, or TTY route/action wiring.
Avoid adding one-helper files to `internal/cmd/`; when a helper is not clearly
command-local, extract it to an internal package with a narrow API. When a
helper is command-local, colocate it with the command domain that owns the data
shape instead of creating another top-level `cmd` file.
Do not solve `cmd` file count by creating nested command subpackages that still
own business logic; Go directories are package boundaries, so useful cleanup is
domain extraction (`securitygate`, `registryaudit`, `reviewui`, `backend`,
`manualinventory`, etc.) followed by thinner command wiring.

## Provider Model

Built-in providers are enough for now; dynamic plugins are not required.
Providers expose stable capabilities rather than pretending every package
manager supports the same operations.

Provider responsibilities:

| Method | Purpose |
|--------|---------|
| `Name` | Stable provider id such as `brew`, `mise`, `npm`, `uv`, `cargo`, `apt`, `flatpak`, `external`. |
| `Supported` | Whether the provider is available on the current OS/profile. |
| `ListLive` / `Live` | Read installed/live state. |
| `ListDesired` / `Desired` | Read desired state from configured manifests. |
| `Validate` | Validate desired definitions where implemented. |
| `Check` | Compare desired vs live state. |
| `Plan` | Build install/remove/update actions without mutating. |
| `Apply` / `Update` | Mutate through provider-native commands after explicit policy/confirmation. |
| `Discover` | Find unmanaged live entries where safe. |
| `Policy` / `SecurityGate` | Evaluate update/install safety before mutation. |
| `Recommend` | Suggest backend/provider improvements, such as Homebrew-to-mise moves. |

Capability flags should keep UX honest: add/remove/apply/update, dry-run,
rollback, unmanaged discovery, privilege requirements, external/manual steps,
update policy, backend recommendation, security scan, and security gate.

Initial providers:

| Provider | OS | Desired source | Notes |
|----------|----|----------------|-------|
| `brew` | macOS | `Brewfile`, `Brewfile.tmpl`, or rendered `~/Brewfile` for the default root | Formulae, casks, taps. VS Code entries are opt-in. Explicit formula detection should avoid treating dependencies as drift. Qualified formula/cask entries such as `owner/tap/name` surface their tap as implicit desired inventory so tap ownership is visible without creating drift. Homebrew default taps are not reported as extra unless explicitly desired. |
| `mise` | macOS/Linux | `mise ls --current --json --cd <dir>` over the source root and project manifest dirs; TOML files for hygiene/fix evidence | Cross-platform runtimes and tools. Native mise config inheritance is authoritative for inventory; raw TOML is used only when file/line/version evidence is required. Prefer exact versions; `latest` is rejected except for the supported Node `lts` shortcut. |
| language globals | macOS/Linux | future `~/.config/updev/manifests/*` | npm/pnpm/bun/uv/cargo/global tool snapshots. |
| `manual` / `external` | macOS/Linux | scan state plus overrides | Manual apps and vendor/external installers; see [EXTERNAL-MANAGEMENT.md](EXTERNAL-MANAGEMENT.md). |
| `apt` / `flatpak` / `winget` | future | platform manifests | Read-only first; unsupported providers are unavailable, not errors. |

## Runner And Tests

Provider-native commands must go through `internal/runner`. Tests should use
fake runners that fix stdout/stderr/exit status. Avoid embedding shell parsing
in providers when structured provider output is available.

Tests that run the real `mise` command while overriding `HOME` must preserve
trust for the repository-local `mise.toml` through `MISE_TRUSTED_CONFIG_PATHS`;
otherwise tool-local config trust can make unrelated tests fail.

Known direct subprocess exceptions:

- `main.go` calls `os.Exit(cmd.Run(...))`; this is the process boundary.
- `runner.Local.RunStreamingWithEnv` is the single runner-backed subprocess
  implementation that provider/scanner code should call through.
- `cmd.runLegacy` delegates to the explicit Python compatibility escape hatch.
- `cmd.runEdit` and `cmd.editManualOverrideBlock` launch the user's editor for
  foreground interactive edit sessions.
- `cmd.translateBatch` launches Codex for description translation; this is an
  explicit agent-assisted side path, not provider state collection.
- `manualinventory.RunAgentCommand` launches the configured local agent command
  for manual inventory metadata enrichment after explicit opt-in; command code
  validates the structured draft before writing anything.
- `cmd.readGlobalDefault` shells out to `defaults` to read macOS global locale
  when environment locale is not enough.
- `cmd.githubTokenFromCLI` may call `gh auth token` as an isolated credential
  retrieval fallback; provider and scanner commands still go through
  `internal/runner`.
- `brewfile.runCommand` and `brewfile.runCommandQuiet` are compatibility wrapper
  internals and remain isolated from the primary Go provider path.

`scripts/check-direct-subprocesses.sh`, called from `scripts/check-docs.sh`,
keeps this exception list in sync with direct `exec.Command` /
`exec.CommandContext` usage. Additions must be deliberate and documented before
the docs check passes.

## TTY And Text UI

`internal/textui` owns ANSI-safe widths, headings, status labels, color, and
non-TTY-safe output. `internal/reviewui` owns reusable browser/detail behavior:
keyboard focus, Back/Home/Exit, `/` filtering, expandable rows, scroll
preservation, action consumption, route state caching, and opt-in mouse modes.

Keep report builders independent from terminal interaction. TTY views consume
reports; they should not be the only place where behavior is computed.

JSON output paths use the shared `cmd.encodeJSON` helper so indentation,
HTML escaping, stderr error reporting, and encoding failure handling stay
consistent across commands. Command handlers still decide the final semantic
exit code after successful encoding.

## Scalability Audit And Refactor Plan

Source-count budgets, package placement rules, and the active refactor ledger
live in [SOURCE-STRUCTURE.md](SOURCE-STRUCTURE.md). Read that file before adding
new files under `internal/` or moving code between packages.

The short version:

- provider-specific evidence, policy, mutation, parsing, and metadata contracts
  belong in provider or security packages, not in command handlers;
- UI-only presentation belongs in the report/action model and renders through
  `textui` or `reviewui`;
- user-facing text should start from stable JSON codes and decisions, with
  localization at the render boundary;
- external commands go through `internal/runner` unless listed in the direct
  subprocess exceptions above.

## Shared Internal Extraction

Do not create a broad shared framework before two tools need the same behavior.
Potential shared packages are `runner`, `platform`, `status`, `plan`,
`snapshot`, `textui`, and future platform helpers.

Extract only when `updev` and another maintained tool have the same tested API
need. Do not share package-manager-specific logic, OS setting catalogs, whole
command trees, localized UI prose, or config schemas.

## Cross-Tool Relation

`updev` can share patterns with adjacent tools without sharing ownership:

- provider/backend boundaries;
- validate/check/plan/apply/rollback flow;
- status vocabulary;
- runner-based subprocess tests;
- JSON contracts;
- TTY helper patterns.

Package managers such as Homebrew, mise, apt, Flatpak, winget, Scoop, and
Chocolatey belong to `updev`. OS/system settings belong to adjacent OS settings
tools. Shell/editor/terminal config files remain outside `updev`.
