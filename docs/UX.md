# updev interactive UX contract

This document defines the target interactive UX for `updev`, `updev list`, and
cached report review. Keep release state in [RELEASE.md](RELEASE.md); keep this
file as the behavior contract and tracking checklist for action-review UX.

## UX Principle

`updev` is an action review hub, not just a list viewer. After an update, a user
should be able to answer three questions without leaving the TTY flow:

1. What changed, stopped, or needs review?
2. Why did it happen, and what evidence supports that state?
3. What is the next safe action, and can it be run from the current context?

`updev list` remains an installed inventory entry point, but it should still
route a focused item to the relevant action domain when updev already knows that
the item needs manual review, backend convergence, security review, or update
evidence inspection.

## Screen Model

1. Bare `updev`
   - Runs the update workflow.
   - Ends in an interactive `updev update <status>` summary hub on TTY and
     stays there until the user chooses a row action or exits.
   - The first screen preserves the old compact summary content and table
     layout: root/security/safety/update/report lines, update outcome, update
     steps, security attention, inventory drift, and top inventory items.
   - The compact summary's table headings and data rows become selectable.
     Selecting update rows opens update details/logs, security rows open
     security evidence, and inventory rows open inventory details.
   - Manual review and backend convergence appear as additional review actions
     after the preserved compact summary when they have work to do.
   - On the update summary hub, `Enter`, `Space`, or `a` opens the focused
     summary row. Domain detail browsers keep `Enter` / `Space` as
     expand/collapse unless that screen explicitly says otherwise.
   - Summary rows may execute final actions only when the row itself has
     enough context; otherwise they route to the domain detail view.
   - Back from a domain detail view returns to the update summary hub, not to
     the old footer selector. Back from the summary hub exits the hub.
2. `updev list`
   - Opens the grouped installed inventory browser first on TTY.
   - `Tab` / `Shift+Tab` switches directly between installed inventory and
     manual app rows while preserving each view's cursor, filter, and expanded
     rows.
   - Back/Home from the top inventory browser opens the domain switcher so
     backend convergence and cached update/security evidence are easy to reach
     without duplicating inventory filter controls.
   - `updev hub` opens that domain switcher directly when a user wants review
     domains first.
   - The installed inventory rows are primarily read-only.
   - Rows should expose routing actions to the relevant action domain when a
     matching manual/backend/security/update review context exists.
   - Row routing actions preserve the focused item identity. Opening backend,
     manual, update, or security review from an installed item shows the rows
     for that item first and Back returns to the original inventory browser.
3. Shared detail browser
   - Is the primary review surface for all action domains.
   - Collapsed rows show status, badges, concise summary, and action count.
   - The focused row always shows `a/1`, `2`, ... action hints before expansion
     when actions exist.
   - Header height remains stable when focus moves between rows with and
     without actions; action hints may appear or disappear, but the body must
     not jump.
   - Expanded rows separate detail, evidence, and actions.
   - Write actions always require confirmation before changing local state.
4. Transition and performance model
   - Screen-to-screen navigation should feel like moving inside one TUI, not
     like repeatedly exiting and starting separate programs.
   - Long-running preparation must not block the first useful screen. Render
     the screen with the data already available, show an explicit loading or
     preparing state for the missing block, and refresh that block when the
     evidence arrives.
   - Progress should be domain-scoped when possible: inventory, backend
     convergence, manual review, update evidence, and security evidence can
     become ready independently instead of forcing one all-or-nothing wait.
   - Independent review data should be prepared concurrently when it does not
     change behavior.
   - The long-term target is a single Bubble Tea router that owns dashboard,
     table, detail, and confirmation views in one `tea.Program`; until then,
     new subprogram transitions are treated as UX debt and should be reduced.

## Current Performance Model

The current baseline optimizes post-report review and post-provider review
loading, not the entire provider execution pipeline.

- `updev`, `updev last`, and `updev list` keep common review navigation inside
  one routed TUI once the first screen is open.
- Manual review and backend convergence preparation are asynchronous review
  blocks. The dashboard/list screen renders with loading rows, then refreshes
  those rows when evidence is ready.
