# updev release target

This document tracks the current implemented release and the next release
target. Keep longer-term ordering in [ROADMAP.md](ROADMAP.md), implementation
history in git log, and tag-specific details in
[release-notes](release-notes/).

Version labels use `<tool> vMAJOR.MINOR.PATCH`, while JSON `schema_version`
stays an integer schema contract. `v0.x` releases are public preview releases;
`v1.0.0` is reserved for the first stable public contract.

## Current Release

The current implemented release is `updev v0.6.5`. `updev version`,
`updev --version`, and `updev -v` report this command contract.

`updev v0.6.5` is the full-scope `v0.6.x` consolidation release for the
macOS/Homebrew/mise public preview path. It keeps the `v0.6.0` provider-gate
contract, the `v0.6.2` scalability/export cleanup, the `v0.6.3`
provider-boundary maintenance slice, and the `v0.6.4` shared text UI badge
ownership, then adds the remaining `v0.6` groundwork for embedded agent
guidance, provider compatibility ledgers, and experimental portable inventory
evidence.

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

- [v0.6.5](release-notes/v0.6.5.md): full-scope v0.6.x consolidation release.
- [v0.6.4](release-notes/v0.6.4.md): text UI badge-boundary maintenance patch.
- [v0.6.3](release-notes/v0.6.3.md): provider-boundary maintenance patch.
- [v0.6.2](release-notes/v0.6.2.md): first architecture cleanup/export patch.
- [v0.6.1](release-notes/v0.6.1.md): provider-gate maintenance patch.
- [v0.6.0](release-notes/v0.6.0.md): first updev-owned Homebrew/mise provider
  gate release.

## Current v0.6.5 Scope

`v0.6.5` includes the full remaining `v0.6` scope before the project narrows
toward `v1.0.0`. The stable preview runtime contract remains
macOS/Homebrew/mise-first.

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
   - Includes read-only Linux/manual inventory fixtures and scanner groundwork
     only where it is data-backed and clearly labeled experimental.
   - Keep repository-local or machine-local assumptions behind explicit config,
     fixture, or compatibility flags.
   - Do not claim Linux/Windows provider support as stable in this release.

5. **Agent/manual enrichment safety**
   - Keep optional agent-assisted manual inventory enrichment as structured
     draft metadata only.
   - Batch enrichment may be supported, but agent output must be
     schema-validated, reviewable from CLI/TUI, and safe when Codex or another
     configured agent is unavailable.
   - Avoid scattering generated guidance: canonical agent-facing docs are
     embedded by README/help/skill surfaces instead of duplicated in command
     reference prose.

6. **Docs and public export hygiene**
   - Keep `RELEASE.md` current-state focused. Older release detail belongs in
     `docs/release-notes/<tag>.md` and git log.
   - Keep public export docs free of dotfiles-only assumptions.
   - Extend drift checks only where they protect real maintenance risks:
     release-note presence, README links, command/help snapshots, validation
     parity, and generated/embedded docs matching canonical sources.

7. **Provider-general inventory and cross-platform evidence**
   - Includes provider-general inventory foundations after the mise/Homebrew gate
     contract is stable: cross-platform fixtures, Linux read-only scanners, and
     provider promotion suggestions.
   - Start Linux with data-backed read-only evidence for package/tool/app
     sources such as apt/dpkg, Flatpak, Snap, AppImage, and desktop entries.
   - Keep Windows at fixture/spike level unless a real runner or machine is
     available; do not make Windows support a release promise.

8. **Provider evidence, release-age, and advisory coverage**
   - Broaden Homebrew and mise release-age/advisory confidence beyond GitHub and
     first registry paths.
   - Broaden Homebrew release-age and advisory confidence beyond GitHub
     release/tag/ref URL paths.
   - Uses provider-native audit paths only where package identity is reliable.

9. **Scanner and native-audit hardening**
   - Continue scanner hardening after OSV-Scanner, gitleaks, zizmor, Trivy, and
     Grype.
   - Keep Syft and Prowler explicit future commands until their runtime cost,
     provider mapping, and false-positive handling are proven.
   - Keep scanner output mapped to the same security decision vocabulary used
     by provider gates.

