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
  inactive deployment-scope mismatch filtering, VS Code opt-in defaults, manual-app guidance, and
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
- `updev v0.7.16` is the current preview contract: it keeps the `v0.6.x`
  Homebrew/mise provider gate and the `v0.7.3` support-label catalog, includes
  the `v0.7.5` P1 scalability refactor, the `v0.7.8` large command-package
  reset, the `v0.7.9` agent-friendly quality tooling patch, and the `v0.7.10`
  provider evidence detail patch, the `v0.7.11` scanner hardening patch, and
  the `v0.7.12` policy ergonomics patch, the `v0.7.13` route-to-intent
  completion and navigation cleanup patch, the `v0.7.14` post-route polish
  patch, the `v0.7.15` drift-prevention and rendered Brewfile sync patch, and
  the `v0.7.16` Brewfile hook/apply bridge patch. It
  keeps `check` fast, adds slower `audit` evidence for release/scheduled
  reviews, tracks non-blocking `aislop` findings in `SOURCE-STRUCTURE.md`,
  shows richer Homebrew/mise source, release-age, and cache context in detail
  views, and classifies scanner/native-audit unavailable/error states with
  structured issue fields. Policy diagnostics now identify expired, invalid,
  duplicate, shadowed, broad, missing-reason, and missing-expiry rules, and TUI
  row actions route to item-scoped detail views when a stable target exists,
  redundant inventory attention/detail choices stay out of the primary hubs,
  and Homebrew extra live inventory rows explain drift causes plus offer
  category-explicit Brewfile adoption through the mutation boundary. Homebrew
  wrapper diagnostics, trust handling, rendered Brewfile sync,
  deployment-scope mismatch filtering, warning-only daily hooks, and safe
  item-scoped `updev apply brewfile` installs are part of the current preview
  contract.
- `updev brewfile ...` and `brewfile` remain compatibility or low-level
  surfaces, not the primary human workflow.
- Chezmoi Brewfile onchange hooks are warning-only daily plumbing. They do not
  run `brew bundle`; first-time bootstrap remains an explicit bootstrap task,
  and normal missing Homebrew desired-state application uses `updev apply
  brewfile`.

## Near-Term Order

