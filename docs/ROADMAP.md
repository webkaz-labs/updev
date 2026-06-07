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
- `updev brewfile ...` and `brewfile` remain compatibility or low-level
  surfaces, not the primary human workflow.

## Near-Term Order

1. Close `updev v0.5.7` with the real-terminal hands-on UX/action acceptance
   pass. The implemented baseline includes detailed list/action clarity,
   operable manual review detail rows, preserved provider log newlines,
   dashboard row actions, manual brew/cask suppression, backend apply paths,
   configurable backend preference tiers, scalable `mise/github` inference from
   provider metadata, routed query input, and routed safe write confirmations.
2. Ship `updev v0.5.8` as the TTY performance/streaming bridge before the next
   provider-gate release unless it is explicitly deferred. The target is a
   stable dashboard shell that opens before every domain is complete, refreshes
   update/security/inventory/translation/manual/backend blocks independently,
   preserves provider logs as evidence, and defines cancellation/partial-result
   behavior.
3. Ship `updev v0.6.0` as the first updev-owned mise release-age gate, aligned
   with the Homebrew gate vocabulary and JSON/text evidence model. Add
   `updev skill` / `updev help agent` only after they embed the canonical agent
   guidance files rather than duplicating workflow text, and start focused docs
   drift checks for the highest-risk mirrored surfaces. `broot` and `rtk` remain
   dogfood fixtures for scalable backend resolver behavior, not one-off rules.
4. Continue provider-general inventory after the mise gate work:
   cross-platform fixtures, Linux read-only scanners, provider promotion
   suggestions, and a Windows evidence spike that remains experimental until a
   real Windows runner or machine is available.
5. Continue provider evidence quality with richer source URLs and ownership
   confidence where provider metadata is cheap and reliable.
6. Broaden Homebrew and mise release-age/advisory confidence beyond GitHub and
   first registry paths.
7. Broaden Homebrew release-age and advisory confidence beyond GitHub
   release/tag/ref URL paths.
8. Add provider-native audit paths where package identity is reliable.
9. Continue scanner hardening after OSV-Scanner, gitleaks, zizmor, Trivy, and
   Grype. Keep Syft and Prowler explicit future commands, not default package
   update gates.
10. Add provider contract drift checks for local and CI runs. These checks
   should sample supported provider command shapes, detect upstream CLI/API or
   security-feed schema changes, mark affected provider versions unsupported
   when compatibility is unknown, and open/update documentation and GitHub
   issues for agent follow-up.
11. Add pending-update gates for VS Code and future providers as their update
   flows move into Go, using the same updev-owned gate vocabulary.
12. Add policy ergonomics: guided add/edit/list helpers, diagnostic indexes, and
   shadowed-rule references.
13. Keep agent-assisted review optional for ambiguous candidates.
14. Keep Go CLI standard checks as part of future release reviews; direct
   subprocess exceptions, JSON encoding, and verbosity policy are documented for
   the current tool surface.
15. Maintain the macOS/Homebrew/mise public preview with installation docs,
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
- Extract shared internals only after updev and another maintained tool need
  the same tested API.
- Consider future OS settings providers only after updev provider lessons are
  stable.
