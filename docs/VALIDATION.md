# updev validation

この手順は Go 版 `updev` の v1 コアと `brew` テンプレ連携の最低限の
回帰確認用です。実機 state を変更するコマンドは一時 root で確認する。

## 前提

- `chezmoi apply` 済み
- `~/.config/sheldon/aliases.zsh` を読み込んだシェルで実行

## Tool-local tasks

```bash
mise -C tools/updev run check
mise -C tools/updev run test
mise -C tools/updev run vet
mise -C tools/updev run mod-verify
mise -C tools/updev run test-fresh
```

## 1) 日常更新と inventory 表示

```bash
updev --dry-run
updev --no-color --dry-run
updev --config /tmp/missing-updev-config.toml check --manifest-only
UPDEV_TUI=0 updev --dry-run
updev last
updev last --section inventory --status attention --details
updev last --section logs --details
updev last --format json
updev list
UPDEV_TUI=0 updev list
UPDEV_TUI=0 updev list --query git --details
updev list --query git
updev list --provider npm
updev list --category runtime
updev list --status missing
updev list --status attention
updev list --status profile-mismatch
updev list --query git --details
updev list --query git --limit 5
updev list --format json --provider npm
updev list --provider manual --query Pencil
updev list --format json --provider manual --query Pencil
updev inventory scan --provider manual
updev inventory scan --provider manual --format json
updev inventory plan --provider manual
updev inventory plan --provider manual --action needs-review
updev inventory plan --provider manual --format json
updev inventory review --provider manual
updev inventory review --provider manual --format json
updev inventory review --provider manual --action list
updev inventory review --provider manual --action accept --query '<candidate>'
updev inventory review --provider manual --action update --query '<override>'
updev inventory review --provider manual --action ignore --query '<candidate>'
updev inventory review --provider manual --action remove --query '<override>'
updev inventory render --report manual-apps
updev inventory render --report manual-apps --format json
updev status
updev status --refresh
updev check
updev check --manifest-only
updev fix mise --dry-run
updev check --dependencies
updev doctor dependencies
updev doctor dependencies --format json
```

期待:
- `updev --dry-run` は更新コマンドを実行せず compact dashboard を表示し、
  full inventory を流さない
- `--no-color` は human text の ANSI color を抑止し、`--config` は
  `UPDEV_CONFIG` と同じ TOML 設定 path override として機能する
- `UPDEV_TUI=0` では TTY selector を開かず、従来の text 出力だけを返す
- 日本語環境（`UPDEV_LANG=ja` または OS/locale が ja）では human TTY の
  helper/selector/detail 文言が日本語になる。`UPDEV_PROGRESS=0` 以外の TTY
  では、遅い inventory 読み込みや update safety check 中に stderr の spinner
  が動き、完了時に消える
- `updev --dry-run` を連続実行すると、同じ Homebrew / VS Code update
  candidate set では 2 回目以降の safety evidence が
  `updev update safety cache` 由来として表示され、policy は再評価される
- TTY の plain `updev list` で `Provider filter` → `mise` を選ぶと、
  grouped table のまま mise tool rows の長い説明を `Enter` / `Space` /
  click でその場に展開でき、`j/k`・矢印・PgUp/PgDn・wheel で移動できる。
  `/` で表内 text filter、`x` で filter clear、`b` または Backspace で
  filter clear 後に list hub へ戻れる
- TTY では `updev --dry-run --interactive` からも detail view を開き、
  `Enter` / `Space` / click で security evidence や update logs を展開でき、
  `/` / `x` / PgUp/PgDn / wheel が同じように使える
- TTY update hub では `Updates filter` で provider/status/query、`Security
  filter` で provider/decision/query の絞り込みを選択できる
- `updev last` は直近 update report の compact dashboard を再表示する
- `updev last --section ...` は cached report から inventory / logs /
  security などを再実行なしで drill-down できる
- `updev last --format json` は cached report を parse 可能な JSON で返す
- `list` は grouped inventory を表示する
- `--status attention` は missing/extra/drift/held/blocked/error/unavailable
  だけに絞り、通常の `list` 先頭には summary が出る
- `--status profile-mismatch` は inactive profile/scope 由来の drift だけを
  絞り込む。該当なしなら空の focused list を返す
- `--category` は Brewfile category や mise/runtime などの tool section を
  絞り込める
