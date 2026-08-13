# updev release target

This document tracks the implemented release and the next bounded target.
Long-term order belongs in [ROADMAP.md](ROADMAP.md); tag-specific history
belongs in [release-notes](release-notes/).

## Current Release

The current implemented release is `updev v0.7.20`. `updev version`,
`updev --version`, and `updev -v` report this command contract.

`v0.7.20` combines the first bounded mise-centered package-authority slice with
the architecture-ownership work needed before expanding that authority. The
`v0.7.19` implementation baseline was not tagged separately; its user-facing
changes and validation evidence are included in `v0.7.20`.

Current behavior:

- macOS/Homebrew/mise remains the supported preview path. Linux and Windows
  binaries are published, but provider behavior outside that path remains
  experimental.
- `updev apply brewfile` applies only missing, gate-approved desired items
  through an item-scoped executor. Extras remain read-only drift and outdated
  packages remain owned by `updev update`.
- Active Homebrew desired state merges the selected Brewfile with supported
  mise bootstrap declarations. The bounded `brew/formula/btop` pilot is
  mise-owned while Intel macOS still uses native Homebrew execution.
- Provider-neutral update reports and strict command planning are owned by
  `internal/updatereport` and `internal/updateplan`. Inventory and package apply
  execution use injected runner seams; only composition roots construct a
  local runner.
- Outer command parsing covers update, list, last, and apply without invoking
  live providers. `reviewui` keeps only command-consumed exports while route,
  Back/Home, focus, expanded-row visibility, and action-consumption behavior
  remain regression-tested.
- Daily Brewfile hooks are warning-only and never call `brew bundle`.
  Bootstrap remains an explicit, separately warned path.
- TUI acceptance separates model tests, built-binary semantic PTY journeys,
  terminal snapshots, and visual regression. The semantic journey passes on
  Linux and Intel macOS CI; macOS owns the exact terminal snapshots.
- Machine-checkable validation blocks reject comment-only or incomplete
  assertions, and release tags rerun the reusable CI workflow before version,
  release-note, GoReleaser, and artifact-attestation steps.

Previous tagged baseline: [v0.7.18](release-notes/v0.7.18.md) introduced exact
candidate advisory classification and concise provider metadata failures.

Release evidence:

- [x] `mise -C tools/updev run check`
- [x] `mise -C tools/updev run audit`
- [x] `mise -C tools/updev run docs-check`
- [x] `mise -C tools/updev run validation-check`
- [x] `mise -C tools/updev run goreleaser-check`
- [x] `mise -C tools/updev run goreleaser-snapshot`
- [x] `mise -C tools/updev run test-tui`
- [x] `mise -C tools/updev run test-e2e` compatibility gate
- [x] Complete the bounded
  [v0.7.20 real-terminal acceptance](validation/daily-and-tty.md#v0720-real-terminal-release-check)
  for summary shortcuts, installed/manual switch, focused Back/Home
  restoration, warning-only hook guidance, and provider logs
- [x] `brewtmplcheck --lenient` recognizes active mise-owned Homebrew desired
  items without false `dump only` drift
- [x] Linux and Intel macOS semantic TUI CI proof
- [x] `git diff --check`
- [x] `chezmoi apply --dry-run`
- [x] [v0.7.20 release notes](release-notes/v0.7.20.md) exist.

## Next Release Target: v0.7.21

`v0.7.21` expands package authority only after the `v0.7.20` ownership seams
are released. It follows `MM-107` through `MM-110` in the shared
mise machine-management plan.

Scope, in order:

1. Freeze a deterministic, read-only `MM-107` classification baseline for every
   active Homebrew formula, cask, tap, mise tool, and bootstrap package identity.
2. Move reviewed formula cohorts of at most ten exact identities to `[tools]`
   or `[bootstrap.packages]`, preserving executor and rollback evidence.
3. Move reviewed cask and tap cohorts to `brew-cask:` declarations and
   `[bootstrap.brew.taps]`; keep unsupported identities in the Brewfile with a
   stable deferral reason.
4. Promote only when every frozen identity and reviewed delta is migrated or
   explicitly deferred, parity is clean, each identity has one writable
   authority, and real-Mac apply/update plus four-part rollback pass.

Release-ready criteria:

- [ ] Commit `docs/evidence/package-authority/mm-107-baseline.v1.json` without
  mutating desired state.
- [ ] Complete every generated `MM-108-Cnn` and `MM-109-Cnn` cohort row.
- [ ] Pass package parity, executor capability, wrapper/watcher, JSON/text/TUI,
  architecture dry-run, and real Intel Mac acceptance gates.
- [ ] Prove the one-authority promotion gate and canonical four-part rollback.
- [ ] Run the standard check, audit, docs, validation, TUI, GoReleaser,
  diff, and chezmoi dry-run release gates.
- [ ] Add `v0.7.21` release notes.

Non-goals:

- no broad install/uninstall or full daily `mise bootstrap` apply;
- no use of `package-metadata.toml` as a writable package manifest;
- no migration of identities without exact cohort review and rollback evidence;
- no provider support promotion, new default scanner, or generic shared
  framework extraction.
