# updev release target

This document tracks the current implemented release and the next release
target. Keep longer-term ordering in [ROADMAP.md](ROADMAP.md), implementation
history in git log, and tag-specific details in
[release-notes](release-notes/).

Version labels use `<tool> vMAJOR.MINOR.PATCH`, while JSON `schema_version`
stays an integer schema contract. `v0.x` releases are public preview releases;
`v1.0.0` is reserved for the first stable public contract.

## Current Release

The current implemented release is `updev v0.7.14`. `updev version`,
`updev --version`, and `updev -v` report this command contract.

`updev v0.7.14` is the first polish patch after the route-to-intent completion
release. It preserves the same supported macOS/Homebrew/mise preview scope,
keeps provider log streaming outside the alternate-screen TUI, keeps focused
summary/list/last routes inside their origin browser, and adds bounded
Homebrew-drift actions plus readability/quality cleanup where cached reports
can prove the item identity and safe write target.

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
- Security policy changes stay explicit and local. Diagnostics can guide
  cleanup, renewals, and narrower rules, but updev does not create broad
  permanent allows or post remote issues without explicit user intent.

Current release validation:

- [x] `mise -C tools/updev run check`
- [x] `mise -C tools/updev run docs-check`
- [x] `git diff --check`
- [x] `chezmoi apply --dry-run`
- [x] Route-to-intent and focus-regression tests prove summary/list/last row
  actions open focused item details and restore origin focus without requiring a
  second item selection when a stable target exists.
- [x] Homebrew inventory drift tests prove extra live items explain likely
  bypass causes and only offer category-explicit Brewfile adoption through the
  existing mutation boundary.
- [x] Hub/navigation tests prove `updev hub` is a review-domain switcher instead
  of a duplicate filter menu, while filters remain available from inventory
  browsing.
- [x] Release notes exist.

## Next Release Target: v0.7.15

`updev v0.7.15` should continue the same preview scope and spend the next patch
on the highest-friction dogfood loops that remain after v0.7.14: preventing
Homebrew drift before it appears, tightening provider evidence where release-age
proof is still weak, and keeping list/hub/detail navigation low-step.

Scope:

1. **Drift prevention and wrapper diagnostics**
   - Add user-facing diagnostics that explain whether the shell `brew` wrapper
     is active in the current shell and whether the low-level `brewfile`
     mutation boundary is configured.
   - Plan the `brew` wrapper adoption flow so successful installs can keep the
     Brewfile in sync without surprising category guesses.
   - Keep direct `brewfile` and compatibility commands documented as low-level
     surfaces, not the primary workflow.

2. **Provider evidence follow-through**
   - Continue improving Homebrew/mise evidence only where cached reports can
     prove item identity, release age, source URL, and action target.
   - Add resolver-backed detail only through data-driven provider metadata; keep
     opaque or unsupported providers review-only with concrete reason codes.
   - Revisit high-noise summaries and raw evidence strings after real TTY
     dogfood, but avoid broad scanner/provider expansion.

3. **TUI workflow polish**
   - Dogfood `updev`, `updev last`, `updev list`, `updev hub`, security policy,
     manual inventory, backend convergence, and update/security detail flows
     from a real TTY and from cached reports.
   - Fix any path where a focused actionable row still opens an unrelated list,
     loses Back/Home/focus/expanded-row state, or makes the user select the same
     item again.
   - Keep provider command execution outside alternate-screen TUI so Homebrew
     and mise logs continue to stream normally.

4. **Review readability polish**
   - Make expanded detail rows lower-noise: concise Japanese summaries first,
     grouped evidence second, raw provider/log/source details only when useful.
   - Keep update/security/backend/manual action labels short and stable, with
     clear disabled or review-only reasons when no direct action is safe.
   - Keep summary tables readable: no duplicated security sections, no overly
     long reason text in compact rows, and no header/body alignment regressions.
   - Make backend convergence loading and empty states explicit so a preparing
     backend plan is not mistaken for "0 findings".

5. **Policy UX follow-through**
   - Keep policy cleanup, renew, and narrowing flows reachable from the security
     views where the relevant rule is visible.
   - Preserve JSON/text parity for every TUI-only convenience.

6. **Regression coverage and release hygiene**
   - Add or update fast route/readability regression tests for every fixed TTY
     path. Reserve slow PTY/e2e dogfood for release acceptance.
   - Keep README, CLI docs, release notes, and public export docs current-state
     focused.
   - Keep generated/embedded docs drift checks passing.

