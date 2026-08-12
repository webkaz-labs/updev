# Navigation And Route-To-Intent

## Screen Model

### Update summary

- Bare `updev` runs the update workflow, then stays on an interactive summary
  hub until the user chooses a row or exits.
- The summary keeps root/security/update/report context, update outcomes,
  security attention, inventory drift, and review actions visible.
- Selecting a summary row opens its scoped update, security, inventory, manual,
  backend, log, or full-report destination. It never starts a timed automatic
  transition.
- Enter, Space, or `a` opens the focused summary row. Direct summary shortcuts
  are `i` for installed inventory, `m` for manual apps, `b` for backend
  convergence, `s` for security, and `u` for update evidence. A shortcut with
  no matching rows reports an explicit empty state and never opens an
  unrelated list. `q` exits the summary hub.

### Installed inventory

- `updev list` opens grouped installed inventory first.
- Tab and Shift+Tab switch installed and manual-app rows while preserving each
  view's cursor, filter, expansion, and scroll.
- Home/Back from the top browser opens the domain hub. `updev hub` opens it
  directly.
- A row exposes manual/backend/security/update/apply routing actions only when
  matching evidence exists.
- Item actions preserve identity, open matching findings first, and return to
  the same inventory state.

### Shared detail and confirmation

- Domain detail is the primary review surface. Collapsed rows show status,
  compact badges, summary, and action count.
- Enter/Space expands unless the screen explicitly presents Enter as the
  primary action. Expanded rows separate concise detail, evidence, and actions.
- Arrow keys can focus actions and Enter runs the focused action. `a` and
  numbered keys invoke the same actions as shortcuts.
- Confirmation names one exact item and mutation, defaults to cancel, and
  returns to the originating detail. Success refreshes that item before return.

## Route-To-Intent

A routing action represents an intent for the focused item, not a request to
open a generic domain list. Carry:

- `domain`: security, backend, manual, update, apply, policy, or another review
  owner;
- `target`: provider, kind, name, candidate version, and stable item key;
- `intent`: review, allow, hold, apply, edit, trust, open, or another concrete
  next step;
- `origin`: source route, filter/query, cursor, expansion, scroll, and action
  focus.

The target view opens the matching row first, expanded when useful, with the
relevant action visible. A full list is a fallback only when the source cannot
identify one stable target; explain that fallback in the source row.

Examples:

- inventory `sec` on `go` opens `go` security evidence;
- inventory `bak` on `ripgrep` opens matching backend findings;
- a held update summary row opens that package's update/security evidence;
- a drift row opens the matching provider/status-filtered inventory;
- a policy action opens the matching finding/rule action, not all policy rows.

## Input Precedence And Return

Input precedence is confirmation/modal, active text input, focused action,
focused row, then global keys. Movement never invokes an action. Help and
focused-action hints reserve stable height so moving between rows does not move
the body.

Returning from any child restores the complete origin state. If refreshed data
removes the item, focus the nearest surviving row and show a concise refreshed
state rather than leaving an invalid cursor or blank screen.

In child/detail browsers, `b` or Backspace returns to the immediate origin and
`h` returns to the dashboard root. These keys do not replace item actions or
the summary shortcuts above.
