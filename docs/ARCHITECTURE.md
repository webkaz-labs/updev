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
    provider/   provider interfaces and comparison helpers
    brew/       Homebrew and Brewfile provider
    mise/       mise provider
    plan/       updev-specific status/report model
    runner/     subprocess runner and test seam
    snapshot/   manifest snapshots and rollback helpers
    textui/     table, width, color, and non-TTY rendering helpers
    reviewui/   reusable TTY review/detail browser
```

Keep new provider-specific code in its provider package when possible. Put code
in `internal/cmd/` only when it is command parsing, report building,
human/JSON rendering, or a command-local adapter.

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
| `brew` | macOS | `Brewfile`, `Brewfile.tmpl`, or rendered `~/Brewfile` for the default root | Formulae, casks, taps. VS Code entries are opt-in. Explicit formula detection should avoid treating dependencies as drift. Tap drift is explicit-only unless `tap "owner/tap"` is desired. |
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
- `cmd.runLegacy` delegates to the explicit Python compatibility escape hatch.
- `cmd.openEditor` launches the user's editor for a foreground interactive edit
  session.
- `cmd.runTranslateWorker` launches Codex for description translation; this is
  an explicit agent-assisted side path, not provider state collection.
- `cmd.readMacOSLocale` shells out to `defaults` to read macOS global locale
  when environment locale is not enough.
- `cmd.githubToken` may call `gh auth token` as an isolated credential retrieval
  fallback; provider and scanner commands still go through `internal/runner`.
- `brewfile.runTemplate` / `brewfile.runBrewBundleCheck` are compatibility
  wrapper internals and remain isolated from the primary Go provider path.

## TTY And Text UI

`internal/textui` owns ANSI-safe widths, headings, status labels, color, and
non-TTY-safe output. `internal/reviewui` owns reusable browser/detail behavior:
keyboard focus, Back/Home/Exit, `/` filtering, expandable rows, scroll
preservation, and opt-in mouse modes.

Keep report builders independent from terminal interaction. TTY views consume
reports; they should not be the only place where behavior is computed.

JSON output paths use the shared `cmd.encodeJSON` helper so indentation,
HTML escaping, stderr error reporting, and encoding failure handling stay
consistent across commands. Command handlers still decide the final semantic
exit code after successful encoding.

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