1. Continue the broader `v0.7.x` workstream plan in
   [RELEASE.md](RELEASE.md#next-release-target-v0717). `v0.7.17` follows the
   v0.7.16 Brewfile hook/apply bridge with real TTY dogfood, route/readability
   polish, and a decision on whether hook-triggered `apply-safe` should remain
   documented-only or become explicit opt-in behavior.
2. Preserve TTY/report regression guardrails for the accepted `updev`,
   `updev last`, and `updev list` flows before making additional UX changes.
3. Continue `v0.7.x` support-label dogfood without turning the catalog into list
   noise. Provider rows surface `experimental`, `compatibility`, and `deferred`
   when they affect trust or expectations; `supported_preview` remains
   detail-only by default. The support-catalog route from `updev hub` and the
   list hub exposes the provider/command/report/inventory-source catalog with
   surface, label, and query filters.
4. Use `v0.7.x` to decide support labels for provider-general inventory:
   macOS/Homebrew/mise stays supported preview; Linux read-only scanners stay
   experimental until fixture plus container/VM dogfood proves them; Windows
   evidence remains fixture/spike until a real runner or machine is available.
5. Continue provider evidence quality in `v0.7.x` with richer source URLs,
   ownership confidence, broader Homebrew/mise release-age/advisory confidence
   beyond the current registry paths, and provider-native audit paths where
   package identity is reliable.
6. Continue agent-friendly quality tooling dogfood: low-noise local gates,
   `govulncheck`, public-repo supply-chain checks, and pinned `aislop`
   non-blocking audit are implemented. Use the baseline in
   [SOURCE-STRUCTURE.md](SOURCE-STRUCTURE.md#agent-quality-audit-ledger) to
   decide which finding classes are worth cleanup or eventual blocking.
7. Continue scanner hardening after `v0.7.11` only where it improves bounded,
   explicit evidence. Keep Syft and Prowler explicit future commands, not
   default package update gates.
8. Extend provider contract drift checks beyond the local compatibility ledger
   only when public issue automation has explicit credentials, repository
   ownership, and posting policy.
9. Extend pending-update gates beyond the current Homebrew/mise/VS Code paths
   only as those providers move into Go, using the same updev-owned gate
   vocabulary and item-scoped allow/hold/review decisions.
10. Dogfood `updev apply brewfile` as the safe Homebrew desired-state bridge:
    keep active desired tied to `[brewfile].desired`, propose only missing
    desired installs, never uninstall extras, leave outdated updates to
    `updev update`, and keep `brew bundle` as explicit bootstrap/compatibility
    fallback.
11. Keep policy ergonomics current as policy rules grow: guided helpers,
   diagnostic indexes, and shadowed-rule references should remain visible from
   CLI and TUI review paths.
12. Keep `updev skill` / `updev help agent` synchronized with canonical
   `docs/agent/` files through docs-check drift coverage.
13. Continue hardening portable structured manual inventory sources and optional
   agent-assisted enrichment: agent output is structured draft metadata, not
   desired state, until the user accepts or edits it from CLI/TUI review.
14. Keep agent-assisted review optional for ambiguous candidates. Agent-generated
   manual app metadata must be schema-validated draft data, callable from the
   manual app TUI flow, and safe when Codex or another configured agent is not
   installed.
15. Keep Go CLI standard checks as part of future release reviews; direct
   subprocess exceptions, JSON encoding, and verbosity policy are documented for
   the current tool surface.
16. Keep release notes and public docs focused on `v0.7.x` preview hardening.
    Do not turn support-level labels into `v1.0.0` promises until the dogfood
    evidence is strong enough to freeze them.
17. Maintain the macOS/Homebrew/mise public preview with installation docs,
    privacy boundaries, and explicit experimental status for Linux/Windows
    providers. Reserve `updev v1.0.0` for the first stable public contract after
    that scope is deliberately narrowed and documented.

## v0.7.x Workstream Sequencing

Use this sequencing when turning the next major axes into issues or release
patches:

| Order | Workstream | First useful slice | Do not do yet |
|-------|------------|--------------------|---------------|
| 1 | Scalability refactor | `v0.7.5`: finish P1 by reducing `internal/cmd` below 50, splitting `manualinventory`, moving shared route state into `reviewui`, and auditing external CLI parser ownership. | Broad framework extraction, nested command subpackages that still own business logic, or provider evidence expansion before structure is unblocked. |
| 2 | Architecture guardrail | `v0.7.7`: enforce `internal/cmd` production/test budgets separately so command sprawl cannot silently return. | Treat this as the full refactor or start provider feature growth immediately after a guardrail-only release. |
| 3 | Required large refactor | `v0.7.8`: move report assembly, list/manual/backend view models, common routed TUI mechanics, and owner-specific tests out of `internal/cmd`; reduce production files to `<= 20` and tests to `<= 14`. | Docs-only, guardrail-only, cosmetic renames, broad framework extraction, or provider evidence expansion before the structure is actually thinner. |
| 4 | Provider evidence quality | `v0.7.10`: improve Homebrew/mise held/review explanations with source URLs, release dates, cache age, and item-scoped commands. | Promote Linux/Windows or opaque mise/vfox paths based on weak evidence. |
| 5 | Agent-friendly quality tooling | `v0.7.9`: keep the promoted Go CLI standard healthy by proving fast `check`, slower `audit`, and non-blocking agent-quality evidence stay low-noise in updev. | Make noisy SAST/style checks blocking before dogfood, or make every local `check` run slow vulnerability/AI-quality audits. |
| 6 | Scanner hardening | `v0.7.11`: make explicit scanner/native-audit evidence more structured and bounded, with clear unavailable semantics and report/TUI parity. | Make slow or broad scanners part of the default update gate. |
| 7 | Policy ergonomics | `v0.7.12`: add policy diagnostics, guided edit/renew/narrow flows, and route-to-intent TUI actions where users already review security details. | Auto-post public issues or create broad permanent allow rules without explicit user intent. |
| 8 | Route-to-intent completion | `v0.7.13`: finish item-scoped summary/list/last routes, keep Back/Home/focus restoration covered, and hide redundant primary review actions now replaced by focused routes. | Promote Linux/Windows providers, broaden default scanners, or start large architecture work without a focused release plan. |
| 9 | TTY dogfood polish | `v0.7.14`: use real TTY flows and cached reports to close remaining second-selection, Back/Home/focus, compact-readability, translation, and provider-evidence polish gaps after route-to-intent is complete. | Expand provider support or scanner defaults based on weak evidence. |

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
