# updev release target

This document tracks the current implemented release and the next release
target. Keep longer-term ordering in [ROADMAP.md](ROADMAP.md), implementation
history in git log, and tag-specific details in
[release-notes](release-notes/).

Version labels use `<tool> vMAJOR.MINOR.PATCH`, while JSON `schema_version`
stays an integer schema contract. `v0.x` releases are public preview releases;
`v1.0.0` is reserved for the first stable public contract.

## Current Release

The current implemented release is `updev v0.7.17`. `updev version`,
`updev --version`, and `updev -v` report this command contract.

`updev v0.7.17` is a summary-first TTY routing patch for the
macOS/Homebrew/mise public preview. It keeps the `v0.7.16` Brewfile hook/apply
bridge, keeps daily hooks warning-only, and makes the post-update dashboard
faster to use by routing inventory summaries to installed inventory and adding
one-key summary jumps for common review domains.

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

## Next Release Target: v0.7.18

`updev v0.7.18` should keep the v0.7.17 summary-first dashboard stable while
deciding whether the Brewfile hook bridge should grow an explicit safe-apply
opt-in.

Scope:

1. Decide whether `[chezmoi_hooks.brewfile].mode = "apply-safe"` remains
   documented-only or becomes an explicit opt-in daily hook mode. The default
   daily hook must remain warning-only either way.
2. If `apply-safe` becomes real, make it call only `updev apply brewfile
   --safe-only` after the same item-scoped release-age, tap trust, provenance,
   and security-policy gates used by normal apply.
3. Improve apply-candidate detail readability only where real dogfood shows
   unnecessary verbosity: concise reason first, grouped evidence second, raw
   command/provider evidence only where useful.
4. Add focused regression coverage for daily hook mutation guards if the
   dotfiles hook keeps evolving.
5. Keep public docs generic and avoid environment-specific paths, profile names,
   or local package assumptions.

Non-goals:

- stable `v1.0.0` promises;
- Linux/Windows promotion beyond experimental provider evidence;
- broad scanner default expansion;
- public issue posting or automatic remote remediation;
- large architecture refactors without a focused release plan;
- general Homebrew cleanup/uninstall automation;
- unscoped `brew bundle` as a daily mutation path.

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
- [ ] Release notes exist.

## Released patch notes

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
