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
- `updev v0.7.6` is the current preview contract: it keeps the `v0.6.x`
  Homebrew/mise provider gate and the `v0.7.3` support-label catalog, includes
  the `v0.7.5` P1 scalability refactor, and fixes support-catalog Japanese help
  text consistency.
- `updev brewfile ...` and `brewfile` remain compatibility or low-level
  surfaces, not the primary human workflow.

## Near-Term Order

1. Dogfood `updev v0.7.6` on the macOS/Homebrew/mise preview path. The current
   policy is fixed: keep macOS/Homebrew/mise as supported preview, Linux/Windows
   and agent enrichment experimental, `brewfile` compatibility oriented, and
   external installer execution / dynamic plugins / stable Linux-Windows update
   providers deferred.
2. Continue the broader `v0.7.x` workstream plan in
   [RELEASE.md](RELEASE.md#next-v07x-workstream-plan) after `v0.7.6`. The order
   remains provider evidence quality, scanner hardening, then policy
   ergonomics. Keep each slice small enough to validate and release as a patch
   if dogfood risk is high.
3. Preserve TTY/report regression guardrails for the accepted `updev`,
   `updev last`, and `updev list` flows before making additional UX changes.
4. Continue `v0.7.x` support-label dogfood without turning the catalog into list
   noise. Provider rows surface `experimental`, `compatibility`, and `deferred`
   when they affect trust or expectations; `supported_preview` remains
   detail-only by default. The support-catalog route from `updev hub` and the
   list hub exposes the provider/command/report/inventory-source catalog with
   surface, label, and query filters.
5. Use `v0.7.x` to decide support labels for provider-general inventory:
   macOS/Homebrew/mise stays supported preview; Linux read-only scanners stay
   experimental until fixture plus container/VM dogfood proves them; Windows
   evidence remains fixture/spike until a real runner or machine is available.
6. Continue provider evidence quality in `v0.7.x` with richer source URLs,
   ownership confidence, broader Homebrew/mise release-age/advisory confidence
   beyond the current registry paths, and provider-native audit paths where
   package identity is reliable.
7. Continue scanner hardening after OSV-Scanner, gitleaks, zizmor, Trivy, and
   Grype. Keep Syft and Prowler explicit future commands, not default package
   update gates.
8. Extend provider contract drift checks beyond the local compatibility ledger
   only when public issue automation has explicit credentials, repository
   ownership, and posting policy.
9. Extend pending-update gates beyond the current Homebrew/mise/VS Code paths
   only as those providers move into Go, using the same updev-owned gate
   vocabulary and item-scoped allow/hold/review decisions.
10. Keep policy ergonomics current as policy rules grow: guided helpers,
   diagnostic indexes, and shadowed-rule references should remain visible from
   CLI and TUI review paths.
11. Keep `updev skill` / `updev help agent` synchronized with canonical
   `docs/agent/` files through docs-check drift coverage.
12. Continue hardening portable structured manual inventory sources and optional
   agent-assisted enrichment: agent output is structured draft metadata, not
   desired state, until the user accepts or edits it from CLI/TUI review.
13. Keep agent-assisted review optional for ambiguous candidates. Agent-generated
   manual app metadata must be schema-validated draft data, callable from the
   manual app TUI flow, and safe when Codex or another configured agent is not
   installed.
14. Keep Go CLI standard checks as part of future release reviews; direct
   subprocess exceptions, JSON encoding, and verbosity policy are documented for
   the current tool surface.
15. Keep release notes and public docs focused on `v0.7.x` preview hardening.
    Do not turn support-level labels into `v1.0.0` promises until the dogfood
    evidence is strong enough to freeze them.
16. Maintain the macOS/Homebrew/mise public preview with installation docs,
    privacy boundaries, and explicit experimental status for Linux/Windows
    providers. Reserve `updev v1.0.0` for the first stable public contract after
    that scope is deliberately narrowed and documented.

## v0.7.x Workstream Sequencing

Use this sequencing when turning the next major axes into issues or release
patches:

| Order | Workstream | First useful slice | Do not do yet |
|-------|------------|--------------------|---------------|
| 1 | Scalability refactor | `v0.7.5`: finish P1 by reducing `internal/cmd` below 50, splitting `manualinventory`, moving shared route state into `reviewui`, and auditing external CLI parser ownership. | Broad framework extraction, nested command subpackages that still own business logic, or provider evidence expansion before structure is unblocked. |
| 2 | Provider evidence quality | Improve Homebrew/mise held/review explanations with source URLs, release dates, cache age, and item-scoped commands. | Promote Linux/Windows or opaque mise/vfox paths based on weak evidence. |
| 3 | Scanner hardening | Make explicit scanner/native-audit evidence more structured and bounded, with clear unavailable semantics. | Make slow or broad scanners part of the default update gate. |
| 4 | Policy ergonomics | Add policy diagnostics and guided edit/renew/narrow flows where users already review security details. | Auto-post public issues or create broad permanent allow rules without explicit user intent. |

Every workstream must preserve the same invariants:

- provider logs remain visible outside alternate-screen TUI;
- cached reports, JSON/text, and TUI detail views agree on decisions and
  reasons;
- item-scoped allow/hold/review is preserved whenever the provider supports it;
- support labels remain preview boundaries, not `v1.0.0` promises;
- docs stay current-state focused and release notes carry tag-specific history.

## Later Targets

- Add external package, Linux, and Windows package/tool providers incrementally
  after the macOS/Homebrew path remains stable. Linux can lead with fixtures and
  container/VM dogfood; Windows waits for at least a runner or real-machine
  validation before it becomes a release promise.
- Extend the provider-compatibility ledger only after the local/CI contract
  checks need additional fields such as linked public issues or agent-ready
  remediation notes.
- Keep agent guidance drift checks current: embedded skill output, README
  discovery text, and CLI help should all point back to the same canonical
  files.
- Add broader documentation drift checks where they are cheap and durable:
  release tags to release-note files, README links, command-help snapshots,
  validation task parity with CI, and generated/embedded docs matching their
  canonical sources.
- Converge every provider on the same updev-owned gate model: provider-native
  safety settings are evidence, while updev reports consistent decisions,
  confidence, remediation, and policy override hooks.
- Publish `updev v1.0.0` only after `v0.7.x` dogfood and the readiness
  checklist in [DESIGN.md](DESIGN.md#public-release-readiness) are satisfied.
- Make manual-app and provider-generated reports agent-maintained only after
  the scan/override model is stable.
- Keep Markdown app reports generated from structured sources and live evidence;
  do not make repository-specific prose files the public default source of
  desired state.
- Extract shared internals only after updev and another maintained tool need
  the same tested API.
- Consider future OS settings providers only after updev provider lessons are
  stable.
