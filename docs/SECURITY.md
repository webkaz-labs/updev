# updev security design

This document defines security scan/gate/review/policy behavior for `updev`.
It is a security evidence and decision model, not a claim that package managers,
casks, extensions, or vendor installers are safe.

## Entry Points

```text
updev security scan
  Audit installed/desired tools, project manifests, native provider audit
  evidence, selected external scanners, registry/repository posture, and local
  policy.

updev security review
  Convert non-allow findings into stable review candidates, prompts, and
  policy-command hints. It does not perform external web or agent research by
  default.

updev security gate
  Evaluate pending install/update candidates before mutation. Homebrew is
  included by default; VS Code extension gates are opt-in.

updev security policy
  Inspect, add/update, renew, narrow, and dry-run cleanup of local temporary
  allow/review/hold/block overrides.
```

## Completion Goal

The Go security slice is complete enough for the first `updev` Go milestone
when routine updates are safer without turning package management into a full
security platform.

Done for v1 means:

- `updev update` runs with strict security evidence enabled by default, shows
  what was held or skipped, and applies only candidate-scoped safe Homebrew and
  mise mutations while holding risky candidates in strict mode;
- VS Code extension safety evidence exists as an opt-in path;
- `updev security scan` collects normalized evidence from package inventory,
  project manifests, provider-native audits, selected scanners,
  registry/repository posture, and local policy;
- `updev security gate` evaluates pending Homebrew and mise update candidates
  before mutation;
- `updev security review` gives enough package/source/remediation context for a
  human or future agent decision;
- `updev security policy` supports temporary local decisions with reasons,
  expiry, diagnostics, and cleanup.

Non-goals:

- proving a package, cask, extension, repository, or vendor installer is safe;
- auto-allowing casks/vendor installers only because advisory databases are
  quiet;
- making SBOM inventory, cloud/SaaS posture, Prowler, or Syft part of the
  default update gate;
- requiring external web research or an agent runner for normal updates;
- treating unavailable optional scanners as package update failures.

## Update Gate

`updev update` protects the mutation about to happen; it does not audit the
whole workstation or repository.

Update order:

1. discover pending update candidates with cheap provider-native sources, such
   as `HOMEBREW_NO_INSTALL_FROM_API=1 brew outdated --json=v2 --greedy`;
2. skip mutation gates for providers with no pending candidates;
3. build candidate identities from provider, kind, name, installed version,
   candidate version, tap/source URL, repository, tag, and extension publisher
   where available;
4. load raw evidence cache for those identities and reapply current local
   policy every run;
5. fetch only missing or stale evidence;
6. decide `allow`, `hold`, `review`, or `block`;
7. run safe candidates and skip unsafe candidates according to
   `--security warn|strict|off`.

Mode semantics:

| Mode | Behavior |
|------|----------|
| `warn` | May proceed with stale or missing non-critical evidence, but must show the gap. |
| `strict` | Default. Runs only candidates with allow evidence. Candidates with missing, stale, low-confidence, too-new, or policy-blocked evidence are held and remain visible as skipped items. |
| `off` | Skips mutation gates and makes that mode visible in the report. |

Default update gate scope:

- Homebrew formula/cask candidates from the pending update set.
- mise candidates from `mise outdated --json --cd <root>`. GitHub-backed
  entries, explicit `aqua:<owner>/<repo>` entries, mise registry entries whose
  preferred backend is `aqua:<owner>/<repo>`, and selected core runtimes
  (`go`, `node`, `rust`) use GitHub release/tag/ref dates. `npm:`, `cargo:`,
  and `pipx:` entries use registry publish/upload dates plus basic provenance
  checks. vfox-backed entries are not trusted by backend name alone. updev first
  resolves the short name through `mise registry --json`, then looks up the
  concrete backend identity such as `vfox:<owner>/<plugin>` in a data-driven
  provider metadata registry. A provider metadata resolver may use bounded
  sources such as `vendor_release_notes`, `vendor_json`, `github_release`,
  `github_tag`, or `package_registry`; the candidate can become `allow` or
  `hold` only when the resolver returns a release date for the exact candidate
  version. Missing resolver entries, unavailable vendor metadata, parse
  failures, or unknown upstream evidence stay `review`. The initial
  vfox metadata fixture is Google Cloud CLI, where
  `vfox:mise-plugins/vfox-gcloud` resolves release dates from the official
  Google Cloud CLI release notes.
  Candidates newer than the configured minimum age are `hold`; candidates with
  enough evidence and age are `allow`; unsupported or opaque backends remain
  `review`. Strict mode uses scoped
  `mise upgrade --minimum-release-age <Nd> <tool...>` for allowed candidates
  instead of an unscoped provider-wide upgrade when held/review rows also
  exist. The `<Nd>` value comes from updev config/env, so the mutation path does
  not depend on a global mise native setting. updev also compares the normal
  `mise outdated --json` output with an age-disabled
  `MISE_MINIMUM_RELEASE_AGE=0d mise outdated --json` probe, so candidates held
  by mise's native `minimum_release_age` can still appear as item-level holds
  without per-tool `mise latest` probes.
