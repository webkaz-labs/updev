# TTY Performance And Lifecycle

## Current Boundary

The current model optimizes post-provider review. Real brew/mise commands run
outside the alternate-screen TUI and stream stdout/stderr directly. After they
finish, dashboard/list/last reuse one routed review program where common
navigation no longer exits and restarts separate TUIs.

Manual review and backend convergence are asynchronous post-provider blocks.
The useful dashboard/list shell appears with stable loading rows, then those
blocks refresh in place. Query input, filters, item-scoped detail, and safe
confirmations stay in the same router.

Security, inventory, translation, and other report-building domains still need
explicit partial/cancellation semantics before they may refresh the live
dashboard. Until then they complete the structured report first.

## Readiness And Refresh

- Render root/config/security mode and available report data before optional
  review blocks finish.
- Keep one stable loading/progress row per domain. A completed message replaces
  that row without moving unrelated content or focus.
- Correlate each result with the current request, route, and target. Late stale
  responses cannot overwrite newer state.
- Independent review preparation may run concurrently when it does not change
  behavior or mutation order.
- Timing assertions use readiness/state messages, not arbitrary sleeps or
  machine-specific wall-clock thresholds.

## Logs And Errors

- Provider stdout/stderr remains visible while provider commands run and is
  also available as expanded evidence afterward.
- Generic progress noise does not become an updated/deferred outcome.
- One failed optional domain leaves other usable data visible.
- Loading, partial, unavailable, held, and completed reports are distinguishable
  in both TTY and cached state.

## Cancellation And Exit

- `q`, Back, Esc, and Ctrl+C have documented ownership at each router/input or
  confirmation state.
- Exiting cancels context-aware optional loaders. A canceled result cannot be
  saved as a completed cached report.
- Terminal state is restored on normal exit, cancellation, and error.
- Non-TTY, `--plain`, JSON, and cached `last` behavior remain deterministic and
  do not depend on live TTY refresh.

## Future Streaming Gate

A new domain may stream into the dashboard only after it defines request
correlation, cancellation, partial-report persistence, cache freshness, and
error rendering. Do not hide provider logs behind a live dashboard merely to
show progress earlier.
