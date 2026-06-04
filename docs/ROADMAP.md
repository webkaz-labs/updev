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
- TOML policy/config, update safety cache behavior, dependency contract checks,
  backend convergence evidence, mise manifest hygiene/fix, and inventory
  override foundations are implemented.
- Manual/vendor app scans, normalized identity reconciliation, review
  candidates, override write actions, manual plan/check decision views, default
  human entry points, gated vendor guidance, generated report previews, and
  structured finding codes are implemented.
- `updev brewfile ...` and `brewfile` remain compatibility or low-level
  surfaces, not the primary human workflow.

## Near-Term Order

1. Ship `updev v0.6.0` as a Linux-first provider-general inventory slice:
   cross-platform fixtures, Linux read-only scanners, provider promotion
   suggestions, and a Windows evidence spike that remains experimental until a
   real Windows runner or machine is available.
2. Continue provider evidence quality with richer source URLs and ownership
   confidence where provider metadata is cheap and reliable.
3. Broaden Homebrew release-age and advisory confidence beyond GitHub
   release/tag/ref URL paths.
4. Add provider-native audit paths where package identity is reliable.
5. Continue scanner hardening after OSV-Scanner, gitleaks, zizmor, Trivy, and
   Grype. Keep Syft and Prowler explicit future commands, not default package
   update gates.
6. Add pending-update gates for VS Code and future providers as their update
   flows move into Go.
7. Add policy ergonomics: guided add/edit/list helpers, diagnostic indexes, and
   shadowed-rule references.
8. Keep agent-assisted review optional for ambiguous candidates.
9. Keep Go CLI standard checks as part of future release reviews; direct
   subprocess exceptions, JSON encoding, and verbosity policy are documented for
   the current tool surface.
10. Maintain the macOS/Homebrew/mise public preview with installation docs,
    privacy boundaries, and explicit experimental status for Linux/Windows
    providers. Reserve `updev v1.0.0` for the first stable public contract after
    that scope is deliberately narrowed and documented.

## Later Targets

- Add external package, Linux, and Windows package/tool providers incrementally
  after the macOS/Homebrew path remains stable. Linux can lead with fixtures and
  container/VM dogfood; Windows waits for at least a runner or real-machine
  validation before it becomes a release promise.
- Publish `updev v1.0.0` only after the readiness checklist in
  [DESIGN.md](DESIGN.md#public-release-readiness) is satisfied.
- Make manual-app and provider-generated reports agent-maintained only after
  the scan/override model is stable.
- Extract shared internals only after updev and another maintained tool need
  the same tested API.
- Consider future OS settings providers only after updev provider lessons are
  stable.