- Homebrew can safely scope upgrades to allowed formula/cask names, so one held
  Homebrew candidate does not block unrelated allowed Homebrew updates. Homebrew
  cannot generally install an older intermediate release for the same
  formula/cask through normal `brew upgrade`; if the latest Homebrew candidate
  is still inside the release-age window, that package stays held until
  Homebrew exposes an older versioned path, the latest ages in, or the user
  explicitly allows it by policy after review. Strict mode must not run
  `brew update` immediately before `brew upgrade`, because refreshing metadata
  first can replace an already-aged local candidate with a brand-new latest
  candidate and cause continuously released packages to stay held forever.
  Instead, package upgrades use `HOMEBREW_NO_AUTO_UPDATE=1` against the gated
  local metadata, then refresh Homebrew metadata after the scoped upgrade. If
  the gate has no Homebrew candidates, strict mode may run metadata-only
  `brew update`, immediately re-run the Homebrew gate without using stale
  outdated caches, and apply newly discovered safe candidates in the same run.
  Newly discovered unsafe candidates remain held. If the pending Homebrew set
  contains only held/review candidates, updev may refresh Homebrew metadata but
  must not run an unscoped `brew upgrade`; the held package names remain visible
  as item-level skipped rows instead of a generic provider-wide block.
- Brewfile-managed VS Code extensions only when opted in.
- Local policy evaluation for those candidates.

The provider direction is to converge on one gate vocabulary for every provider:
provider-native age policy is detected as evidence, while updev-owned gates
eventually decide `allow`, `hold`, `review`, or `block` consistently across
Homebrew, mise, VS Code, and future providers.

Default update does not run project-native audits, OSV-Scanner, gitleaks, zizmor,
Trivy, Grype, SBOM generation, cloud/SaaS posture checks, or agent-assisted web
research. Those belong to explicit scan/review commands.

## Agent-Assisted Review Boundary

Agent-assisted inventory enrichment is optional review tooling. It must not be
part of the default update gate, default scan, or non-TTY JSON output unless the
user explicitly invokes an agent command or enables a non-default agent setting.

Security rules for calling an agent:

- pass only the minimal structured evidence needed for the row under review,
  such as app name, bundle id, version, path basename, provider evidence, and
  source URLs where already present;
- do not send secrets, license data, shell history, browser data, private
  documents, raw plist contents beyond selected public identifiers, or broad
  filesystem listings;
- run the configured command directly from the configured argv list, without a
  shell, with a bounded timeout and no implicit mutation rights;
- treat agent output as untrusted draft data that must be parsed, schema
  validated, and shown for human review before it affects desired state;
- allow bounded batch enrichment only when each row keeps separate evidence,
  provenance, and draft review state;
- require an explicit TUI or CLI action before writing draft TOML, and another
  explicit accept/edit/ignore decision before writing accepted TOML;
- keep missing Codex or other agent commands non-fatal, with a clear fallback to
  live evidence and manual review;
- record provenance (`source = "agent"`, command identity when safe, reviewed
  time, and evidence kinds) without storing prompts that may contain private
  local context.

The safe default is local-only review. If a configured agent performs network
research, the command description and confirmation copy must make that boundary
visible before execution.

## Network And Privacy Boundaries

`updev` security commands collect package metadata and local manifest evidence.
They should not collect secrets, account credentials, license keys, shell
history, browser data, or private document contents.

Network behavior depends on the command and available tools:

| Source | When used | Data sent |
|--------|-----------|-----------|
| Homebrew metadata and taps | Homebrew update/gate/scan paths | Formula/cask names, versions, tap/source URLs. |
| VS Code Marketplace | VS Code extension checks when opted in | Extension publisher/name and installed/current versions. |
| OSV and GitHub Advisory APIs | Security scan/gate when package identity is available | Ecosystem, package name, version, and repository/source identifiers where available. |
| Registry APIs such as npm, crates.io, PyPI | Registry posture checks | Package name and version. |
| External scanners | Explicit scan paths or configured scanner mode | The files each scanner normally inspects; keep slow or broad scanners opt-in. |

