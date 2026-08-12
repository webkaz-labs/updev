# Security And Integration Validation

Security gates, explicit package detection, Brewfile/wrapper integration, and
public installer smoke. Return to the [validation index](../VALIDATION.md).

## Security smoke

詳細な scope と exit-code semantics は [SECURITY.md](../SECURITY.md) を正とする。
最低限:

```bash
updev security scan --format json
updev security gate --provider all --format json
updev security review --format json
updev security policy --format json
```

期待:
- JSON 出力は parse できる
- allow/review/hold/block の policy decision が反映される
- scanner が未導入でも unavailable/warning として扱われ、panic しない

## 明示インストール判定回帰

```bash
updev --print-explicit-formulas | head -n 30
```

期待:
- Go provider と同じ明示インストール判定で formula 名が1行1件で表示される
- 依存 formula だけの項目は表示されない

## brewtmplcheck 回帰

```bash
mise run brew-check
mise run brew-check-strict
brewtmplcheck --lenient
brewtmplcheck --strict
```

期待:
- コマンドが正常に動作し、結果が出力される。`--strict` は差分があれば
  exit `2` でよい
- `--strict` は `tap` / `vscode` 差分も検出対象
- `--lenient` は `brew` / `cask` のみ対象
- `brew` 比較は明示インストール判定を基本にしつつ、`brew info` JSONで拾えない例外は「テンプレ記載かつ実インストール済み」を補完

## brew 直打ち連携（実機確認）

テスト対象 formula を一時導入して反映を確認する。

```bash
UPDEV_BREW_CATEGORY=<category> brew install <test-formula>
UPDEV_BREW_CATEGORY=<category> brew uninstall <test-formula>
```

期待:
- install/uninstall 成功後に `Brewfile.tmpl` が更新される
- `brewtmplcheck` 実行で意図した差分のみになる

## 非対話時カテゴリ確認（必要時）

```bash
UPDEV_BREW_CATEGORY= brew install <test-formula> < /dev/null
```

期待:
- categorized Brewfile では既定カテゴリを推測せず、カテゴリ明示を求めて失敗する
- uncategorized Brewfile ではカテゴリなし adoption が末尾追加として扱われる

## 公開インストール smoke

公開済み tag を使って、installer と release asset の checksum flow を確認する。

```bash
tmp=$(mktemp -d /private/tmp/updev-install.XXXXXX)
tag=$(curl --fail --location --silent --show-error \
  https://api.github.com/repos/webkaz-labs/updev/releases/latest |
  sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
TMPDIR=/private/tmp UPDEV_INSTALL_DIR="$tmp/bin" \
  tools/updev/scripts/install.sh --version "$tag"
"$tmp/bin/updev" version
```

期待:
- `checksums.txt` の SHA-256 検証が成功する
- `$UPDEV_INSTALL_DIR/updev` が作成される
- `updev version` が指定 tag を返す
