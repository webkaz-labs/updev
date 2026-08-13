# updev validation

This is the stable validation index. Use isolated fixtures for commands that
can mutate manifests or local policy. Real-machine writes always require the
explicit command boundary and user approval.

## Standard Gates

```bash
mise -C tools/updev run check
mise -C tools/updev run audit
mise -C tools/updev run docs-check
mise -C tools/updev run validation-check
mise -C tools/updev run test-tui
git diff --check
chezmoi apply --dry-run
```

- `check` is the normal agent/pre-commit gate: formatting, Staticcheck
  (`SA*` correctness checks and `U1000` dead-code checks),
  golangci-lint, ShellCheck, tests, vet, module verification, build, mise
  contract fixtures, and deterministic Go analyzer cache recovery fixtures.
- `audit` is the slower release/scheduled gate: govulncheck, GitHub Actions
  supply-chain posture, and non-blocking agent-quality evidence.
- `test-tui` runs pinned shell-use semantic and visual lanes. Use
  `test-tui-update-baselines` only for an explicit reviewed baseline update.
- `test-e2e` remains the tmux compatibility gate during shell-use migration;
  `test-e2e-smoke` is the short local dashboard loop.
- `validation-check` executes the marked release-smoke block below and rejects
  comment-only or incomplete assertions.

## Machine-Checkable Release Smoke

<!-- validation:release-smoke -->
```bash
test -f docs/PRODUCT.md
grep -q '^# updev product' docs/PRODUCT.md
grep -q '^The current implemented release is `updev v0.7.20`' docs/RELEASE.md
test "$(grep -c '^## Released patch notes' docs/RELEASE.md || true)" -eq 0
test "$(go run . version)" = 'updev v0.7.20'
```

The semantic TUI lane uses `NO_COLOR=1` plus isolated HOME/XDG/TMPDIR/provider
fixtures. Linux and macOS run the same journeys and assert OS-neutral route,
focus, action, and content invariants; canonical exact terminal snapshots are
compared on macOS. The visual lane fixes terminal size, locale, theme, and
color; captures SVG; normalizes volatile values without changing glyph
positions; renders with pinned resvg; and compares exact PNG output with pinned
ODiff. Baseline IDs and review criteria are canonical in [DESIGN.md](DESIGN.md).

## Detail Checklists

| Document | Read when |
|----------|-----------|
| [daily-and-tty.md](validation/daily-and-tty.md) | Validating update/list/last, inventory/manual review, streaming, focus restoration, or real-terminal UX. |
| [mutations-and-backends.md](validation/mutations-and-backends.md) | Validating add/remove/rollback/sync or backend convergence. |
| [security-and-integration.md](validation/security-and-integration.md) | Validating security, explicit install detection, Brewfile checks, wrapper integration, or public install. |
| [release.md](validation/release.md) | Validating GoReleaser config, archives, checksums, and release-note wiring. |

Machine-dependent timing values are temporary dogfood evidence, not permanent
criteria. Keep stable behavior and route assertions in these documents.