- `--details` は表で切れた説明を下部 details に展開する
- `--limit` は section ごとの行数を抑え、隠れた行数を表示する
- `--provider manual` は `docs/apps.md`、structured overrides、read-only
  `.app` bundle evidence を表示し、通常の inventory / update / sync /
  security 対象には混ぜない
- `mas` が PATH にある場合、`--provider manual` は短い timeout で
  `mas list` を読み、Mac App Store evidence として review candidate /
  generated preview に含める
- `updev inventory scan --provider manual` は normalized manual evidence と
  review candidates を read-only で返し、live-only manual app があれば
  exit `2` になる
- `updev inventory review --provider manual` は live-only manual app があれば
  exit `2` で review-needed を返し、default では書き込みなしで configured
  overrides TOML 向けの preview を表示する。`--action accept|edit|ignore`
  は `--query` で選択した単一 candidate を configured overrides TOML に
  append する。既存 override と identity が一致する場合は duplicate append
  を止める。`--action list|update|remove` は既存 override を確認・編集・削除
  する
- `updev inventory plan --provider manual` の JSON item は
  `suggested_provider` / `install_hint` / `review_url` /
  `command_preview` を返し、vendor installer は実行せず preview に留める
- `updev list` / bare `updev` の TTY selector は v0.5.x polish gate で確認済み。
  `updev --dry-run --interactive` の manual default route、Back/Exit
  post-navigation、`updev list --interactive` の clean exit は pty smoke で確認し、
  Home と in-view filter は browser model tests で確認する
- `updev inventory render --report manual-apps` は書き込みなしで Markdown
  preview を返し、JSON では target path / content / review candidates を返す
- `status` / `check` は inventory cache を使って provider summary、
  category meaning、変更なし/変更ありを compact に表示する。`--refresh`
  で fresh read できる
- global と project-local の mise manifest に `latest`、version 欠落、Node
  以外の `lts` がある場合は blocked の mise manifest item として
  file/line/reason が出る
- `updev check --manifest-only` は live provider を叩かず manifest hygiene だけを
  確認し、`mise run dot-review` から実行される
- `updev fix mise --dry-run` は `latest` entry の解決候補を表示するだけで
  manifest を書き換えない。実際に書く場合は `--apply` が必要
- `updev check --dependencies` / `updev doctor dependencies` は Homebrew/mise
  と任意 scanner の local CLI/version/JSON contract を read-only で確認し、
  optional scanner 不在は unavailable として扱う。required JSON contract の
  変更は drift として返る
- JSON 出力は parse できる

### v0.5.x manual inventory polish dogfood

manual/vendor inventory UX の回帰確認では、以下を実機または fixture test で確認する。

```bash
updev inventory plan --provider manual --limit 12
updev inventory plan --provider manual --action open-vendor --limit 12
updev inventory plan --provider manual --action adopt-brew --limit 12 --format json
updev inventory plan --provider manual --action adopt-mas --limit 12
updev inventory plan --provider manual --action needs-review --query '<candidate>' --format json
updev inventory review --provider manual --query '<candidate>'
updev inventory review --provider manual --format json --query '<candidate>'
updev --dry-run --interactive
updev list --interactive
```

一時 write smoke は実 override file を触らず、`/private/tmp` root と一時
`XDG_CONFIG_HOME` を使う。Codex sandbox では macOS の default `TMPDIR` が
書き込み不可の場合があるため、`mktemp -d /private/tmp/updev-review.XXXXXX`
を使う。

期待:
- `plan` の compact text は action / candidate / reason / next step が読み取り
  やすく、JSON に切り替えなくても次の確認先を判断できる
- `open-vendor` rows は可能な限り `review_url` 付きで、URL が無い場合も
  evidence gap が次作業として分かる
- JSON は `--limit` に関係なく全件を返す。text では `--limit` が表示行数を
  抑える
- `adopt-brew` rows の `command_preview` は `brew info --cask ...`、`adopt-mas`
  rows は `mas lookup ...` または `mas search ...` で、外部 installer 実行を
  示唆しない。実機に `adopt-brew` 候補がない場合は fixture/unit test の
  Homebrew cask evidence で確認する
- `review --query '<candidate>'` は exact match の候補、preview、実行可能な
  `accept` / `edit` / `ignore` next command、既存 override がある場合の
  update/duplicate/skip 方針を人間が判断できる