10. **Provider contract drift and compatibility tracking**
    - Provides local/CI detection for provider command/API contract drift.
    - Maintains a provider-version compatibility ledger that can record supported
      versions, failing versions, evidence dates, and remediation notes.
    - Public issue creation is opt-in only and requires documented repository
      ownership, credentials, and posting policy.

11. **Common pending-update gates for future providers**
    - Apply the updev-owned gate model to VS Code and future providers as their
      update flows move into Go.
    - Keep provider-native safety settings as evidence, not as the only source
      of user-visible decisions.
    - Preserve item-scoped allow/hold/review behavior so one risky candidate
      does not unnecessarily block unrelated safe candidates.

12. **Policy ergonomics**
    - Provides guided add/edit/list helpers for policy rules and overrides.
    - Shows diagnostic indexes and shadowed-rule references where policy
      decisions are ambiguous.
    - Keep policy changes reviewable from both CLI and TUI flows.

13. **v1 readiness narrowing**
    - At the end of this release, mark which provider surfaces are stable,
      preview, experimental, or explicitly deferred.
    - Update release notes, public docs, and the v1 readiness checklist so the
      next phase can narrow the stable contract instead of carrying all
      experimental surfaces forward by implication.

### Non-Goals

- automatic external/vendor installer execution;
- stable broad Linux/Windows provider support;
- a stable `v1.0.0` contract;
- dynamic provider plugins;
- default agent execution for manual inventory enrichment;
- unscoped provider-wide writes when item-scoped evidence is required.

### v0.6.5 Release Criteria

- [x] The release note exists under `docs/release-notes/` and represents the
  full remaining `v0.6` consolidation scope, not a narrow cleanup-only patch.
- [x] `updev version`, `docs/CLI.md`, README install/help text, and public
  export metadata agree on `updev v0.6.5`.
- [x] Provider-boundary cleanup keeps command handlers focused on CLI parsing,
  report mutation, rendering, and route/action wiring.
- [x] TTY/report regression coverage protects `updev`, `updev last`, and
  `updev list` dashboard/list/detail navigation, filtered drill-down, focus
  restoration, and stable focused actions.
- [x] Provider-general inventory groundwork has data-backed fixtures or scanners
  for the included non-macOS surfaces and labels them experimental.
- [x] Homebrew/mise release-age and advisory evidence is broader than the
  current GitHub-first paths, or any remaining provider gaps are explicitly
  represented as review decisions with actionable reason codes.
- [x] Scanner/native-audit results map into the shared security decision
  vocabulary and do not introduce separate ad hoc report prose.
- [x] Provider contract drift checks and compatibility ledger updates are
  documented and runnable locally/CI without public posting by default.
- [x] VS Code/future-provider pending-update gate design is reflected in docs
  and any implemented paths use item-scoped decisions.
- [x] Policy rule/override UX is reviewable from CLI and TUI and includes
  diagnostics for shadowed or ambiguous rules.
- [x] Agent/manual enrichment remains optional, schema-validated, and safe when
  no agent command is installed.
- [x] Canonical validation passes before export: `mise run check`,
  `mise run docs-check`, `git diff --check`, and `chezmoi apply --dry-run`.
- [x] Public export to `webkaz-labs/updev` passes `scripts/check-docs.sh`,
  `go test ./...`, `go vet ./...`, and `go mod verify` before tagging.

## Next Release: v1 Readiness Narrowing

The next release after `v0.6.5` should stop expanding preview breadth by
default and instead narrow the public contract:

1. label every provider surface as stable, preview, experimental, or deferred;
2. decide which Linux/Windows fixture-backed paths remain experimental and which
   need real-machine dogfood before further promotion;
3. freeze or rename any command/config/report fields that should become stable
   in `v1.0.0`;
4. keep macOS/Homebrew/mise as the supported preview path unless release
   evidence proves another provider path is ready.

## Later Ordering

Longer-term priorities live in [ROADMAP.md](ROADMAP.md). After `v0.6.5`, later
work should focus on narrowing and hardening:

1. Decide the stable `v1.0.0` macOS/Homebrew/mise contract and explicitly label
   every other provider surface as preview, experimental, or deferred.
2. Promote Linux providers only after fixture coverage and real
   container/VM/machine dogfood prove the read-only scanner and update-gate
   contracts.
3. Promote Windows providers only after a Windows runner or real machine proves
   the fixture/spike assumptions.
4. Extract shared internals only after updev and another maintained tool need
   the same tested API.

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
