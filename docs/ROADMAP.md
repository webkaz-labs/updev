# updev roadmap

This document tracks long-term ordering and later work. Keep implementation
history in git log and tag-specific [release notes](release-notes/). The
implemented and next bounded release belong in [RELEASE.md](RELEASE.md).
Product boundaries live in [PRODUCT.md](PRODUCT.md), interaction behavior in
[UX.md](UX.md), and visual rules in [DESIGN.md](DESIGN.md).

## Current State

- The supported preview is macOS with Homebrew and mise. Linux read-only
  evidence remains experimental; Windows remains fixture/spike territory.
- Update, grouped inventory, cached review, desired-state drift, item-scoped
  manifest mutation, manual-app review, backend convergence, and security
  policy all share deterministic text/JSON reports and routed TTY views.
- Provider logs remain visible before the alternate-screen dashboard. Routed
  views preserve item identity, filter, focus, and origin on Back/Home.
- Homebrew and mise updates use updev-owned release-age, provenance, advisory,
  trust, and policy decisions. Optional scanners fail as structured evidence,
  not as hidden package mutations.
- `updev apply brewfile` applies only missing, gate-approved desired items
  through the selected item-scoped executor. Extras stay read-only drift and
  outdated packages stay owned by `updev update`.
- Resolved mise bootstrap packages, the active Brewfile, strict package
  metadata, package parity, and executor capability form one read/report/apply
  chain. The bounded `brew/formula/btop` pilot proves portable mise ownership
  with native Homebrew execution on Intel macOS.
- Daily chezmoi Brewfile hooks are warning-only and never call `brew bundle`.
  First-time bootstrap remains a separate, explicitly warned path.
- Fast checks, release audits, executable validation blocks, pinned shell-use
  semantic journeys, terminal snapshots, and rendered visual baselines protect
  the public command and TTY contracts.

## Near-Term Order

1. Finish the release gates in
   [RELEASE.md](RELEASE.md#next-release-target-v0720). `AD-U1` through `AD-U4`
   are complete: update report/planning ownership, injected execution,
   outer-command contracts, and the bounded `reviewui` export audit now have
   explicit owners and regression coverage.
2. Preserve the current one-level documentation ownership model and executable
   validation index while completing that refactor. Do not create a second
   writable index or move visual-system rules out of `DESIGN.md`.
3. Preserve the completed macset `off | read | desired` mise integration while
   finishing the updev owner extraction. Resume the shared
   machine-management plan with
   aggregate status only after the outer updev command contract is stable.
   Updev and macset remain standalone tools; chezmoi is optional integration.
4. After `v0.7.20` passes its release gates, execute the planned
   [v0.7.21 package-authority expansion](#planned-v0721-package-authority-expansion).
   Move desired declarations in bounded cohorts; do not create a second
   writable authority or use metadata as a package manifest.
5. Improve provider evidence quality where identity is reliable: source and
   release metadata, advisory precision, cache age, ownership confidence, and
   provider-native audits. Keep broad or opaque evidence in review state.
6. Harden policy ergonomics, manual-app structured sources, and optional agent
   enrichment without adding automatic permanent allows or making an agent a
   runtime requirement.
7. Expand support only with matching fixtures and runtime evidence. Linux can
   advance through container/VM dogfood; Windows requires a real runner or
   machine before becoming a release promise.

## Planned v0.7.21: Package Authority Expansion

`v0.7.21` starts only after the `v0.7.20` architecture-ownership work is
complete. Its goal is to move the classified, migration-ready Homebrew desired
declarations to mise-centered authority without creating a second writable
package manifest. Formula, cask, and tap changes run as bounded cohorts; the
metadata sidecar remains annotation-only; unsupported identities stay explicit
in the Brewfile with a stable deferral reason.

The canonical target model, frozen classification baseline, cohort ledger,
acceptance evidence, and promotion gate are `MM-107` through `MM-110` in the
mise machine-management plan.
This roadmap owns only release ordering. Completion means every identity in the
frozen baseline is migrated or explicitly deferred and the one-authority
promotion gate passes. Broad install/uninstall, daily full `mise bootstrap`, and
removal of classified Brewfile exceptions remain non-goals.

## Architecture Direction

- `internal/cmd` owns argument parsing, terminal I/O, localization, and route
  dispatch. Provider-neutral planning, report construction, persistence, and
  execution belong to domain packages with narrow runner dependencies.
- Text, JSON, cached reports, and TTY views project the same typed report and
  stable decision vocabulary. No view may recreate provider policy.
- External commands use context-aware runner seams. A direct subprocess
  exception needs a documented ownership reason and regression coverage.
- Shared internals are extracted only after updev and another maintained tool
  need the same tested API. Similar names alone are not sufficient.
- Desired-source migration switches readers, writers, wrappers, hooks, and
  rollback together. Brewfile and mise declarations must not become two
  writable authorities for one active identity.

## Product And Release Invariants

- Provider logs stay visible outside the alternate-screen TUI.
- Cached reports, JSON/text, and TTY details agree on decisions and reasons.
- Safe actions remain item-scoped; unsupported, unavailable, hold, review, and
  block states never produce an executable mutation.
- Support labels describe evidence boundaries, not stable-release promises.
- Optional scanner or agent integrations degrade to explicit unavailable or
  skipped evidence.
- Current-state docs contain behavior and decision rules. Release chronology
  belongs only in release notes and git history.
- `updev v1.0.0` waits for the readiness criteria in
  [PRODUCT.md](PRODUCT.md#stable-release-readiness), not a calendar milestone.

## Later Targets

- Extend package/tool providers incrementally after the macOS/Homebrew/mise
  path remains stable, using the same allow/hold/review/block contract.
- Promote Linux provider behavior only after fixtures plus container/VM
  dogfood; defer Windows promotion until real-machine evidence exists.
- Extend the provider compatibility ledger only when local/CI drift needs new
  stable fields or an explicitly authorized issue-posting policy.
- Keep embedded agent guidance, README discovery, command help, validation
  tasks, CI, release tags, and release-note files checked against their
  canonical sources.
- Keep Markdown app reports generated from structured sources and live
  evidence; repository-specific prose is not a public desired-state default.
- Consider OS-settings provider expansion only after package-provider and
  cross-tool ownership lessons are stable.
