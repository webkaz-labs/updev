# updev external and manual management

This document tracks the direction for Homebrew-external installers, vendor
managed apps, manual apps, and generated inventory reports. It defines the
provider model and current read-only boundary; implementation history belongs
in git log.

## Goal

Homebrew-external software should be visible in the same review system as
Homebrew and mise without making humans hand-maintain fragile Markdown tables.
The target pipeline is:

```text
provider scan -> normalized inventory -> review candidates -> structured overrides -> generated reports
```

Manual inventory should tell the user what is required, what is installed, who
owns updates, and whether a safe automated path exists. "Required" does not
mean "safe to install automatically".

## Scope

In scope:

- macOS `.app` bundles from `/Applications` and `~/Applications`;
- Homebrew casks and tap metadata;
- Mac App Store receipts / `mas list` where available;
- vendor-managed apps such as Creative Cloud or hardware utilities;
- external installers such as `dmg`, `zip`, `pkg`, AppImage, or vendor download
  pages;
- structured overrides for lifecycle, scope, owner, install hint, license
  notes, and update owner;
- generated Markdown/JSON reports.

Out of scope for the first implementation:

- silent install of paid, privileged, vendor-account, or checksum-missing apps;
- broad Linux/Windows provider support;
- replacing Homebrew casks that already work well;
- storing secrets, license keys, or account credentials.

Treat the first implementation as the macOS scanner for the `manual` provider,
not as the whole manual/vendor inventory model. Linux and Windows should not
reuse the `.app` bundle implementation; they need separate scanner
implementations that feed the same normalized inventory model:

| Platform | Scanner inputs |
|----------|----------------|
| macOS | `/Applications`, `~/Applications`, bundle `Info.plist`, Mac App Store receipts / `mas list`, Homebrew cask metadata |
| Linux | `.desktop` files, Flatpak, AppImage, Snap, distro package manager metadata |
| Windows | Start Menu shortcuts, installed-app registry, winget, scoop, choco, MSIX/Appx metadata |

Keep the upper model OS-independent. Provider/distribution values such as
`manual`, `mas`, `flatpak`, `winget`, and `vendor` describe ownership or
distribution; identity fields normalize name, app id, bundle id, desktop id, or
package id; evidence fields preserve source path, scanner, confidence, version,
and owner; review fields carry `reason_code`, params, and suggested override
fields.

## Data Model

Store only non-obvious classification in overrides. Scan output should remain
generated state, not hand-edited desired state.

Manual/vendor inventory uses a narrow override file. By default it lives at
`~/.config/updev/inventory-overrides.toml` and is used automatically. Users only
need `~/.config/updev/config.toml` when they want to choose a non-default
override path:

```toml
[inventory]
overrides = "~/.config/updev/manual-overrides.local.toml"
```

The initial override schema reconciles manual/vendor app identities with
Homebrew cask names and aliases:

```toml
[[manual.apps]]
name = "Example App"
aliases = ["example-cask", "Example.app"]
category = "Vendor"
detail = "vendor updater"
managed_by = "vendor"
```

Potential override fields:

| Field | Purpose |
|-------|---------|
| `provider` / `kind` / `name` | Stable identity. |
| `source` | URL, bundle id, cask token, MAS id, vendor id, or known alias. |
| `scope` | `work`, `personal`, OS/arch selector, or local-only note. |
| `lifecycle` | `adopted`, `trial`, `candidate`, `local-only`, `deprecated`. |
| `managed_by` | `brew`, `mas`, `vendor`, `manual`, `external`, `mdm`. |
| `update_owner` | Which tool or vendor handles updates. |
| `install_hint` | Human-safe installation guidance. |
| `license_note` | Non-secret entitlement or account note. |

Generated reports should be clearly marked. Manual edits to generated files
should be rejected or surfaced as drift once an apply/write flow exists.

## External Installer Policy

External installers need stronger gates than package-manager providers:

- third-party `curl | sh` installer guidance is blocked by default unless the
  source, version, and checksum/signature path are explicit;
- preferred flow is `download -> checksum/signature verify -> install`;
- missing checksum/signature produces review/hold, not silent install;
- GitHub Release sources should use common version and asset discovery helpers;
- every install/update/uninstall command must support dry-run preview;
- failures are item-scoped so one broken vendor does not hide the rest of the
  report.

## UX

`updev list --provider manual` remains the explicit manual-app view. The default
inventory should not mix manual/vendor rows into package-manager rows unless
the user chooses that view. Within the manual view, Homebrew cask-owned rows are
used as reconciliation evidence but hidden by default. The tool may use fresh
inventory cache or a short `brew list --cask -1` ownership probe for that
reconciliation; explicit `--status brew` or query filters expose the evidence
when the user is investigating ownership.

Current read-only flows:

```bash
updev inventory scan --provider manual
updev inventory review --provider manual
updev inventory render --report manual-apps
```

The interactive `updev` hub may present manual plan rows in an action-capable
detail browser. Mutating choices must still call the same explicit review write
path (`accept`, `edit`, or `ignore`) and ask for confirmation before writing
overrides.

Future gated flows:

```bash
updev external check
updev external plan
```

Mutation should stay gated behind explicit plan/apply style commands. Where no
safe provider exists, the tool should open or print vendor instructions rather
than attempting install.

## Acceptance Criteria

- duplicate cask/manual rows are reconciled by normalized identity;
- ambiguous rows become review candidates rather than hidden assumptions;
- generated `docs/apps.md` can be reproduced from scan output plus overrides;
- unsafe external installers are held with actionable guidance;
- JSON reports expose the same identities, lifecycle, owner, and safety
  decisions as human text.
