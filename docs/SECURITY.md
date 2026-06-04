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

- `updev update` runs with security evidence enabled by default, shows what was
  held or skipped, and holds risky Homebrew mutations in strict mode;
- VS Code extension safety evidence exists as an opt-in path;
- `updev security scan` collects normalized evidence from package inventory,
  project manifests, provider-native audits, selected scanners,
  registry/repository posture, and local policy;
- `updev security gate` evaluates pending Homebrew update candidates before
  mutation;
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
   as `brew outdated --json=v2`;
2. skip mutation gates for providers with no pending candidates;
3. build candidate identities from provider, kind, name, installed version,
   candidate version, tap/source URL, repository, tag, and extension publisher
   where available;
4. load raw evidence cache for those identities and reapply current local
   policy every run;
5. fetch only missing or stale evidence;
6. decide `allow`, `hold`, `review`, or `block`;
7. run or skip provider update according to `--security warn|strict|off`.

Mode semantics:

| Mode | Behavior |
|------|----------|
| `warn` | May proceed with stale or missing non-critical evidence, but must show the gap. |
| `strict` | Holds mutation when required evidence is missing, stale, low-confidence, or policy-blocked. |
| `off` | Skips mutation gates and makes that mode visible in the report. |

Default update gate scope:

- Homebrew formula/cask candidates from the pending update set.
- Brewfile-managed VS Code extensions only when opted in.
- Local policy evaluation for those candidates.

mise is not part of the default update gate today. `updev` validates mise
manifests for exact pins, rejects unsafe `latest` entries, and can rewrite
resolvable `latest` pins through `updev fix mise`. When mise
`minimum_release_age` is configured, mise applies that policy while resolving
versions; updev detects and reports that effective policy in dependency
diagnostics and `updev fix mise`. updev does not silently mutate mise config to
add a provider-native age setting.

The provider direction is to converge on one gate vocabulary for every provider:
provider-native age policy is detected as evidence, while updev-owned gates
eventually decide `allow`, `hold`, `review`, or `block` consistently across
Homebrew, mise, VS Code, and future providers.

Default update does not run project-native audits, OSV-Scanner, gitleaks, zizmor,
Trivy, Grype, SBOM generation, cloud/SaaS posture checks, or agent-assisted web
research. Those belong to explicit scan/review commands.

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
| `--provider mise` | mise inventory evidence only. |
| `--provider brew` | Homebrew posture/advisory evidence; VS Code excluded unless opted in. |
| `--provider vscode` | Brewfile-managed VS Code posture/advisory evidence only. |
| `--provider project` | Project native audits and external source/directory scanners. |
| `--provider all` | All high-confidence package ecosystems, Homebrew posture, project audits, selected scanners; VS Code remains opt-in. |

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
  opt-in.
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

## Remaining Roadmap

Keep detailed release ordering in [ROADMAP.md](ROADMAP.md). Security-specific
remaining buckets are:

- broaden advisory coverage and curated mappings;
- add provider-native audits where identities are reliable;
- improve lockfile/SBOM/scanner hardening without making slow scanners default
  update gates;
- harden Homebrew release-age, tap/cask provenance, URL cask warnings, and
  skipped/held reporting;
- harden VS Code extension update gates while keeping VS Code opt-in;
- keep agent-assisted review optional for ambiguous candidates;
- improve policy ergonomics with guided add/edit/list helpers, rule indexes,
  and shadowed-rule diagnostics.

## Limitations

This feature reduces obvious update risk and makes evidence gaps visible. It
does not replace upstream maintainer trust, code review, sandboxing, MDM, EDR,
or human judgment for privileged/vendor installers.
