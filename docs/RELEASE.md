# updev release target

This document tracks the current implemented release and the next release
target. Keep longer-term ordering in [ROADMAP.md](ROADMAP.md), implementation
history in git log, and tag-specific details in
[release-notes](release-notes/).

Version labels use `<tool> vMAJOR.MINOR.PATCH`, while JSON `schema_version`
stays an integer schema contract. `v0.x` releases are public preview releases;
`v1.0.0` is reserved for the first stable public contract.

## Current Release

The current implemented release is `updev v0.7.13`. `updev version`,
`updev --version`, and `updev -v` report this command contract.

`updev v0.7.13` is the route-to-intent completion and navigation-surface cleanup
patch after the v0.7.12 policy ergonomics release. It preserves the same
supported macOS/Homebrew/mise preview scope, keeps provider log streaming
outside the alternate-screen TUI, opens focused summary/list/last rows on the
matching item context, and hides redundant top-level review actions that now
duplicate focused routes.

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
- [x] TUI route-to-intent tests prove summary/list/last row actions open
  focused item details without requiring a second item selection when a stable
  target exists.
- [x] TUI navigation tests prove provider/status inventory summary routes,
  update outcome security rows, and list cached-report inventory routes stay
  inside routed browsers.
- [x] Redundant inventory attention/detail choices are hidden from the primary
  update/list hubs, while compact/detail list views remain auxiliary advanced
  paths.
- [x] Release notes exist.

## Next Release Target: v0.7.14

`updev v0.7.14` is planned as a post-route-to-intent provider evidence and real
TTY dogfood polish patch. It should use cached reports and real terminal review
flows to improve evidence only where item identity, source context, release age,
and action target can be proved without broadening provider support.

Scope:

1. **Real TTY dogfood**
   - Dogfood `updev`, `updev last`, `updev list`, `security policy`, manual
     inventory, backend convergence, and update/security detail flows.
   - Fix route, Back/Home, focus restoration, and action-sheet regressions found
     in real terminal use.

2. **Provider evidence polish**
   - Improve Homebrew/mise evidence only where cached reports can prove the
     item identity, release age, source URL, and action target.
   - Keep opaque or unsupported providers review-only with concrete reason
     codes.

3. **Policy UX follow-through**
   - Keep policy cleanup, renew, and narrowing flows reachable from the security
     views where the relevant rule is visible.
   - Preserve JSON/text parity for every TUI-only convenience.

4. **Documentation and release hygiene**
   - Keep README, CLI docs, release notes, and public export docs current-state
     focused.
   - Keep generated/embedded docs drift checks passing.

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
- [ ] Real TTY dogfood covers `updev`, `updev last`, and `updev list`.
- [ ] Cached reports, JSON/text, and TUI detail views agree for any provider
  evidence or policy UX changes.
- [ ] Release notes exist.

## Released patch notes

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
