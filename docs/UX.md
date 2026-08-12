# updev interactive UX

This is the stable index for updev's interactive behavior. updev is a **full
TUI** consumer of the shared Go CLI standard. Plain text, JSON,
`--no-interactive`, and non-TTY fallback are CLI contracts and do not inherit
route, focus, or image-baseline requirements.

## UX Principle

updev is an action-review hub, not only a list viewer. A user should answer
these questions without leaving the TTY flow:

1. What changed, stopped, or needs review?
2. Why did it happen, and what evidence supports that state?
3. What is the next safe action, and can it run from this item?

`updev list` remains the installed-inventory entrypoint. It routes a focused
item to manual, backend, security, update, or apply review only when the target
identity is stable.

## Domain Index

| Document | Owns | Read when |
|----------|------|-----------|
| [ux/NAVIGATION.md](ux/NAVIGATION.md) | summary/list/detail/confirmation routes, focus restoration, route-to-intent | adding an entrypoint, route, shortcut, Back/Home behavior, or item-scoped action |
| [ux/REVIEW-FLOWS.md](ux/REVIEW-FLOWS.md) | row/evidence/action contract and domain-specific review flows | changing manual, backend, security, update, policy, or apply review |
| [ux/PERFORMANCE.md](ux/PERFORMANCE.md) | loading, asynchronous readiness, provider logs, cancellation, cached/partial reports | changing startup, refresh, streaming, background preparation, or TUI lifecycle |
| [DESIGN.md](DESIGN.md) | visual identity, semantic styles, component appearance, baseline IDs | changing colors, spacing, visual hierarchy, tables, detail composition, or visual tests |

Each child owns complete behavior for its topic. This index carries only
cross-cutting invariants and does not duplicate route tables or checklists.

## Cross-Cutting Invariants

- TTY views are projections of the same typed reports used by text and JSON.
- A focused action carries domain, target, intent, and origin when those values
  are known. Do not make the user reselect the same item in a generic list.
- Back, cancel, and recoverable error restore the originating route, cursor,
  filter/query, expanded row, scroll, and action focus.
- Enter operates the visible focused primary action where unambiguous. Number
  keys and `a` are accelerators, not the only usable interaction.
- Every write action identifies the exact target and effect, then requires a
  confirmation whose safe default is cancel.
- Loading, empty, ready, attention, error, and stale states keep a useful route
  shell and an available Back/Exit path.
- Provider stdout/stderr remains visible while real provider commands run;
  alternate-screen review never hides meaningful update progress.
- Non-TTY, `--plain`, JSON, and cached-report contracts remain deterministic.

## Release Boundary

Current and next implementation scope belongs in [RELEASE.md](RELEASE.md), not
here. Long-term ordering belongs in [ROADMAP.md](ROADMAP.md). Completed
checklists and release-by-release UX history belong in git history or release
notes. This contract changes only when the intended ongoing behavior changes.