- Query input, value filters, item-scoped detail routing, and safe write
  confirmations are in-router views.
- Real provider updates run outside the alternate-screen TUI and stream logs
  while the provider command runs. Those logs remain available in expanded
  detail rows.
- Non-TTY, `--plain`, and JSON modes stay deterministic and do not use TTY
  streaming behavior.

The current baseline does not yet open a live dashboard while provider update,
security, inventory, or translation work is still running. Those blocks still
produce the structured report first, with startup/progress feedback where
available.

## Future Performance Track

Future streaming work can build on the current post-provider dashboard model
only where partial/canceled reports cannot be confused with completed cached
reports.

- Consider a stable TTY dashboard shell as soon as there is enough context to
  show root/config/security mode and planned provider blocks, but do not hide
  real provider stdout/stderr behind the TUI.
- Convert security, inventory, translation, and other post-provider domains
  into result messages that can refresh the same router only after their
  cancellation and cache semantics are explicit.
- Keep one stable progress row per domain with elapsed time, current state, and
  last useful provider message.
- Preserve provider stdout/stderr for expanded evidence while keeping generic
  progress noise out of outcome summaries.
- Define cancellation semantics for `q`, Back, and Ctrl+C while background
  domains are still running.
- Keep `test-e2e-fast` as the default local TTY loop and reserve full PTY route
  coverage for release gates or explicit dogfood.

## Row Contract

Every review row should expose these fields where evidence exists:

- `status`: one of `updated`, `deferred`, `held`, `review`, `needs-review`,
  `applyable`, `review-only`, `ok`, `error`, or the provider-native equivalent.
- `summary`: one concise line that explains what the user should notice.
- `detail`: what happened or what decision is being requested.
- `evidence`: provider logs, versions, commands, source URLs, release assets,
  policy source, review confidence, or scanner/advisory evidence.
- `actions`: safe actions or routing actions.
- `next`: if no direct action is safe, the next view or command to use and why.

Rows without write actions must not feel like dead ends. They should explain why
the action is unavailable and how to continue review.

## Route-to-Intent Contract

TUI actions should minimize follow-up selection. When a user chooses an action
from a focused row, updev treats that choice as an intent for that item, not as
a request to open a generic domain list.

Each routing action should carry:

- `domain`: security, backend, manual, update, policy, or another review
  domain.
- `target`: provider, kind, name, candidate version, and any stable item key
  already known by the current row.
- `intent`: review, allow, hold, apply, edit, trust, open, or another concrete
  next step.
- `origin`: the source screen, filter, cursor, expanded row, and scroll state.

The routed view should open the matching detail row first, expanded, with the
relevant action sheet visible when a safe action exists. It should not show a
full list and force the user to select the same item again. A domain list is a
fallback only when the source row cannot identify one stable target; fallback
rows must explain why the action could not be item-scoped.

Examples:

- Inventory `sec` on `go` opens `go` security evidence directly, not the full
  security list.
- Inventory `bak` on `ripgrep` opens matching backend convergence findings for
  `ripgrep` directly, not the full backend list.
- Update summary rows for a held package open that package's update/security
  evidence directly.
- Policy actions from security detail open a focused policy action sheet for
  the matching rule or finding.

Write actions still require confirmation. The improvement is that the
confirmation is reached from the focused item detail without an unrelated
intermediate selector.

## Action Domains

Manual app review actions:

- `accept override`
- `ignore local app`
- `edit override`
- `review cask`
- `review App Store`
- `open vendor`
- `generate draft metadata`
- `accept/edit/ignore draft metadata`

Manual app review should support a complete TTY path when structured inventory
enrichment exists: open manual apps, choose an ambiguous row, generate or refresh
draft metadata with the configured agent, inspect the proposed TOML fields,
then accept, edit, or ignore the draft without losing the list context. Agent
generation is a review action, not an automatic side effect of opening the
manual list. Missing agent tooling must degrade to read-only evidence and a
clear next command.

