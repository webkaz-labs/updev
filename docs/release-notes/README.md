# updev release notes

This directory is the source for GitHub Release notes. The tag workflow requires
`docs/release-notes/<tag>.md` and publishes that file as the release body. For
example, tag `v0.5.4` uses `docs/release-notes/v0.5.4.md`.

Before tagging a preview release:

1. Add or update the note for the exact tag.
2. Keep the first heading in the form `# updev <tag>`.
3. State the supported preview scope and experimental scope.
4. Mention install or upgrade paths only when they are valid for that tag.
5. Include validation highlights that a user can use to judge the release.

If the note for the exact tag is missing, the release workflow fails before
publishing. If the workflow is rerun for an existing release, it updates the
release body from the same file and re-uploads assets with `--clobber`.
