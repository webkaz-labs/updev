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
    brew/       Homebrew and Brewfile provider
    mise/       mise provider
    plan/       updev-specific status/report model
    runner/     subprocess runner and test seam
    snapshot/   manifest snapshots and rollback helpers
    textui/     table, width, color, and non-TTY rendering helpers
    reviewui/   reusable TTY review/detail browser
    securitygate/ provider gate, finding, summary, and update-safety cache model
    updevpath/  updev root, XDG config/cache, policy, and source path resolution
```

Keep new provider-specific code in its provider package when possible. Put
backend/provider recommendation logic in `internal/backend`. Put code in
`internal/cmd/` only when it is command parsing, command-local adapters,
human/JSON rendering, or TTY route/action wiring.

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
- `cmd.runManualAgentCommand` launches the configured local agent command for
  manual inventory metadata enrichment after explicit opt-in; it validates the
  structured draft before writing anything.
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
preservation, and opt-in mouse modes.

Keep report builders independent from terminal interaction. TTY views consume
reports; they should not be the only place where behavior is computed.

JSON output paths use the shared `cmd.encodeJSON` helper so indentation,
HTML escaping, stderr error reporting, and encoding failure handling stay
consistent across commands. Command handlers still decide the final semantic
exit code after successful encoding.

## Scalability Audit And Refactor Plan

Before adding broad provider surfaces, review new work against these placement
questions:

- Is this provider-specific evidence, policy, or mutation logic? Keep it in the
  provider package or a provider-oriented engine, not in the command handler.
- Is this UI-only presentation? Keep behavior in the report/action model and
  render it from `textui` or `reviewui`.
- Is this curated policy data? Put it behind an explicit data/config-backed
  registry with source evidence and tests.
- Is this path, environment, OS, or profile resolution? Centralize it behind a
  testable config/path helper and avoid implicit dotfiles-only defaults.
- Is this an external command? Use `internal/runner` unless it is one of the
  documented interactive or credential exceptions.
- Is this user-facing text? Keep stable JSON codes and decisions first; localize
  and shorten human labels at the render boundary.

Current scalability risks and planned responses:

| Area | Risk | Plan | Priority |
|------|------|------|----------|
| `internal/cmd` size | Command files can mix CLI parsing, report building, provider evidence, TUI routing, and action execution. | Keep backend recommendation reports in `internal/backend`; continue extracting action services, manual inventory, and security gate engines so `cmd` mostly assembles commands and views. | P1 |
| Backend recommendations | Curated backend preference seeds can become opaque as ecosystems broaden. | Keep tool-specific seeds in the backend registry or a provider-metadata resolver with source evidence. Registry rules must carry source evidence that is surfaced in JSON/detail views and covered by tests. New one-off mappings require a registry entry and tests, not an inline `case`. | P1 |
| Direct subprocesses | A direct `exec.Command` can bypass fakes, logs, policy, and test seams. | Keep direct subprocesses only in the documented exception list. Add periodic grep/docs-check coverage so new direct calls are reviewed. TUI actions that mutate state should call runner-backed services. | P1 |
| OS/path defaults | macOS paths, XDG/Home, source-root, and repo-local markdown compatibility can become environment assumptions. | Keep root/config/cache/policy/source resolution in `internal/updevpath`. Continue moving provider/platform-specific paths behind explicit helpers or scanner contracts. Register OS scanners per platform. Keep repo-local markdown as explicit compatibility input, never an implicit public default. | P1 |
| Security gates | Provider switches and feed parsing can grow into another command-local matrix. | Keep the common gate/finding/summary report model and update-safety cache store in `internal/securitygate`. Move provider-specific release-age/advisory/native-audit evidence into provider or security subpackages. mise vfox/asdf-style ecosystems use data-driven provider metadata entries (`provider identity`, resolver type, bounded source URL, parser contract) instead of tool-name branches. Add contract drift checks for provider CLI/API/schema changes. | P1 |
| TUI routing | `updev`, `last`, `list`, manual review, and backend review can diverge in route handling and back-stack behavior. | Keep shared navigation, action focus, scroll preservation, and async refresh primitives in `reviewui`; command code supplies sections, rows, and action handlers. | P2 |
| Width and localization | Hand-computed widths or embedded translated prose can regress tables and JSON contracts. | Keep display width in `textui`; keep recurring reason/status strings as stable codes plus render-time labels. | P1 |
| Test structure | Large test files hide fixture duplication and make targeted runs slower. | Split tests by policy/scanner/native-audit/update/list/router/mise-bump domains and share fixture builders. Prefer focused unit tests before TTY acceptance tests. | P2 |
| Cache/report schema | Caches can accidentally store final decisions instead of reusable evidence. | Document cache ownership and invalidation in the data model. Store raw provider evidence plus decision inputs; recompute decisions when policy changes. | P2 |

Execution order:

1. Freeze the placement rules above and use them in release reviews.
2. Move reusable TTY action and text primitives into `reviewui`/`textui` before
   adding more interactive screens.
3. Expand the backend registry into data-backed entries with source evidence.
4. Extract provider-specific security gate implementations.
5. Split manual inventory scanners and enrichment into platform/source packages.
6. Split large tests and add direct-subprocess/provider-contract drift checks.

Do not add new tool-name-only fixes, direct provider command calls, implicit
repository-local defaults, or TUI-only behavior that is missing from the report
model.

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
