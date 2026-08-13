# Daily, Inventory, And TTY Validation

Daily update/list/last, manual inventory, performance, and real-terminal
acceptance. Return to the [validation index](../VALIDATION.md).

## 日常更新と inventory 表示

```bash
updev --dry-run
updev --no-color --dry-run
updev --config /tmp/missing-updev-config.toml check --manifest-only
UPDEV_TUI=0 updev --dry-run
updev last
updev last --plain --section inventory --status attention --details
updev last --plain --section logs --details
updev last --format json
updev list
updev hub
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
- missing `~/.config/updev/config.toml` は built-in defaults として扱い、
  default 値を書くだけの config file を自動作成しない
- `UPDEV_TUI=0` では TTY selector を開かず、従来の text 出力だけを返す
- 日本語環境（`UPDEV_LANG=ja` または OS/locale が ja）では human TTY の
  helper/selector/detail 文言が日本語になる。`UPDEV_PROGRESS=0` 以外の TTY
  では、遅い inventory 読み込みや update safety check 中に stderr の spinner
  が動き、完了時に消える
- `UPDEV_LANG=ja updev backends plan` では metadata 推論の `mise/github`
  行が安全な rewrite ではなく候補として読める。detail には現在 platform、
  release asset の状態、一致 asset が日本語ラベルで表示される
- 日本語 TTY text mode の `updev` / `updev list` は、Codex CLI がある場合だけ
  `updev list` の説明 cache を best-effort で更新する。Codex が無い環境では
  English description のまま継続し、`updev doctor dependencies` は optional
  `codex` backend を unavailable として報告するが全体 status は失敗にしない
- `updev --dry-run` を連続実行すると、同じ Homebrew / VS Code update
  candidate set では 2 回目以降の safety evidence が
  `updev update safety cache` 由来として表示され、policy は再評価される
- TTY の `updev list` は installed inventory の grouped table から始まる。
  table では `Enter` / `Space` / click で詳細展開でき、`j/k`・矢印・
  PgUp/PgDn・wheel で移動できる。`/` で表内 text filter、`x` で filter
  clear、`b` / Backspace / `h` で domain switcher に移動できる
- TTY の `updev hub` は domain switcher を直接開き、
  installed/manual/backend/update/security/support/advanced compact/details を
  一覧から選べる。provider/kind/category/status/query の slicing は
  `updev list` の表内 filter または CLI flag で行う
- TTY では `updev --dry-run --interactive` からも detail view を開き、
  `Enter` / `Space` / click で security evidence や update logs を展開でき、
  `/` / `x` / PgUp/PgDn / wheel が同じように使える
- TTY update hub では `Updates filter` で provider/status/query、`Security
  filter` で provider/decision/query の絞り込みを選択できる
- `updev last` は TTY で直近 update report の compact dashboard を再表示する
- `updev last --plain --section ...` は cached report から inventory / logs /
  security などを再実行なしで deterministic text として確認できる。
  `updev last --no-interactive` も TUI を開かない
- `updev last --format json` は cached report を parse 可能な JSON で返す
- `list` は grouped inventory を表示する
- `--status attention` は missing/extra/drift/held/blocked/error/unavailable
  だけに絞り、通常の `list` 先頭には summary が出る
- `--status profile-mismatch` は rendered `~/Brewfile` では有効でない
  source-only category/scope 由来の drift だけを絞り込む。該当なしなら
  空の focused list を返す
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
- `updev list` / bare `updev` の TTY selector は release 前の実端末 acceptance
  対象。`updev --dry-run --interactive` は dashboard-first で開き、dashboard
  row から manual/backend/security/update/logs/details に進めることを確認する。
  dashboard は自動で旧 selector や一覧に遷移せず、domain detail から Back で
  dashboard に戻る。
  自動 PTY smoke は起動・終了・基本描画の補助確認として使えるが、最終 gate
  は実端末での目視と操作確認で閉じる
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
- mise v2026.8.2 bootstrap contract は `status --json` の `packages/tools`、
  `plan --json` の `resources/summary`、`packages apply --dry-run` を確認し、
  command 不在・旧版・malformed JSON は required dependency failure になる
- `mise config ls --json` は active source 一覧として parse され、0件、重複
  source/tool、malformed JSON は required dependency drift になる。report には
  全 active source path、mise の reported order、標準 filename から推論できる
  environment 名、source ごとの tool 件数が出る。任意名の environment は
  `mise run mise-contract-check` の base / `--env` / `MISE_ENV` fixture matrix で
  additive loading と非選択 source の不在を確認する
- aggregate `mise bootstrap status --json` の package map は manager 非固定の
  desired record に正規化される。unavailable manager の package も消えず、
  identity 順の deterministic JSON になる
- `updev doctor package-parity --format json` は active Brewfile と resolved
  mise projection を mutation なしで比較し、formula/cask/tap を
  `match` / `brewfile-only` / `mise-only` に分類する。qualified package の
  implicit tap と active source の `[bootstrap.brew.taps]` も同じ canonical
  identity に正規化される。drift は exit `2`
- `updev doctor package-executors --format json` は同じ snapshot と sidecar
  metadata から `native` / `mise` / `unsupported` を返す。macOS amd64 は
  Homebrew formula/cask/tap を native、macOS arm64 は intentional duplicate
  注記済みかつ manager available の mise desired を mise、Brewfile-only を
  native、Linux amd64/arm64 は available な mise manager を mise とする。
  `--interactive` は同じ report を shared detail browser で表示し、package
  command は実行しない
- `mise run package-authority-check` は最初の `brew/formula/btop` pilot が
  mise-only desired、Intel Mac では native executor、Brewfile 再採用なしで
  あることと、4点 rollback を temp fixture 上で非破壊検証する
- `updev apply brewfile --dry-run --format json` は missing item ごとに
  canonical identity、desired source、executor、security decision、exact argv
  を返す。allow の native/mise item だけが item-scoped argv を持ち、review /
  hold / block / unavailable / unsupported は command を持たない。outdated
  は `updev update` のまま
- Homebrew 6 tap trust check は非公式 Brewfile target が無ければ ok、未
  trust の tap/formula/cask があれば item-scoped remediation 付き drift と
  して返る
- JSON 出力は parse できる

### Manual inventory polish dogfood

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
- TTY smoke は `updev --dry-run --interactive` が dashboard-first で開き、
  dashboard row から manual/backend/security/update/logs/details に進めること、
  domain detail の Back が dashboard に戻ること、`updev list --interactive` の
  clean exit、日本語 text/table rendering を確認する。Home と in-view filter は
  `TestBrowserModelsExposeNavigationActions`,
  `TestDetailBrowserFiltersRowsInPlace`,
  `TestToolTableBrowserFiltersRowsInPlace` で同じ browser key handling を確認する
- 標準の built-binary PTY gate は
  `mise -C tools/updev run test-tui` で実行する。pinned shell-use wrapper が
  isolated HOME/XDG/TMPDIR、semantic journey、terminal snapshot、visual diff、
  clean exit と terminal restoration を一括管理する。
- public CI は shell-use semantic journey を `ubuntu-latest` と Intel
  `macos-15-intel` の matrix で実行する。両 OS で route、focus、action、主要な
  content の invariant を検証し、provider/platform 固有 row を含む完全な terminal
  snapshot は canonical な macOS job だけで比較する。tmux compatibility suite の
  削除は、両 job が同一 commit で green になった後の follow-up commit とする。
- 互換 PTY suite は `mise -C tools/updev run test-e2e`、dashboard-only の短縮確認は
  `mise -C tools/updev run test-e2e-smoke` を使う。互換 suite は shell-use parity が
  macOS/Linux CI と実端末で確認できるまで維持するが、新しい primary coverage は
  shell-use 側へ追加する
- 非 TTY smoke は `updev --dry-run --interactive` / `updev list --interactive` /
  `updev last --interactive` が TUI に入らず、警告を出して text output に
  fallback することを確認する。`--plain`、`--format json`、`--no-interactive`
  では警告を出さない
- review-needed の exit `2` は正常扱い。`go run . ...` で確認する場合は
  `exit status 2` と表示される

### TTY performance/streaming dogfood

機能が正しいだけでなく「最初の有用な画面が早く出る」ことを確認する。
dashboard/list/last の遷移や post-provider refresh を変更した時は次を確認する。

```bash
mise -C tools/updev run test-tui
mise -C tools/updev run test-e2e-fast
mise -C tools/updev run test-e2e
UPDEV_LANG=ja updev --dry-run --interactive
UPDEV_LANG=ja updev list --interactive --refresh
```

期待:
- TTY dashboard shell は brew/mise provider command の stdout/stderr が出終わった
  後に表示される。provider command 実行中に alternate-screen TUI へ入らない
- 未完了 domain は安定した loading/progress row を表示し、完了時に同じ画面内で
  内容が refresh される。header や body が大きく jump しない
- non-dry-run text update では、brew/mise の stdout/stderr が provider command
  実行中に見える。実ログを TUI の裏に隠さず、post-update dashboard は provider
  完了後に開く
- provider stdout/stderr は expanded evidence でも確認できるが、generic progress
  noise は updated/deferred outcome summary に混ざらない
- post-update manual/backend review loader は dashboard context を共有する。
  TUI 終了時は context が cancel され、context-aware loader は中断される。
  キャンセル済み message が届いた場合は held partial report として扱われる
- future post-provider security/inventory/translation streaming では、`q` /
  Back / Ctrl+C の cancellation semantics を明示し、partial report を保存する
  場合は completed report と区別できる
- non-TTY、`--plain`、`--format json`、`--no-interactive` は streaming TTY
  behavior に影響されず mode/output contract を維持する。non-dry-run text
  update の provider 実ログは `--plain` でも表示される
- timing は machine-dependent なので、docs に固定秒数を常設しない。必要なら
  dogfood note として一時的に記録する

### Hands-on TTY acceptance

The pinned shell-use lane uses the following reproducible boundary:

- semantic journeys run with isolated `HOME`, every XDG root read by updev
  (`CONFIG`, `DATA`, `CACHE`, `STATE`, and `RUNTIME`), `TMPDIR`, an isolated
  `UPDEV_CONFIG`, fixture provider binaries, `TERM=xterm-256color`, `LC_ALL=C`,
  `TZ=UTC`, and `NO_COLOR=1`;
- visual journeys use the same fixture boundary, enable true color, capture at
  canonical `120x36`, and render SVG to PNG with the project-pinned resvg before
  exact ODiff comparison;
- `80x24` covers compact confirmation and routed interaction, while the
  dashboard semantic journey resizes to `160x48` and reasserts its summary and
  action regions;
- canonical macOS terminal baselines live under `test/tui/baselines/terminal/`,
  visual baselines under `test/tui/baselines/visual/`, and failure evidence under
  `test/tui/artifacts/<semantic|visual>/` as the actual snapshot/SVG/PNG and
  visual diff when applicable;
- a material visual change requires explicit
  `mise -C tools/updev run test-tui-update-baselines`, review of the terminal
  diff, and direct inspection of every changed rendered PNG. Passing ODiff does
  not replace that inspection.

リリース前の最終確認では、自動化された semantic/visual gate に加えて実端末の
画面を確認する。shell-use は route、focus、snapshot、terminal lifecycle の証拠を
固定するが、action hint、日本語表示、詳細展開、confirmation の読みやすさは
実際に使用する terminal で判定する。

#### v0.7.20 real-terminal release check

Run this bounded route from the canonical chezmoi source after applying the
candidate binary. Do not use `go run`; the release check must exercise the same
`updev` executable used every day.

```bash
updev version
UPDEV_LANG=ja updev --dry-run --interactive
UPDEV_LANG=ja updev list --interactive
bash -o pipefail -c 'chezmoi execute-template < .chezmoiscripts/run_onchange_after_00brew.sh.tmpl | bash'
```

Before checking `last`, verify that an ordinary non-dry-run report already
exists. A dry-run writes a separate cache and does not satisfy this prerequisite.

```bash
last_report="${XDG_CACHE_HOME:-$HOME/.cache}/updev/reports/last-update.json"
test -f "$last_report" && UPDEV_LANG=ja updev last --interactive
```

If the file is absent, leave the cached-report subcheck open until the next
ordinary daily `updev` run. Do not trigger a package mutation only to create
release evidence.

Record `TTY route: PASS` only when all of the following hold:

1. `updev version` reports `updev v0.7.20`.
2. The dry-run opens dashboard-first. The dashboard remains visible until
   input; it does not auto-open inventory.
3. Exercise the direct summary shortcuts defined in
   [Navigation And Route-To-Intent](../ux/NAVIGATION.md#update-summary). A
   section with no rows may show an explicit empty state instead of opening an
   unrelated list.
4. In installed inventory, `Tab` switches to manual apps and a second `Tab`
   returns to installed items. Press `/`, enter a query that retains an
   actionable row, and open that row's focused action. After pressing `b`, the
   same non-empty query, row, and action focus are restored without a
   blank-screen pause. Clear the filter only after this assertion.
5. From a child reached through `updev`, `b` returns to the same dashboard row.
   `h` returns to the dashboard root. From `updev list`, `b` returns to the
   originating inventory row rather than the dashboard or an unfiltered list.
6. When the ordinary-run cache exists, `updev last` opens its summary without
   rerunning providers. Open one update or security item and return; the summary
   row and scroll position are preserved.
7. Long Japanese rows remain aligned at the current terminal width. Expanded
   detail keeps summary, evidence, and actions distinct; no URL colon is used
   as a field separator and no line is replaced by invalid UTF-8.
8. The rendered Brewfile hook prints `Brewfile changed`, the rendered path,
   and the safe `updev apply brewfile` preview. It must not execute or recommend
   routine `brew bundle` apply. Active mise-owned Homebrew desired items must
   not appear as `dump only`; specifically, the v0.7.20 `btop` authority pilot
   must be recognized as desired.
9. Exit every surface with `q`. The normal prompt and terminal echo are fully
   restored.

Record `provider streaming: PASS` from the next ordinary daily `updev` run:
brew/mise stdout and stderr must remain visible while each provider runs, and
the dashboard may open only after both finish. Do not trigger a second mutation
solely for release acceptance. Keep the v0.7.20 release checkbox open until
both `TTY route` and `provider streaming` are PASS.

The checklist above is the only v0.7.20 manual command sequence. The following
are reusable domain checks when matching rows exist; they do not add another
release command sequence:

- top `updev` selector は dashboard-first で開き、dashboard rows に
  update evidence、manual apps、backend convergence、security、inventory、logs
  への action hint が表示される。summary 表示後に自動で installed inventory や
  旧 selector へ遷移しない
- manual review plan は dashboard row から開ける。Back-only ではなく、focused
  row action hint、`[操作:N]`、expanded `詳細` / `根拠` / `操作`、`accept` /
  `ignore` / `edit` confirmation を表示する。write action の smoke は
  confirmation の Back でキャンセルする。detail の Back は dashboard に戻る
- backend convergence は dashboard row または `updev list --interactive` の
  routed action から開ける。`[applyable]` と `[review-only]` を区別し、
  expanded row で `適用可否` と action を表示する。write action の smoke は
  confirmation の Back でキャンセルする。installed inventory から開いた場合は
  focused item に一致する finding だけを表示し、Back で元の inventory browser に
  戻る
- installed inventory は `updev list --interactive` で開き、focused row に
  manual/backend/security/update routing action がある場合は `a/1` から該当
  detail browser に進める。routing action は item identity を保持し、対象 item
  の detail rows を先に表示する。manual provider の default bucket には
  Homebrew-managed GUI apps が混ざらない
- security action acceptance は実 report に `hold` / `review` finding がある時に
  `updev --dry-run --interactive` または
  `updev last --section security` から確認する。finding が無い
  実機では `TestDogfoodDetailRowsSelectManualBackendSecurityAndDashboardActions`
  で action key path を固定し、fixture-backed cached report または実 finding が
  出た時に再度実端末 smoke を行う
