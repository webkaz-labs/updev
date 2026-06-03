# updev public preview publishing

This document defines the preview-era publishing flow. During preview,
`tools/updev/` in this dotfiles repository remains canonical. The public
repository under `webkaz-labs` is an exported split, not a place for direct
development.

## Naming checkpoint

Do not create the public repository until the command/repository name is
confirmed. The current working name is `updev`, but it should pass these checks
first:

1. **Scope clarity**: the name should communicate package/tool update and
   inventory work, not OS settings or general dotfile management.
2. **Searchability**: avoid names that are too generic to find in GitHub,
   Homebrew, shell history, or issue searches.
3. **Command ergonomics**: the command should remain short enough for daily use.
4. **Migration cost**: changing the name later touches the binary name, config
   directory, cache directory, docs, shell wrappers, and Homebrew formula.
5. **Public boundary**: the name should still make sense for a macOS/Homebrew/mise
   public preview while Linux/Windows providers remain experimental.

Candidate decision table:

| Candidate | Pros | Risks |
|-----------|------|-------|
| `updev` | Existing command, short, already dogfooded. | Slightly opaque; may read as internal jargon. |
| `devup` | More obvious "developer updates" reading. | Name collisions are more likely; migration cost is immediate. |
| `toolup` | Clear package/tool update angle. | Less natural as a noun; may undersell inventory/check workflows. |

Default until a better name wins: keep `updev` and publish
`webkaz-labs/updev` as a preview repository.

## Preview repository model

- Canonical source: `dotfiles/tools/updev`.
- Public preview repository: `webkaz-labs/<tool-name>`.
- Public repository edits: avoid direct edits. Fix the canonical source, then
  re-export.
- Public support statement: macOS/Homebrew/mise is the supported preview scope;
  Linux and Windows providers are experimental until validated on real runners
  or machines.
- Public privacy statement: document scanned metadata, cache/report locations,
  redaction rules, and optional network calls before tagging a preview release.

## Export

Create or update a local checkout of the public repository, then export:

```bash
tools/updev/scripts/export-public.sh --dry-run \
  --module github.com/webkaz-labs/updev \
  ../updev-public

tools/updev/scripts/export-public.sh \
  --module github.com/webkaz-labs/updev \
  ../updev-public
```

The export script:

- copies the portable Go module surface;
- includes `.github/workflows/` for CI and tag-based binary releases;
- excludes dotfiles-only agent docs and `legacy/`;
- rewrites `dotfiles/updev` imports to the selected public module path;
- uses `rsync --delete` so removed files disappear from the public checkout.

If the final name is not `updev`, change only the `--module` value and public
repository name. Do not rename the canonical command/config/cache paths until a
separate migration plan exists.

## Public repository creation

After the name is confirmed:

```bash
gh repo create webkaz-labs/updev \
  --public \
  --description "Preview CLI for macOS/Homebrew/mise package and developer-tool updates" \
  --clone=false
```

Then initialize a checkout, run the export, and commit:

```bash
git clone git@github.com:webkaz-labs/updev.git ../updev-public
tools/updev/scripts/export-public.sh --module github.com/webkaz-labs/updev ../updev-public

git -C ../updev-public status -sb
git -C ../updev-public diff --check
go -C ../updev-public test ./...
go -C ../updev-public vet ./...
go -C ../updev-public mod verify
git -C ../updev-public add .
git -C ../updev-public commit -m "chore: export updev public preview"
git -C ../updev-public push origin main
```

Use `gh` for GitHub operations and confirm the active account before creating
the repository, pushing, or tagging.

## Preview release gate

Before a public preview tag:

1. `go test ./...`, `go vet ./...`, and `go mod verify` pass in the public
   checkout.
2. `go install github.com/webkaz-labs/updev@<tag>` works from a clean temp
   directory.
3. GitHub Actions CI passes on `main`, and the tag-based release workflow
   publishes `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`, and
   `windows/amd64` archives plus `checksums.txt`.
4. README shows supported platform scope, install/uninstall, first dry-run,
   config path, cache/report paths, privacy boundaries, and experimental
   provider labels.
5. Dotfiles integration smoke builds or installs the public artifact and runs a
   local read-only command.
6. The tag/release notes state `public preview`, not stable `v1.0.0`.
