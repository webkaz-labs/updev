# Trial And Manual State

Trial/manual app state, overrides, and generated report compatibility. Return
to the [data-model index](../DATA-MODEL.md).

## Trial And Manual State

`adopted` entries are deployment candidates. `trial` entries are visible in
status/review but are not installed on other machines. `local-only` entries
live in local state. `candidate` entries can exist in a catalog without being
applied. Promotion from trial to adopted needs an explicit command and focused
diff.

Manual apps are part of reproducibility, but "listed as required" is not the
same as "safe to install automatically". UI and reports must preserve that
distinction.

Manual/vendor live discovery is platform-specific. The shared inventory model
should stay OS-neutral, but scanners are per platform:

- macOS scanner evidence comes from `.app` bundles, bundle `Info.plist`, Mac App
  Store receipts / `mas list`, and Homebrew cask metadata.
- Linux experimental scanner evidence comes from `.desktop` files, Flatpak
  metadata, Snap directories, and AppImage files. Distro package metadata is
  still future provider work.
- Windows experimental scanner evidence currently comes from `winget export`
  JSON fixtures. Installed-app registry entries, Start Menu shortcuts,
  MSIX/Appx metadata, scoop, and choco remain future provider work.

All platform scanners should normalize into the same identity/evidence/review
shape:

| Layer | Examples | Rule |
|-------|----------|------|
| Provider | `manual`, `mas`, `flatpak`, `winget`, `vendor` | Installation/update owner or distribution channel. Keep this separate from OS-specific scanner names. |
| Identity | display name, normalized name, app id, bundle id, MAS id, desktop id, package id | Use the strongest stable id available; names are fallback and matching hints. |
| Evidence | source path, scanner name, review/source URL, confidence, version, owner/update provider, provider-native metadata | Preserve where the fact came from so generated reports and review candidates are explainable. |
| Review | `reason_code`, `remediation_code`, confidence, params, suggested override fields | Ambiguous or unsafe rows become review candidates instead of silent desired state. |
