# updev release target

This document tracks the current implemented release and the next release
target. Keep longer-term ordering in [ROADMAP.md](ROADMAP.md), implementation
history in git log, and tag-specific details in
[release-notes](release-notes/).

Version labels use `<tool> vMAJOR.MINOR.PATCH`, while JSON `schema_version`
stays an integer schema contract. `v0.x` releases are public preview releases;
`v1.0.0` is reserved for the first stable public contract.

## Current Release

The current implemented release is `updev v0.6.4`. `updev version`,
`updev --version`, and `updev -v` report this command contract.

`updev v0.6.4` is a public preview maintenance release for the
macOS/Homebrew/mise path. It keeps the `v0.6.0` provider-gate contract, the
`v0.6.2` scalability/export cleanup, and the `v0.6.3` provider-boundary
maintenance unchanged, then moves shared action-badge presentation out of
`internal/reviewui` and into `internal/textui` so dashboard/list/detail markers
share one width/color/icon implementation.

Current support promise:

- macOS/Homebrew/mise is the supported preview path.
- Linux and Windows binaries are published, but provider support outside the
  macOS/Homebrew/mise path remains experimental until fixture and real-machine
  dogfood prove the contract.
- Provider command execution stays outside the alternate-screen TUI so brew and
  mise logs remain visible.
- TTY dashboards and detail browsers are review surfaces over structured
  reports; behavior must also be represented in cached reports and JSON/text
  output where relevant.

Released patch notes:

- [v0.6.4](release-notes/v0.6.4.md): text UI badge-boundary maintenance patch.
- [v0.6.3](release-notes/v0.6.3.md): provider-boundary maintenance patch.
- [v0.6.2](release-notes/v0.6.2.md): first architecture cleanup/export patch.
- [v0.6.1](release-notes/v0.6.1.md): provider-gate maintenance patch.
- [v0.6.0](release-notes/v0.6.0.md): first updev-owned Homebrew/mise provider
  gate release.

## Next Release: v0.6.x

The next `v0.6.x` patch should stay on the same cleanup-and-groundwork line
before any broad provider expansion. The goal is to make the public preview
maintain, easier to test, and harder to regress while keeping the runtime
contract stable.

### Scope

1. **Provider-boundary cleanup P2**
   - Continue shrinking `internal/cmd` only where the target package can own the
     full domain behavior without importing TUI/report types.
   - Prefer provider packages for provider command contracts, parsing, safety
     evidence, and metadata resolvers.
   - Keep command handlers focused on CLI parsing, report mutation, rendering,
     and route/action wiring.

2. **Security/update evidence model hardening**
   - Keep the common gate model, update-safety cache, and report vocabulary in
     `internal/securitygate` and stable reason-code packages.
   - Do not add provider-specific ad hoc prose or one-off tool branches in
     command code.
   - Preserve candidate-scoped strict updates: safe candidates can proceed while
     held/review candidates remain item-visible.

3. **TTY/report UX regression guardrails**
   - Preserve the accepted `updev`, `updev last`, and `updev list` navigation
     contract: dashboard rows route to filtered details, Back/Home restore the
     previous view, and focused actions stay stable.
   - Keep grouped high-information lists for inventory/manual/backend/security
     views, with compact badges and expanded evidence/actions.
   - Avoid TUI-only behavior that cannot be reached from cached report data.

4. **Portable inventory groundwork**
   - Add read-only Linux/manual inventory fixtures or scanner groundwork only
     when it is data-backed and clearly labeled experimental.
   - Keep repository-local or machine-local assumptions behind explicit config,
     fixture, or compatibility flags.
   - Do not claim Linux/Windows provider support as stable in this release.

5. **Agent/manual enrichment safety**
   - Keep optional agent-assisted manual inventory enrichment as structured
     draft metadata only.
   - Batch enrichment may be supported, but agent output must be
     schema-validated, reviewable from CLI/TUI, and safe when Codex or another
     configured agent is unavailable.
   - Avoid scattering generated guidance: canonical agent-facing docs should be
     reusable by README/help/skill surfaces before adding `updev skill` or
     `updev help agent`.

