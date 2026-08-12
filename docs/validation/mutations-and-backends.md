# Mutation And Backend Validation

Desired-manifest mutation, rollback, sync, and backend convergence checks.
Return to the [validation index](../VALIDATION.md).

## sync / add / remove / rollback

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

## Backend convergence smoke

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
- Homebrew formula と CLI-only cask は1回の `mise registry --json` snapshot
  から short name を解決し、GUI cask と `[backends].keep_homebrew` 対象は
  migration action を出さない
- registry-backed candidate は installed version を pin し、隔離 temp config の
  `mise lock --platform <current-platform>` が成功した場合だけ mise desired-state
  追加 action を出す。`mise install --dry-run` の成功だけでは platform 対応と
  判定しない
- platform 非対応は review-only candidate として理由を残し、1 run の platform
  lock は安定 kind/name 順で最大32件に制限する。超過時は warning を返す
- pin 追加 action は mise config だけを書き、Brewfile ownership と package 実体を
  変更しない。mise install 確認後の Brewfile entry 削除は別 confirmation とする
- backend findings がある時、daily `updev` の post-update hub と
  `updev list` TTY から backend convergence view へ移動でき、JSON を見なくても
  current / recommended / confidence / command status / OS 条件 / next action
  を確認できる
- JSON findings には `command_names` / `command_status` が含まれ、mise
  backend rewrite では `current_os` / `recommended_os` が OS 条件の引き継ぎ
  判断材料として残る
- どちらも read-only で manifest を変更しない