- `review --action accept|edit|ignore` の write smoke は実 config ではなく
  一時 root / 一時 `XDG_CONFIG_HOME` で実行する。実 override file に書く場合
  はユーザー承認を取る
- TTY smoke は `updev --dry-run --interactive` が manual review plan を default
  route にすること、Back/Exit post-navigation、`updev list --interactive` の
  clean exit、日本語 text/table rendering を確認する。Home と in-view filter
  は `TestBrowserModelsExposeNavigationActions`,
  `TestDetailBrowserFiltersRowsInPlace`,
  `TestToolTableBrowserFiltersRowsInPlace` で同じ browser key handling を確認する
- review-needed の exit `2` は正常扱い。`go run . ...` で確認する場合は
  `exit status 2` と表示される

## 2) sync / add / remove / rollback

```bash
root=$(mktemp -d)
mkdir -p "$root/dot_config/mise"
printf '{{ if has "personal" .profiles }}\n{{ end }}\n' > "$root/Brewfile.tmpl"
printf '[tools]\ngo = "1.26.3"\n' > "$root/dot_config/mise/config.toml"
state="$root/state"

XDG_STATE_HOME="$state" updev add --root "$root" \
  --provider brew --kind brew --category personal jq --format json
XDG_STATE_HOME="$state" updev remove --root "$root" \
  --provider brew --kind brew jq --format json
XDG_STATE_HOME="$state" updev add --root "$root" \
  --provider mise --version 1.2.3 npm:demo --format json
XDG_STATE_HOME="$state" updev rollback --root "$root" --format json
XDG_STATE_HOME="$state" updev sync --root "$root" --format json
```

期待:
- `add/remove` は `mutationReport` を返し、snapshot / diff /
  rollback command を含む
- `rollback` は最新 snapshot を復元する
- `sync` は drift があれば exit `2`、drift がなければ exit `0`
- 裸の `updev add jq` のような曖昧入力は held で止まり、候補を出す

## 3) backend convergence smoke

```bash
root=$(mktemp -d)
mkdir -p "$root/dot_config/mise" "$root/home"
printf 'brew "git"\n' > "$root/home/Brewfile"
printf 'brew "ripgrep"\nbrew "git"\n' > "$root/Brewfile.tmpl"
printf '[tools]\nripgrep = "15.1.0"\n"cargo:fd-find" = { version = "10.4.2", os = ["macos/x64"] }\n"aqua:sharkdp/fd" = { version = "10.4.2", os = ["macos/arm64", "linux"] }\n' > "$root/dot_config/mise/config.toml"

HOME="$root/home" updev backends plan --root "$root" --format json
HOME="$root/home" updev backends doctor --root "$root"
```

期待:
- finding があれば exit `2`
- `plan` は Homebrew→mise と mise backend rewrite の候補を返す
- `doctor` は mise backend rewrite の候補を返す
- backend recommendation は hardcoded 個別 map だけでなく、
  platform/provider/kind ごとの preference policy に照らして説明できる
- backend findings がある時、daily `updev` の post-update hub と
  `updev list` TTY から backend convergence view へ移動でき、JSON を見なくても
  current / recommended / confidence / command status / OS 条件 / next action
  を確認できる
- JSON findings には `command_names` / `command_status` が含まれ、mise
  backend rewrite では `current_os` / `recommended_os` が OS 条件の引き継ぎ
  判断材料として残る
- どちらも read-only で manifest を変更しない

## 4) security v1 smoke

詳細な scope と exit-code semantics は [SECURITY.md](SECURITY.md) を正とする。
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

## 5) 明示インストール判定回帰

```bash
updev --print-explicit-formulas | head -n 30
```

期待:
- Go provider と同じ明示インストール判定で formula 名が1行1件で表示される
- 依存 formula だけの項目は表示されない

## 6) brewtmplcheck 回帰

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

## 7) brew 直打ち連携（実機確認）

テスト対象 formula を一時導入して反映を確認する。

```bash
UPDEV_BREW_CATEGORY=work brew install <test-formula>
UPDEV_BREW_CATEGORY=work brew uninstall <test-formula>
```

期待:
- install/uninstall 成功後に `Brewfile.tmpl` が更新される
- `brewtmplcheck` 実行で意図した差分のみになる

## 8) 非対話時カテゴリ既定値（必要時）

```bash
UPDEV_BREW_CATEGORY= brew install <test-formula> < /dev/null
```

期待:
- 対話不能時に処理が止まらず、既定カテゴリで処理される