6. **Docs and public export hygiene**
   - Keep `RELEASE.md` current-state focused. Older release detail belongs in
     `docs/release-notes/<tag>.md` and git log.
   - Keep public export docs free of dotfiles-only assumptions.
   - Extend drift checks only where they protect real maintenance risks:
     release-note presence, README links, command/help snapshots, validation
     parity, and generated/embedded docs matching canonical sources.

### Non-Goals

- automatic external/vendor installer execution;
- broad Linux/Windows provider support;
- a stable `v1.0.0` contract;
- dynamic provider plugins;
- default agent execution for manual inventory enrichment;
- unscoped provider-wide writes when item-scoped evidence is required.

### v0.6.4 Release Criteria

- [x] `updev version`, `updev --version`, and `updev -v` report
  `updev v0.6.4`.
- [x] `docs/release-notes/v0.6.4.md` exists and describes the
  patch as maintenance/groundwork unless the scope deliberately changes.
- [x] `docs/CLI.md` current-version text matches `updev v0.6.4`.
- [x] `docs/SOURCE-STRUCTURE.md` records any package count or
  placement-rule changes introduced by the release.
- [x] Action badge rendering, width truncation, stable priority,
  status-aware coloring, and Nerd Font marker detection are owned by
  `internal/textui` and covered by package tests.
- [x] `internal/reviewui` adapts row actions into text UI badge inputs without
  owning badge presentation rules.
- [x] The accepted grouped list/detail badge behavior remains covered by
  review UI tests.
- [x] No new provider evidence path, Linux/Windows/manual scanner behavior, or
  runtime provider support promise is introduced by this patch.
- [x] Canonical validation passes before export: `mise run check`,
  `mise run docs-check`, `git diff --check`, and `chezmoi apply --dry-run`.
- [x] Public export to `webkaz-labs/updev` passes `scripts/check-docs.sh`,
  `go test ./...`, `go vet ./...`, and `go mod verify` before tagging.

## Later Ordering

Longer-term priorities live in [ROADMAP.md](ROADMAP.md). The short version:

1. Continue provider-general inventory after the v0.6.x cleanup line with
   deeper read-only Linux/Windows scanners.
2. Broaden Homebrew and mise release-age/advisory confidence beyond GitHub and
   first registry paths.
3. Apply the common updev-owned gate model to VS Code and future providers.
4. Add provider-native audit paths where package identity is reliable.
5. Continue scanner hardening after OSV-Scanner, gitleaks, zizmor, Trivy, and
   Grype.
6. Prepare for public `updev v1.0.0` only after the stable macOS/Homebrew/mise
   scope is deliberately narrowed and documented.

## Public Preview Maintenance

The public preview should stay narrow and incremental:

1. **macOS-first support** remains the stable preview path while Linux/Windows
   binaries and provider expansion are clearly labeled experimental.
2. **Pre-v1 hardening** freezes the stable command/config/JSON surface for the
   macOS/Homebrew/mise update workflow, labels experimental provider surfaces,
   removes or clearly hides compatibility-only paths, and keeps compatibility
   tests for exit codes, JSON shape, no-color/non-TTY output, and config
   parsing.
3. **Distribution maintenance** keeps the mise GitHub backend install path,
   release binaries, checksums, source install path, and
   `docs/release-notes/<tag>.md` in sync. The tag workflow uses that note as the
   GitHub Release body and updates existing release notes on reruns. The
   repository-local `mise.toml` pins the Go toolchain and `mise run check`
   mirrors the public CI's module verify, vet, test, and build gates.
4. **`updev v1.0.0`** is the first stable public release. Its stable promise is
   macOS-first package/tool orchestration for Homebrew, Brewfile-derived
   desired state, mise, focused security gates, inventory, sync,
   add/remove/edit/rollback, and documented JSON reports. Manual/vendor app
   inventory may remain opt-in unless it reaches the same stability bar.
