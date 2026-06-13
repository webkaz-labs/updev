# updev roadmap

This document tracks long-term ordering and later work. Keep implementation
history in git log, and keep the current/next release target in
[RELEASE.md](RELEASE.md). Design foundations live in [DESIGN.md](DESIGN.md).

## Current State

- Update workflow, grouped inventory, read-only `sync`, guided
  `add/remove/edit`, `rollback`, security v1, and read-only
  `backends doctor/plan` run in Go.
- `updev sync` uses the shared inventory cache by default, supports
  `--refresh`, and reports cache age in human text output.
- Compact update dashboards, `updev last`, list/update selector hubs,
  expandable detail rows, keyboard and opt-in mouse navigation, filters,
  Japanese human labels, provider log streaming, updated/deferred summaries,
  profile mismatch filtering, VS Code opt-in defaults, manual-app guidance, and
  non-destructive smoke coverage are implemented.
- Common TTY review flows use routed dashboard/table/detail/input/confirmation
  views so post-report navigation no longer repeatedly exits and restarts
  separate programs. Manual review and backend convergence evidence can refresh
  asynchronously after the first useful screen is visible.
- TOML policy/config, update safety cache behavior, dependency contract checks,
  backend convergence evidence, mise manifest hygiene/fix, mise native
  `minimum_release_age` diagnostics, and inventory override foundations are
  implemented.
- Manual/vendor app scans, normalized identity reconciliation, review
  candidates, override write actions, manual plan/check decision views, default
  human entry points, gated vendor guidance, generated report previews, and
  structured finding codes are implemented.
- Documentation source-of-truth guidance, canonical `docs/agent/` usage/skill
  files, tag-specific release notes, and focused `docs-check` drift coverage are
  implemented.
- `updev v0.6.4` is the current preview contract: it keeps the `v0.6.0`
  mise/Homebrew provider gate scope, the `v0.6.2` scalability/export cleanup,
  the `v0.6.3` provider-boundary maintenance slice, and shared text UI
  action-badge ownership for grouped dashboard/list/detail views.
- `updev brewfile ...` and `brewfile` remain compatibility or low-level
  surfaces, not the primary human workflow.

## Near-Term Order

1. Keep dogfooding `updev v0.6.4` on the macOS/Homebrew/mise preview path while
   preparing the next narrow v0.6.x cleanup patch. Linux/Windows binaries stay
   experimental.
2. Continue the scalability refactor track in
   [ARCHITECTURE.md](ARCHITECTURE.md#scalability-audit-and-refactor-plan)
   before adding broad provider surfaces. Command handlers should keep shrinking
   only where the target package can own the full domain behavior without
   importing TUI/report types.
3. Add TTY/report regression guardrails for the accepted `updev`, `updev last`,
   and `updev list` flows before making additional UX changes.
4. Add `updev skill` / `updev help agent` only if the canonical `docs/agent/`
   files can be embedded without duplicating command reference in code.
5. Add Linux read-only manual inventory evidence after cross-platform fixtures
   exist. Stable support should remain macOS/Homebrew/mise until real
   Linux/Windows dogfood exists.
6. Add explicit opt-in public-repository issue automation for provider contract
   drift after credentials and repository ownership boundaries are documented.
7. Continue hardening portable structured manual inventory sources and optional
   agent-assisted enrichment: agent output is structured draft metadata, not
   desired state, until the user accepts or edits it from CLI/TUI review.
8. Continue provider-general inventory after the mise gate work:
   cross-platform fixtures, Linux read-only scanners, provider promotion
   suggestions, and a Windows evidence spike that remains experimental until a
   real Windows runner or machine is available.
9. Continue provider evidence quality with richer source URLs and ownership
   confidence where provider metadata is cheap and reliable.
10. Broaden Homebrew and mise release-age/advisory confidence beyond GitHub and
   first registry paths.
11. Broaden Homebrew release-age and advisory confidence beyond GitHub
   release/tag/ref URL paths.
12. Add provider-native audit paths where package identity is reliable.
13. Continue scanner hardening after OSV-Scanner, gitleaks, zizmor, Trivy, and
   Grype. Keep Syft and Prowler explicit future commands, not default package
   update gates.
14. Extend provider contract drift checks beyond local/CI reporting only when
   issue automation has explicit public-repository credentials and opt-in
   posting policy.
15. Add pending-update gates for VS Code and future providers as their update
   flows move into Go, using the same updev-owned gate vocabulary.
16. Add policy ergonomics: guided add/edit/list helpers, diagnostic indexes, and
   shadowed-rule references.
17. Keep agent-assisted review optional for ambiguous candidates. Agent-generated
   manual app metadata must be schema-validated draft data, callable from the
   manual app TUI flow, and safe when Codex or another configured agent is not
   installed.
18. Keep Go CLI standard checks as part of future release reviews; direct
   subprocess exceptions, JSON encoding, and verbosity policy are documented for
   the current tool surface.
19. Maintain the macOS/Homebrew/mise public preview with installation docs,
    privacy boundaries, and explicit experimental status for Linux/Windows
    providers. Reserve `updev v1.0.0` for the first stable public contract after
    that scope is deliberately narrowed and documented.

## Later Targets

- Add external package, Linux, and Windows package/tool providers incrementally
  after the macOS/Homebrew path remains stable. Linux can lead with fixtures and
  container/VM dogfood; Windows waits for at least a runner or real-machine
  validation before it becomes a release promise.
- Add a provider-compatibility ledger generated from local/CI contract checks:
  supported provider versions, last verified command/API shapes, failing
  versions, linked issues, and agent-ready remediation notes.
- Add an agent guidance drift check once `docs/agent/` exists: embedded skill
  output, README discovery text, and CLI help should all point back to the same
  canonical files.
- Add broader documentation drift checks where they are cheap and durable:
  release tags to release-note files, README links, command-help snapshots,
  validation task parity with CI, and generated/embedded docs matching their
  canonical sources.
- Converge every provider on the same updev-owned gate model: provider-native
  safety settings are evidence, while updev reports consistent decisions,
  confidence, remediation, and policy override hooks.
- Publish `updev v1.0.0` only after the readiness checklist in
  [DESIGN.md](DESIGN.md#public-release-readiness) is satisfied.
- Make manual-app and provider-generated reports agent-maintained only after
  the scan/override model is stable.
- Keep Markdown app reports generated from structured sources and live evidence;
  do not make repository-specific prose files the public default source of
  desired state.
- Extract shared internals only after updev and another maintained tool need
  the same tested API.
- Consider future OS settings providers only after updev provider lessons are
  stable.