The same flow should support bounded batch enrichment from the manual review
queue. The TUI may send multiple selected or filtered rows to the agent in one
run, then return a draft review list where accept/edit/ignore is still visible
per row and Back returns to the originating manual app context.

Backend convergence actions:

- `rewrite mise backend`
- `remove old mise backend`
- `remove Brewfile ownership`
- `open backend review`

Metadata-inferred backend candidates such as `mise/github` remain review-only
until release assets, version mapping, and official distribution ownership are
verified.

Security actions:

- `allow 7 days`
- `custom allow`
- `allow and rerun item`
- `hold`
- `open security review`

Update/log actions:

- `open update evidence`
- `open security review` for held security steps
- `open backend review` or `open manual review` when the update evidence points
  to those domains
- `preview mise bump` for pinned-version opportunities. The action must be
  scoped to the focused tool, show the equivalent dry-run command
  (`mise upgrade --bump <tool>`), require confirmation, and refresh the report
  after success. Held or review-needed bump rows route through security review
  before the write action is available.
- `apply safe mise bumps` when `[update.mise_bump].mode = "safe"` and at least
  two safe candidates are present. The action opens a review table, runs a
  scoped dry-run preflight, asks for confirmation, then runs
  `mise upgrade --bump <tool...>` for exactly the reviewed safe set.
- `auto mise bumps` when `[update.mise_bump].mode = "auto"`. The dashboard must
  show the automatic bump plan and final result as normal update evidence, not
  as a hidden side effect. Held/review/blocked candidates stay visible with
  their review route.

Installed inventory actions:

- Write actions are not executed directly from the installed inventory table.
- Routing actions are allowed and expected: open manual review, backend review,
  security review, mise bump opportunities, or update evidence for the focused
  item.

## Completed UX Baseline

The routed UX baseline is complete only while these user-visible behaviors keep
holding in the real TTY, not only in renderer tests:

1. Update review clarity
   - `updev --dry-run --interactive` opens the old compact summary as the
     selectable first screen, not a replacement card/list view.
   - The selectable summary still shows root/security/safety/update/report
     lines plus the update, security, and inventory summary tables.
   - Updated, deferred, held, skipped, errored, and review-needed rows are
     visually distinct in collapsed detail rows.
   - Focused rows with actions show `a/1`, `2`, ... hints before expansion.
   - Expanded rows keep meaningful provider log newlines and show detail,
     evidence, and actions as separate blocks.
2. Manual app review operability
   - The post-`updev` manual app review path is never a Back-only dead end.
   - Manual rows with safe write paths expose accept, edit, or ignore actions
     from details and require confirmation before writing.
   - Cask, App Store, and vendor rows that cannot be safely written explain the
     missing evidence or next command.
3. In-dashboard action routing
   - Summary table headings and rows route directly to update evidence, manual
     app review, backend convergence, security review, installed inventory, or
     logs where that row has enough context.
   - `Enter` on a focused summary row follows the same primary action as
     `a/1`, so the update result can be operated as a summary-first hub
     without requiring action-key memorization.
   - Footer selectors remain available only as a fallback for cross-domain or
     filter flows that do not fit inside a single dashboard row; they must not
     appear automatically after the dashboard summary.
4. Installed inventory routing
   - `updev list --interactive` remains an inventory browser, but rows route to
     the relevant action domain when matching manual/backend/security/update
     evidence exists.
   - Routing is item-scoped: a backend action on `ripgrep` opens matching
     backend findings for `ripgrep`, not the full backend list. Rows without
     matching evidence do not show misleading domain actions.
   - Default manual app inventory suppresses Homebrew-managed GUI apps while
     keeping cask evidence available through explicit filters.
5. Backend convergence safety
   - Safe mise backend rewrites and covered old-entry removals can be applied
     after confirmation from the detail browser.
   - Metadata-inferred `mise/github` moves stay review-only until release asset,
     version mapping, platform, and distribution ownership evidence are strong
     enough to apply.

## Accepted Routed UX Baseline

This baseline was accepted in `v0.5.7` and remains an invariant for later TUI
changes.

