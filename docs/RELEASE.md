# updev release target

This document tracks the current implemented release and the next release
target. Keep longer-term ordering in [ROADMAP.md](ROADMAP.md), implementation
history in git log, and tag-specific details in
[release-notes](release-notes/).

Version labels use `<tool> vMAJOR.MINOR.PATCH`, while JSON `schema_version`
stays an integer schema contract. `v0.x` releases are public preview releases;
`v1.0.0` is reserved for the first stable public contract.

## Current Release

The current implemented release is `updev v0.7.18`. `updev version`,
`updev --version`, and `updev -v` report this command contract.

`updev v0.7.18` is a strict safety evidence precision patch for the
macOS/Homebrew/mise public preview. It keeps the `v0.7.17` summary-first route
shortcuts, keeps the `v0.7.16` Brewfile hook/apply bridge warning-only by
default, and makes safety holds easier to understand by separating provider
metadata failures from advisory evidence and by showing whether an advisory
match affects the exact update candidate, only matches a source range, or is
related evidence that still needs review.

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
- [x] `mise -C tools/updev run goreleaser-check`
- [x] `mise -C tools/updev run goreleaser-snapshot`
- [x] `mise -C tools/updev run test-e2e-smoke`
- [x] `updev apply brewfile --dry-run --format json`
- [x] `updev apply brewfile --safe-only --dry-run`
- [x] `updev list --status missing --plain --limit 20`
- [x] Release notes exist.

## Next Release Target: v0.7.19

`updev v0.7.19` should continue hardening the Brewfile hook/apply bridge and
reduce remaining TUI route friction without changing the public preview
boundary. The daily hook default stays warning-only; any mutation path remains
explicit, item-scoped, and gated by the same release-age, tap-trust,
provenance, and policy checks as normal updev commands.

Scope:

1. Finish the hook/apply contract:
   - keep `run_onchange_after_*brew*` warning-only by default;
   - document and, if implemented, gate
     `[chezmoi_hooks.brewfile].mode = "apply-safe"` behind explicit opt-in;
   - route the opt-in path through `updev apply brewfile --safe-only`, never
     raw `brew bundle`;
   - keep first-time bootstrap as a separate explicit path that warns it bypasses
     updev's normal safety gate.
2. Make `updev apply brewfile` a dependable item-scoped desired-state bridge:
   - active desired follows `[brewfile].desired`;
   - missing desired Homebrew items become install candidates;
   - extras remain drift evidence and are never uninstalled automatically;
   - outdated items remain the responsibility of `updev update`;
   - standalone Brewfile markers such as `# updev: category <name>` work without
     requiring chezmoi.
3. Complete route-to-intent polish for apply/drift review:
   - summary, list, last, and hub routes open the focused item when a stable
     target exists;
   - Back/Home restores the originating cursor, filter, expanded row, and
     action focus;
   - actions that only open the same generic list are removed or replaced by a
     focused route;
   - summary shortcut keys remain available only on summary hubs and do not
     interfere with detail-row expansion.
4. Reduce TUI detail cognitive load:
   - show a concise decision/reason first;
   - group update, security, backend, and apply evidence into short sections;
   - hide raw provider commands/logs behind expanded evidence rows unless the
     row is an error;
   - keep Japanese human labels aligned across summary, list, last, and detail
     surfaces.
5. Harden Homebrew drift/trust edge cases:
   - recognize already trusted taps and do not keep review rows alive after the
     trust state changes;
   - distinguish item-scoped trust, tap-scoped trust, missing desired installs,
     deployment-scope mismatch, and cask repair failures;
   - refresh the cached report or current routed view after a local write action
     so the user does not see stale drift rows.
6. Extend the v0.7.18 metadata/advisory precision work only where dogfood shows
   remaining ambiguity:
   - keep npm/Cargo/PyPI/vendor checks as bounded metadata probes, not embedded
     package-manager clients;
   - hold only the affected candidates when the failed probe can be item-scoped;
   - keep policy override guidance tied to the exact reason that caused the
     hold or review.
7. Add focused regression coverage for:
   - daily hook warning-only mutation guards;
   - apply-safe opt-in guards if implemented;
   - route-to-intent focus restoration from `updev`, `updev last`, and
     `updev list`;
   - standalone Brewfile behavior without chezmoi-specific assumptions;
   - public docs avoiding environment-specific paths, profile names, and local
     package assumptions.

Non-goals:

- stable `v1.0.0` promises;
- Linux/Windows promotion beyond experimental provider evidence;
- broad scanner default expansion;
- public issue posting or automatic remote remediation;
- large architecture refactors without a focused release plan;
- general Homebrew cleanup/uninstall automation;
- unscoped `brew bundle` as a daily mutation path;
- making apply-safe the default daily hook behavior.

Release-ready criteria:

- [ ] `mise -C tools/updev run check`
- [ ] `mise -C tools/updev run docs-check`
- [ ] `git diff --check`
- [ ] `chezmoi apply --dry-run`
- [ ] `mise -C tools/updev run goreleaser-check`
- [ ] `mise -C tools/updev run goreleaser-snapshot`
- [ ] Real TTY dogfood covers the warning-only daily hook, `updev apply
  brewfile --dry-run`, `updev list --status missing`, focused apply-candidate
  routes when candidates exist, Back/Home/focus restore, and visible Homebrew
  command logs.
- [ ] Real TTY dogfood covers summary shortcuts from `updev` and `updev last`
  plus installed/manual inventory switching from `updev list`.
- [ ] Standalone Brewfile fixture coverage proves no chezmoi dependency is
  required for marker parsing or missing desired install candidates.
- [ ] Release notes exist.

## Released patch notes

- [v0.7.18](release-notes/v0.7.18.md): strict safety evidence precision with
  concise provider metadata failure summaries and OSV/advisory match
  classification.
- [v0.7.17](release-notes/v0.7.17.md): summary-first TTY routing with
  installed-inventory summary routes and one-key `i/m/b/s/u` jumps from
  `updev` and `updev last`.
- [v0.7.16](release-notes/v0.7.16.md): Brewfile hook ownership split with
  warning-only daily hooks, explicit bootstrap fallback, and safe
  `updev apply brewfile` item-scoped installs.
- [v0.7.15](release-notes/v0.7.15.md): drift-prevention patch with Homebrew
  wrapper diagnostics, trust handling, rendered Brewfile sync, and routed
  Homebrew drift actions.
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
