# Release Workflow Validation

GoReleaser configuration, archive, checksum, and release-note checks. Return to
the [validation index](../VALIDATION.md).

## GoReleaser release workflow

release workflow や `.goreleaser.yml` を変更した場合は、公開 repo に export
する前に canonical 側で GoReleaser 設定と snapshot artifact を確認する。

```bash
mise -C tools/updev run actionlint
mise -C tools/updev run goreleaser-check
mise -C tools/updev run goreleaser-snapshot
```

期待:
- `.goreleaser.yml` が GoReleaser v2 schema として valid
- GitHub Actions workflow が pinned actionlint で valid
- `dist/checksums.txt` が生成される
- archive 名が install script の期待する `updev_<tag>_<os>_<arch>` 系を維持する
- Windows だけ `.zip`、macOS/Linux は `.tar.gz`
- release notes は `docs/release-notes/<tag>.md` から GitHub Release body に渡す
- tag workflow は reusable CI workflow を先に完了し、`updev version` と tag
  が一致しない場合は GoReleaser の前に失敗する