- [x] Shared table browser rows can carry TTY-only routing actions and expose
  `a/1`, `2`, ... action hints like the detail browser.
- [x] `updev list --interactive` brew/mise inventory rows can route to backend
  convergence review instead of staying purely read-only.
- [x] `updev list --interactive` manual evidence rows can route to manual app
  review instead of staying purely read-only.
- [x] `updev list --interactive` item rows expose routing actions when a focused
  item matches security or update evidence.
- [x] `updev list` starts with the installed inventory browser, while
  `updev hub` opens the review-domain switcher directly.
- [x] `updev list` can switch between installed inventory and manual app rows
  with `Tab` / `Shift+Tab` without visiting the domain switcher.
- [x] Installed inventory expanded rows show enough evidence to explain why a
  routing action exists.
- [x] Filtered `updev list --interactive` views propagate row routing actions
  back to the list hub instead of swallowing them.
- [x] Dashboard rows route to domain detail views without forcing users back to
  a footer-only selector for common paths.
- [x] Bare `updev` keeps the dashboard as the top hub; Back from child detail
  views returns to the dashboard instead of opening the old selector.
- [x] Domain detail rows with safe write actions can execute those actions via
  `a/1`, `2`, ... after confirmation.
- [x] Domain detail rows without safe write actions explain the missing evidence
  or next command instead of appearing Back-only.
- [x] Update detail rows distinguish updated, deferred, held, skipped, errored,
  and review-needed states with status color and compact badges.
- [x] Provider stdout/stderr keeps meaningful newlines in expanded details.
- [x] Installed inventory row actions carry item identity and open item-scoped
  manual/backend/update/security detail rows before returning to the inventory
  browser.
- [x] Shared table/detail browsers reserve the focused-action hint line so body
  rows do not jump when focus moves between actionable and non-actionable rows.
- [x] Bare `updev` prepares manual review and backend convergence plans in
  parallel before opening the dashboard.
- [x] Bare `updev` uses a single routed TUI model for the update dashboard and
  common dashboard/table/detail review screens, so logs, inventory, manual
  review, backend convergence, security, and full-report detail routes do not
  repeatedly leave and restart the alt-screen.
- [x] `updev list` uses a routed TUI model for the installed inventory, manual
  app inventory, backend convergence, cached update/security evidence, and
  compact detail views, so common list-domain navigation no longer repeatedly
  leaves and restarts the alt-screen.
- [x] TTY dogfood covers `updev --dry-run --interactive`,
  `updev list --interactive`, and `updev last --section security`.
- [x] Value-based filter selectors for `updev list`, update evidence, and
  security evidence stay inside the routed TUI model.
- [x] Query input for `updev list`, update evidence, and security evidence stays
  inside the routed TUI model.
- [x] Safe write-action confirmation views for manual overrides, backend
  convergence rewrites/removals, and security allow/hold rules stay inside the
  routed TUI model.
- [x] External review/open actions and manual override edit remain explicit
  non-goals for routed execution because they intentionally run provider
  commands, `open`, or an editor.
- [x] `updev list` opens the installed inventory immediately and refreshes
  backend convergence evidence/actions asynchronously when that background
  block becomes ready.
- [x] `updev list` surfaces review-evidence counts in text output and TUI list
  titles (`upd`, `sec`, `bak`) so empty confirmation columns can be
  distinguished from missing evidence wiring.
- [x] `updev hub` does not precompute backend convergence counts before showing
  the selector. It exposes a loading backend choice and lets the routed view
  refresh asynchronously.
- [x] Bare `updev` and `updev last --interactive` open the update dashboard
  before manual review and backend convergence plans finish, then refresh those
  review-action blocks asynchronously.
- [x] `updev last --plain`, `--no-interactive`, and JSON cached-report sections
  remain cache-only. They must not run manual review, backend convergence,
  security, translation, or provider scans to enrich plain details; derived
  evidence belongs in cached reports or in TTY async refresh after the first
  screen is visible.
