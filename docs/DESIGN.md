---
version: alpha
name: updev Terminal Operations
description: Dense, calm terminal design for package update and evidence review.
colors:
  canvas: "#0C0D0E"
  primary: "#E6E6E6"
  muted: "#737A85"
  section: "#BB9AF7"
  identity: "#7DCFFF"
  requested: "#7AA2F7"
  success: "#9ECE6A"
  attention: "#E0AF68"
  danger: "#F7768E"
typography:
  terminal:
    fontFamily: monospace
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1
spacing:
  inset: "2px"
  section-gap: "1px"
  column-gap: "2px"
components:
  dashboard-shell:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.primary}"
  metadata-help:
    textColor: "{colors.muted}"
  section-heading:
    textColor: "{colors.section}"
  focused-row:
    textColor: "{colors.identity}"
  requested-value:
    textColor: "{colors.requested}"
  successful-state:
    textColor: "{colors.success}"
  attention-state:
    textColor: "{colors.attention}"
  dangerous-state:
    textColor: "{colors.danger}"
---

# updev Terminal Operations

This file is the visual source of truth for updev's full TUI. Product scope
lives in [PRODUCT.md](PRODUCT.md), route and interaction behavior in
[UX.md](UX.md), implementation structure in [ARCHITECTURE.md](ARCHITECTURE.md),
and executable visual checks in [VALIDATION.md](VALIDATION.md).

## Overview

updev should feel like a well-run operations console: dense, calm, and
evidence-first. It is designed for engineers repeatedly scanning updates,
holds, security findings, desired-state drift, and item-scoped actions. The
interface favors aligned information and predictable focus over decorative
chrome. Failure increases clarity, not visual noise.

The YAML palette is the fixed reference theme for visual baselines. Runtime
ANSI colors remain terminal-theme dependent, but semantic roles, contrast
hierarchy, and non-color cues are stable. `internal/textui` owns style helpers;
routes must not introduce raw ANSI styling independently.

## Colors

| Role | Current implementation | Reference token | Required non-color cue |
|------|------------------------|-----------------|------------------------|
| canvas/text | terminal defaults | `{colors.canvas}` / `{colors.primary}` | labels, grouping, spacing |
| metadata/help | dim | `{colors.muted}` | lower hierarchy, never hidden |
| section | bold magenta | `{colors.section}` | section label and row gap |
| focus/identity/key | cyan plus leading marker | `{colors.identity}` | leading `>` and cursor position |
| requested version/count | blue | `{colors.requested}` | requested/count column |
| action/success/active/updated | green | `{colors.success}` | verb or status word |
| review/hold/drift | yellow | `{colors.attention}` | `review`, `hold`, or `drift` |
| error/blocked | red | `{colors.danger}` | `error` or `blocked` |

A distinction must not rely on green versus blue, icon availability, or Nerd
Font detection alone. `NO_COLOR` preserves the same words, markers, ordering,
and hierarchy. Add a semantic helper in `internal/textui` before adding a new
visual role.

## Typography

The `monospace` token means the user's configured terminal font and cell
metrics, not a bundled font. Bold identifies headings; dim
de-emphasizes metadata. Do not simulate display typography, scale text by
viewport, or use letter spacing. Japanese, English, icons, and URLs are measured
with the same terminal-cell width implementation used by tables and wrapping.

## Layout

- Spacing token magnitudes map to terminal cells, not literal rendered pixels.
- Horizontal inset is `{spacing.inset}` cells and may reduce to one at `80x24`.
- Unrelated groups have `{spacing.section-gap}` blank row.
- Columns use `{spacing.column-gap}` cells where width allows. Collapse detail
  before identity, status, or action.
- Title/help regions reserve stable measured rows so focus hints and loading
  messages do not move the body.
- Use unframed sections and grouped tables. Avoid nested boxes.
- Truncate with a cell-aware ellipsis. Expanded evidence wraps without breaking
  URL schemes at `:`.
- `80x24` is minimum, `120x36` canonical, and `160x48` the wide table check.

## Components

| Component | Visual anatomy | Visual invariants |
|-----------|----------------|-------------------|
| dashboard shell | title, key help, summary sections, focused action | no timed navigation; body does not jump as hints change |
| grouped table/list | group heading, aligned columns, compact rows | headings remain distinct; semantic columns remain stable |
| row | focus marker, identity, status, compact action badge, summary | one focus marker; identity is not clipped first |
| expanded detail | concise decision, then details/evidence/actions | do not repeat metadata already visible in the row |
| action list | cursor, short verb, short consequence | arrows and Enter work; number keys are accelerators only |
| confirmation | exact item, mutation/effect, confirm/cancel | cancel is visibly selected by default |
| loading/error/empty | stable route shell and state-specific body | never show a blank or apparently frozen screen |

Visual composition does not redefine interaction. Focus restoration, route
targets, input precedence, asynchronous refresh, and Back/Home behavior are
owned by [UX.md](UX.md) and its child documents.

## Visual Baselines

| Baseline ID | Route/state | Viewport | Required visual evidence |
|-------------|-------------|----------|--------------------------|
| `dashboard-ready-ja` | post-update summary with update/security/inventory/review | `120x36` | hierarchy, aggregate state, visible focus |
| `inventory-grouped-ja` | grouped installed inventory with action badges | `120x36` | grouping, aligned columns, compact actions |
| `inventory-expanded-last-ja` | final visible inventory row expanded | `120x36` | detail/action scroll into view |
| `security-detail-ja` | mixed hold/review table and expanded evidence | `120x36` | status distinction and readable evidence |
| `mutation-confirm-ja` | item-scoped confirmation | `80x24` | target/effect visible; cancel focused |

Each ID maps to a canonical macOS terminal snapshot and a PNG visual baseline
under `test/tui/baselines/`. The same shell-use journeys run on Linux and
macOS and assert OS-neutral route, focus, action, and content invariants. Exact
terminal snapshots remain macOS-only because provider availability and rows are
platform-dependent. The project-local wrapper uses pinned Microsoft `shell-use`
for semantic state and SVG capture, pinned `resvg` for PNG render, and pinned
`ODiff` for exact comparison. The tmux suite remains only as a bounded migration
compatibility gate.

Layout-size mismatch fails. Pixel comparison defaults to exact. Any tolerance
or ignored region is baseline-specific, measured, documented in
`VALIDATION.md`, and reviewed with actual/diff images. A failed test never
rewrites a baseline automatically.

## Do's and Don'ts

- **Do** make the current decision, affected item, and next safe action scan in
  that order.
- **Do** preserve complete keyboard and `NO_COLOR` operation.
- **Do** keep status labels short in rows and full evidence in expansion.
- **Don't** use color, an emoji, or a Nerd Font glyph as the only state cue.
- **Don't** add decorative borders, nested cards, or empty framing.
- **Don't** repeat row metadata in expanded detail or action descriptions.
- **Don't** call terminal text capture a visual regression test.