Local reports, caches, and policy files live under the configured updev state
and config paths. JSON output may contain package names, versions, source URLs,
local paths to manifests, and advisory identifiers. Redact or avoid sharing
reports if those values are sensitive in your environment.

## Cache Model

Cache raw evidence, not final decisions. Policy changes must take effect
immediately, and release age should be recalculated from cached upstream
timestamps at runtime.

Recommended TTLs:

| Evidence | TTL |
|----------|-----|
| Pending update list | Fresh for real mutation; 5-10 minutes for dry-run/report reuse. |
| Homebrew metadata | 12-24 hours. |
| mise GitHub/registry candidate metadata | 12-24 hours. |
| VS Code Marketplace metadata | 6-24 hours. |
| GitHub release/tag/ref date | 12-24 hours. |
| OSV/GitHub Advisory clean result | 6 hours. |
| Positive advisory finding | 24 hours, then refresh for fixed-version changes. |
| API error, missing metadata, unavailable evidence | 30-60 minutes. |

Cache invalidation must include package identity and candidate version. Text and
JSON output should show cache source and age when evidence is reused.

## Scope Options

Provider scope:

| Option | Scope |
|--------|-------|
| no provider | Installed/desired package scan, project native audits, default external scanners. |
| `--provider mise` | mise pending-update gate from `mise outdated --json`; GitHub/aqua/registry-backed core runtime/npm/cargo/pipx candidates are release-age checked, native `minimum_release_age` holds are detected by batch comparison, and unsupported or opaque backends stay review-held. |
| `--provider brew` | Homebrew posture/advisory evidence; VS Code excluded unless opted in. |
| `--provider vscode` | Brewfile-managed VS Code posture/advisory evidence only. |
| `--provider project` | Project native audits and external source/directory scanners. |
| `--provider all` | All high-confidence package ecosystems, Homebrew posture, project audits, selected scanners; VS Code remains opt-in. |

Homebrew 6 tap trust is handled as security posture, not as an automatic
mutation. `updev doctor dependencies` reads `brew trust --json=v1` with
`HOMEBREW_NO_INSTALL_FROM_API=1` and compares it with non-official entries in
the configured `Brewfile.tmpl`. Security posture rows for non-official taps and
qualified formula/cask entries include the preferred item-scoped `brew trust`
command and a structured `trust_command_argv` array so agents do not need to
parse shell text. `updev`, `updev last`, and `updev list` security detail views
can run confirmed item-scoped `brew trust --formula` / `brew trust --cask`
actions. Whole-tap `brew trust --tap` is offered only from tap findings,
requires an explicit confirmation, and never runs automatically during update.

Scanner scope:

| Option | Scope |
|--------|-------|
| `--scanner auto` | OSV-Scanner, gitleaks, and zizmor when workflow files exist. |
| `--scanner none` | Disable external scanners without disabling direct OSV/API, registry posture, or native audits. |
| `--scanner all` | Run every supported scanner: OSV-Scanner, gitleaks, zizmor, Trivy, and Grype. |
| `--scanner name[,name]` | Run specific scanner aliases such as `osv`, `secrets`, `workflows`, `trivy-fs`, or `anchore-grype`. |

## Decision Model

Security decisions use the same vocabulary across scan, gate, policy, update
text, and JSON:

| Decision | Meaning |
|----------|---------|
| `allow` | Candidate can proceed. |
| `hold` | Skip for now due to release age, vulnerability, missing confidence, or local policy. |
| `review` | Human or agent review is required before trusting the candidate. |
| `block` | Known vulnerability, malware, revoked/disabled package, or explicit deny. |
| `unknown` | Insufficient data; provider policy decides whether this holds or only warns. |

## Release-Age Configuration

Default release-age holds use three days for both Homebrew and updev-owned mise
candidate gates.

```toml
[security.homebrew]
min_release_age_days = 3
outdated_timeout_seconds = 60

[security.mise]
min_release_age_days = 3
```

Environment overrides are available for one-off checks:

```bash
UPDEV_HOMEBREW_MIN_RELEASE_AGE_DAYS=1 updev --plain
UPDEV_BREW_OUTDATED_TIMEOUT_SECONDS=90 updev --plain
UPDEV_MISE_MIN_RELEASE_AGE_DAYS=1 updev --plain
```

`mise-bump` candidate discovery is optional. If `mise outdated --json --bump`
is temporarily unavailable, for example because GitHub rate limits are
exceeded, updev reports the bump gate as held/review instead of failing the
whole update.