- [x] Dry-run update reports do not replace the last real update report. This
  preserves cached update/security evidence for `updev list` confirmation
  badges while still keeping the dry-run report inspectable from its own cache
  file.

## Release Boundary

The routed dashboard/list/detail flow is the accepted baseline. Current and
future provider gates should build on the same action-review model instead of
creating separate flows.

## v0.7.3 Support Label TUI Rules

`v0.7.3` makes support labels visible in the TUI only when they change a human
decision. Do not repeat every catalog label on every row.

- Provider rows and provider details may show support labels. Hide
  `supported_preview` in dense lists unless there is spare space; show
  `experimental`, `compatibility`, and `deferred` as compact badges because they
  change user expectations.
- `doctor dependencies` text/detail rows show the exact compatibility-ledger
  `support_label`, because those rows review provider command/API contract
  support.
- Manual inventory evidence may show inventory-source support labels in detail
  metadata only. Avoid adding another always-visible list column.
- Command and report surface labels belong in a support-catalog route reachable
  from both `updev hub` and the list hub. They are not duplicated on unrelated
  update/inventory dashboards.
- The full support catalog route keeps the same filterable TUI pattern as other
  high-information lists: surface filter, label filter, query filter,
  expandable details, and Back/Home restoration.
- The support-catalog route is the only place where command and report support
  labels are shown as first-class rows. Other dashboards should link to that
  route instead of duplicating the catalog.

## Completed Performance Checklist

- [x] Continue reducing PTY route-suite runtime; fast PTY smoke is the default
  local TTY check, while the full route suite remains for release gates or
  explicit dogfood. Local Go tasks set a tmp-based `GOCACHE` by default so
  sandboxed runs do not fall back to a blocked user cache.
- [x] Preserve provider command log visibility: non-dry-run text updates keep
  brew/mise stdout and stderr streaming before the post-update dashboard opens.
  TTY streaming work must not move real provider logs behind an invisible
  alternate-screen view.
- [x] Route post-update manual review and backend convergence loading through a
  shared dashboard context. Exiting the TUI cancels outstanding background plan
  work where the underlying provider supports context cancellation, and
  canceled plan messages become held partial reports instead of completed data.
- [x] Keep provider command execution outside the alternate-screen TUI. The
  dashboard opens after brew/mise logs finish; post-provider review domains can
  refresh there, but provider stdout/stderr is never hidden behind the TUI.
- [x] Define the cancellation/partial-result boundary for future
  post-provider security, inventory, and translation background TTY domains:
  those domains may reuse the dashboard refresh model only after they can
  distinguish canceled partial reports from completed cached reports. Until
  then, provider execution remains outside the alternate-screen TUI and cached
  reports stay deterministic for `last`, `--plain`, and JSON output.
- [x] Keep non-TTY, `--plain`, and JSON output mode contracts deterministic
  while adding post-update TTY streaming behavior. JSON output and `--plain`
  never open the update hub, while non-dry-run text still streams provider logs.

## Manual Inventory Portability Track

- [x] Stop treating repository-local `docs/apps.md` as an implicit public
  desired-state source. Keep Markdown input only behind explicit configuration
  or repository-local compatibility.
- [x] Add structured manual inventory sources such as
  `[inventory.manual].sources = ["~/.config/updev/manual-apps.toml"]`.
- [x] Add a draft status model so agent-generated app metadata can be displayed
  and reviewed without becoming desired state.
- [x] Add CLI agent enrichment actions that can generate single-row or bounded
  batch draft TOML from filtered manual review candidates.
- [x] Add TUI actions from the manual app list/detail flow for generating,
  viewing, accepting, editing, and ignoring draft metadata.
- [x] Add bounded batch enrichment from filtered manual review rows while
  keeping accept/edit/ignore decisions per generated draft.
- [x] Preserve item-scoped routing and Back behavior when manual enrichment
  opens agent draft review, structured source editing, or generated report
  preview screens.
- [x] Keep `updev inventory render --report manual-apps` as the Markdown output
  path so README-style reports can be regenerated from structured sources and
  live evidence.
