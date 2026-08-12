# updev release target

This document tracks the implemented release and the next bounded target.
Long-term order belongs in [ROADMAP.md](ROADMAP.md); tag-specific history
belongs in [release-notes](release-notes/).

## Current Release

The current implemented release is `updev v0.7.19`. `updev version`,
`updev --version`, and `updev -v` report this command contract.

`v0.7.19` completes the first mise-centered package-authority slice without
turning mise bootstrap into an ungated daily mutation path. It also closes the
first full-TUI testing migration slice with an isolated, pinned shell-use
harness and reviewed terminal/visual baselines.

Current behavior:

- macOS/Homebrew/mise remains the supported preview path. Linux and Windows
  binaries are published, but provider behavior outside that path remains
  experimental.
- `updev apply brewfile` applies only missing, gate-approved desired items
  through an item-scoped executor. Extras remain read-only drift and outdated
  packages remain owned by `updev update`.
- Active Homebrew desired state merges the selected Brewfile with supported
  mise bootstrap package declarations. The bounded `brew/formula/btop` pilot
  is mise-owned while Intel macOS still uses native Homebrew execution.
- Backend convergence resolves formula and CLI-only cask names through one
  bounded mise registry snapshot before generic GitHub inference. Ownership
  migration remains a reviewed, multi-step operation.
- Chezmoi daily Brewfile hooks are warning-only by default. They never call
  `brew bundle`; bootstrap remains an explicit, separately warned path.
- TTY routes preserve item identity and origin state. Provider stdout/stderr
  remains outside the alternate-screen review surface.
- TUI acceptance separates model tests, built-binary semantic PTY journeys,
  terminal snapshots, and visual regression. Microsoft shell-use, resvg, and
  ODiff are project-pinned; golden updates are explicit.
- Oversized CLI, data-model, and validation contracts are organized as concise
  stable indexes with one-level child domains. Executable validation blocks and
  docs-check prevent orphaned children; release history stays in tag-specific
  notes and git history.

Previous baseline: [v0.7.18](release-notes/v0.7.18.md) introduced exact
candidate advisory classification and concise provider metadata failures.

Release-ready criteria:

- [x] `mise -C tools/updev run check`
- [x] `mise -C tools/updev run audit`
- [x] `mise -C tools/updev run docs-check`
- [x] `mise -C tools/updev run validation-check`
- [x] `mise -C tools/updev run goreleaser-check`
- [x] `mise -C tools/updev run goreleaser-snapshot`
- [x] `mise -C tools/updev run test-tui`
- [x] `mise -C tools/updev run test-e2e` compatibility gate
- [x] Complete the bounded
  [v0.7.19 real-terminal acceptance](validation/daily-and-tty.md#v0719-real-terminal-release-check)
  for summary shortcuts, installed/manual switch, focused Back/Home
  restoration, warning-only hook guidance, and provider logs
- [x] `brewtmplcheck --lenient` recognizes active mise-owned Homebrew desired
  items; the migrated `brew/formula/btop` must not appear as false `dump only`
  drift in the daily hook
- [x] `git diff --check`
- [x] `chezmoi apply --dry-run`
- [x] Release notes exist.

## Next Release Target: v0.7.20

`v0.7.20` is an architecture-ownership patch. It must not
broaden provider support or add a second package mutation surface.

Scope, in order:

1. **Complete.** `internal/updatereport` owns provider-neutral update report
   types, status aggregation, normalization, filters, summaries, and section
   projections. `internal/updateplan` owns default update steps and strict-safety
   projection to item-scoped Homebrew/mise command plans while provider packages
   retain exact argv construction. Parsing, terminal I/O, localization, route
   dispatch, provider execution, and visible streaming remain in `cmd`.
2. **Complete.** Inventory collection and cached refreshes accept an injected
   `runner.Runner`; update-time inventory reuses the update runner. Shared
   `runner.Request` execution consolidates optional env/streaming capability
   selection for update and Brewfile apply paths. Only CLI/TUI composition
   roots construct `runner.Local`.
3. **Complete.** Representative outer `cmd.Run` parsing tests cover update,
   list, last, apply, and their value-taking flags without invoking live
   providers. Forwarding aliases and zero-reference helpers were removed after
   command/package regression tests passed; the existing Staticcheck gate now
   blocks `U1000` as well as `SA*` findings.
4. **Complete.** `reviewui` exports are limited to command-consumed models,
   constructors, state helpers, and types required by those exported
   signatures. Package-local defaults, filters, renderers, wheel messages, and
   width calculations are private. Existing route, Back/Home, focus,
   expanded-row visibility, and action-consumption regressions remain covered
   without adding another route abstraction.
5. **Complete.** Machine-checkable validation blocks remain executable. The
   extractor tests prove comment-only assertions and incomplete commands are
   rejected with usage status, while an executable false assertion fails the
   gate.
6. **CI proof pending.** The shell-use semantic journey runs on
   `ubuntu-latest` and Intel `macos-15-intel`. Keep the tmux compatibility
   harness until both matrix jobs pass in the public repository; remove it only
   in a follow-up commit that preserves all accepted
   route/focus/cancellation journeys.
7. Keep completed macset `off | read | desired` integration stable while this
   refactor proceeds. The next cross-tool aggregate status task remains blocked
   on the completed outer updev command contract in step 3 and remains outside
   this release.

Non-goals:

- no broad migration of Brewfile packages into mise; the classified,
  cohort-based authority expansion is the planned `v0.7.21` target in
  [ROADMAP.md](ROADMAP.md#planned-v0721-package-authority-expansion);
- no full `mise bootstrap` daily apply path;
- no new default scanners or provider support promotion;
- no generic cross-tool framework extraction without two real consumers;
- no automatic baseline rewrite on visual failure.