For mise subprocesses, updev bridges GitHub credentials from
`UPDEV_GITHUB_TOKEN`, `GITHUB_API_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN`, or
`gh auth token` into the child environment as `MISE_GITHUB_TOKEN`. The token is
not added to the recorded command line or report output.

mise native `minimum_release_age` remains provider evidence. updev reports it in
dependency diagnostics and mise fixer output, but the updev-owned mise gate still
records its own allow/hold/review decision so Homebrew, mise, and future
providers can converge on the same report contract.

Pinned mise bump opportunities use the same release-age/security vocabulary,
but they are separate from ordinary `mise upgrade` candidates. The normal
configuration is:

```toml
[update.mise_bump]
mode = "manual" # off | manual | safe | auto
```

updev does not reimplement mise alias or prefix resolution for bump
eligibility. It trusts mise's JSON `bump` field: rows with `bump: null`, such
as `node = "lts"`, are ignored by the bump gate even if `latest` is newer.
Major-only and minor-only prefix selectors such as `node = "24"` or
`node = "24.16"` follow mise's own `--bump` semantics.

`auto` never disables mise native `minimum_release_age` and never applies rows
whose updev decision is hold, review, block, unsupported, opaque, major-version,
or otherwise uncertain. Automatic and safe-batch modes must run a scoped
`mise upgrade --bump <tool...>` command after dry-run preflight; actual writes
may add `--yes` after updev confirmation. Unscoped bump commands are not part
of the trusted update path. Use `UPDEV_MISE_BUMP_MODE` for a one-off mode
override.

For scoped `npm:*` mise bumps, updev supplies a temporary npm user config that
keeps registry/auth entries and removes npm `min-release-age` settings. npm's
standalone supply-chain gate can stay in the user's normal npmrc, while mise
and updev own release-age enforcement for the provider command.

External advisory matching keeps confidence explicit:

| Confidence | Default behavior |
|------------|------------------|
| exact package ecosystem match | May hold/block according to severity and policy. |
| curated mapping | May hold/review according to mapping confidence. |
| repository-only or URL-only match | Review unless policy says otherwise. |
| missing metadata | Unknown; warn or hold depending on mode. |

## Current Coverage

- High-confidence mise `npm:` / `cargo:` / `pipx:` identities use OSV, GitHub
  Advisory evidence, KEV, EPSS, registry posture, source GitHub posture,
  fixed-version evidence, and binary exposure where available.
- Provider-native and project audits cover npm, pnpm, bun, Cargo, pipx/Python,
  Go, Composer, Bundler, and .NET/NuGet. Maven/Gradle manifests are visible as
  unavailable evidence until a stable native command is chosen.
- External scanners are additive: OSV-Scanner and gitleaks run by default when
  available, zizmor runs for GitHub Actions workflows, and Trivy/Grype are
  opt-in. Scanner/native-audit evidence classifies missing binaries,
  unsupported targets, skipped-by-scope checks, timeouts, rate limits, parse
  failures, report-unavailable conditions, and command errors with structured
  `unavailable_reason` or `error_kind` fields.
- Homebrew scan/gate covers formula/cask metadata, cask URL/homepage
  provenance, tap posture, URL casks, disabled/deprecated metadata,
  release-age evidence where GitHub source data is available, curated advisory
  mappings, and local policy overrides.
- VS Code extensions are opt-in and use Marketplace posture, OSV `VSCode`
  evidence, source repository posture, install/rating/age heuristics,
  update-age gates, and installed/current version comparison when `code` is
  available.
- Local policy is shared across scan/gate/update and reports expired, invalid,
  duplicate, shadowed, missing-reason, missing-expiry, and broad rules.
- Provider command/API drift checks are available through
  `updev doctor dependencies` and `updev check --dependencies`; JSON output
  includes a compatibility ledger, and `--ledger <file>` writes it for local/CI
  tracking without public issue posting by default.

## Remaining Roadmap

Keep detailed release ordering in [ROADMAP.md](ROADMAP.md). Security-specific
remaining buckets are:

- broaden advisory coverage and curated mappings;
- add provider-native audits where identities are reliable;
- improve lockfile/SBOM scanner coverage after the explicit scanner evidence
  contract is stable;
- harden Homebrew release-age, tap/cask provenance, URL cask warnings, and
  skipped/held reporting;
- harden VS Code extension update gates while keeping VS Code opt-in;
- keep agent-assisted review optional for ambiguous candidates.

## Limitations

This feature reduces obvious update risk and makes evidence gaps visible. It
does not replace upstream maintainer trust, code review, sandboxing, MDM, EDR,
or human judgment for privileged/vendor installers.
