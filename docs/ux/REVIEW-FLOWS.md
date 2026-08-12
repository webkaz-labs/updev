# Review Rows And Domain Flows

## Row Contract

Every review row exposes these fields when evidence exists:

- `status`: stable decision/outcome such as `updated`, `deferred`, `held`,
  `review`, `applyable`, `review-only`, `ok`, or `error`;
- `summary`: one concise line stating what needs attention;
- `detail`: what happened or what decision is requested;
- `evidence`: versions, commands, source, provider logs, release metadata,
  policy, confidence, or advisory/scanner evidence;
- `actions`: safe item-scoped actions or focused routes;
- `next`: why direct action is unavailable and how to continue.

Collapsed rows prioritize identity, status, action cue, and short summary.
Expanded detail adds information instead of repeating every collapsed field.
Raw commands and logs stay in evidence unless an error requires them
immediately.

## Manual App Review

Supported item intents are `accept override`, `edit override`, `ignore local
app`, `review cask`, `review App Store`, `open vendor`, `generate draft
metadata`, and `accept/edit/ignore draft metadata`.

Agent enrichment is explicit and review-only. A user may select one row or a
bounded filtered batch, generate draft structured metadata, then accept, edit,
or ignore each draft. Missing agent tooling degrades to read-only evidence and
a concrete next command. Back returns to the originating manual list/filter.

Manual inventory sources are structured and explicitly configured. A
repository-local Markdown app list is not an implicit public desired-state
source. Markdown reports are rendered outputs from structured sources and live
evidence.

## Backend Convergence

Backend rows distinguish applyable from review-only recommendations. Registry
or metadata inference remains review-only until ownership, release assets,
version mapping, architecture, and download source are proven.

Supported intents include `rewrite mise backend`, `remove old mise backend`,
`remove Brewfile ownership`, and `open backend review`.

Adding a pinned mise desired entry, verifying mise ownership, removing old
Brewfile ownership, and uninstalling old provider state are separate confirmed
states. The TUI does not chain them invisibly or send the user through a generic
backend selector between steps.

## Security And Policy

Security rows show allow/review/hold/block decisions, concise reason, evidence
freshness, and release-age timing where available. Item-scoped policy actions
operate on the exact finding and return to it after refresh. Temporary override
actions state reason and expiry before confirmation.

Supported intents include `allow 7 days`, `custom allow`, `allow and rerun
item`, `hold`, and `open security review`.

Unavailable or failed scanner evidence is not equivalent to no findings. It
stays visible with source, failure scope, and safe next action.

## Update, Apply, And Provider Drift

Update detail separates updated, held, skipped, and error outcomes per item.
Provider summary rows may route to a filtered item set, but an item row routes
directly to its own evidence.

Update intents include focused update evidence, security/backend/manual review,
and mise bump review. `preview mise bump` shows the equivalent item-scoped
dry-run and requires confirmation. Safe batch bump applies exactly the reviewed
safe set after preflight; automatic mode records its plan and result as normal
update evidence. Held, review, and blocked candidates remain visible.

Installed inventory exposes routes to these intents but does not execute a
write directly from the collapsed table row.

Brewfile apply installs only missing desired items that pass the item-scoped
gate. Extras remain read-only drift, outdated items stay with update, and
deployment-scope mismatch does not expose an adoption action. Successful local
writes refresh the active view so resolved rows do not remain stale.

## Support And Localization

Support labels such as supported, preview, experimental, unavailable, and
unsupported are explicit text, not color-only decoration. Human labels and
short evidence summaries are localized consistently across summary, list,
last, and detail; stable JSON tokens remain English.

Dense rows show support labels only when they change a decision. Provider
detail and dependency doctor may show the exact label; manual inventory keeps
source support in expanded metadata. Command/report support labels live as
first-class filterable rows in one support-catalog route reachable from the
hub, rather than being duplicated across update and inventory dashboards.