7. **Bounded quality cleanup**
   - When touching v0.7.15 routes, reduce local duplication in route
     construction, action labels, compact reason formatting, and empty/loading
     state handling.
   - Move reusable provider-drift and review-action helpers into existing owner
     packages (`brewfile`, `inventoryrun`, `reviewui`, `textui`, or provider
     packages) instead of adding ad hoc helpers under `cmd`.
   - Add focused tests for any extracted helper. Do not churn stable code only
     to satisfy size metrics, and do not reopen the completed large refactor
     unless a v0.7.15 bug fix requires it.

Non-goals:

- stable `v1.0.0` promises;
- Linux/Windows promotion beyond experimental provider evidence;
- broad scanner default expansion;
- public issue posting or automatic remote remediation;
- large architecture refactors without a focused release plan.

Release-ready criteria:

- [ ] `mise -C tools/updev run check`
- [ ] `mise -C tools/updev run docs-check`
- [ ] `git diff --check`
- [ ] `chezmoi apply --dry-run`
- [ ] Real TTY dogfood covers `updev`, `updev last`, `updev list`, `updev hub`,
  and at least one route each for update logs, security review, inventory
  attention, manual review, backend convergence, and policy review.
- [ ] No accepted focused row with a stable target opens a generic second-choice
  list or returns to the wrong origin after Back/Home.
- [ ] Compact summary/detail rows remain readable in Japanese: concise labels,
  no duplicated sections, no excessive raw evidence in compact rows, and aligned
  table headers.
- [ ] Cached reports, JSON/text, and TUI detail views agree for any provider
  evidence or policy UX changes.
- [ ] Release notes exist.

## Released patch notes

- [v0.7.14](release-notes/v0.7.14.md): post-route-to-intent polish with
  Homebrew drift adoption actions, review-domain hub cleanup, and bounded
  quality fixes.
- [v0.7.13](release-notes/v0.7.13.md): route-to-intent completion, focused
  summary/list/last routes, and navigation surface cleanup.
- [v0.7.12](release-notes/v0.7.12.md): policy diagnostics, guided
  maintenance, and route-to-intent TUI actions.
- [v0.7.11](release-notes/v0.7.11.md): scanner hardening patch with structured
  scanner/native-audit unavailable and error evidence.
- [v0.7.10](release-notes/v0.7.10.md): provider evidence quality patch with
  richer Homebrew/mise release age, source, cache, and localized detail
  evidence.
- [v0.7.9](release-notes/v0.7.9.md): agent-friendly quality tooling patch with
  promoted Go CLI standard gates, slower audit checks, and non-blocking
  agent-quality evidence.
- [v0.7.8](release-notes/v0.7.8.md): large refactor with sync/config owner
  packages, consolidated routed adapters, and stricter command package budgets.
- [v0.7.7](release-notes/v0.7.7.md): command production-file budget hardening
  before provider/scanner/policy feature growth.
- [v0.7.6](release-notes/v0.7.6.md): Japanese TUI help text consistency fix
  for the support-catalog label filter.
- [v0.7.5](release-notes/v0.7.5.md): scalability refactor completion with
  package-count gates, manual inventory platform split, owner-package
  extractions, and route-state helper consolidation.
- [v0.7.4](release-notes/v0.7.4.md): list evidence projection extraction,
  tighter evidence key matching, and v0.7.x workstream foundation checks.
- [v0.7.3](release-notes/v0.7.3.md): support-label TUI visibility, provider
  support badges, and dependency support-label text output.
- [v0.7.2](release-notes/v0.7.2.md): compatibility-ledger JSON contract test
  precision for support labels.
- [v0.7.1](release-notes/v0.7.1.md): compatibility-ledger JSON contract polish
  for support labels.
- [v0.7.0](release-notes/v0.7.0.md): public-preview hardening with support
  labels and compatibility-ledger support labels.
- [v0.6.5](release-notes/v0.6.5.md): full-scope v0.6.x consolidation release.
- [v0.6.4](release-notes/v0.6.4.md): text UI badge-boundary maintenance patch.
- [v0.6.3](release-notes/v0.6.3.md): provider-boundary maintenance patch.
- [v0.6.2](release-notes/v0.6.2.md): first architecture cleanup/export patch.
- [v0.6.1](release-notes/v0.6.1.md): provider-gate maintenance patch.
- [v0.6.0](release-notes/v0.6.0.md): first updev-owned Homebrew/mise provider
  gate release.
