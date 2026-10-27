# ikesu が herdr の設定ファイルをそのまま読めるようにする段階の計画

作成: 2026-10-11（親 Claude）。

## 依頼

店長（2026-10-11）: ikesu が herdr の設定をそのまま読めるようにする段階を始めてよい（親の提案を承認）。

経緯: 店長の herdr の設定（`.artifacts/config-compat/user-herdr-config.toml`。店長の `~/.config/herdr/config.toml` からコメントを除いた写し）を ikesu の設定へ写そうとしたところ、次の理由で写せなかった。

- `[keys]` の値を文字列で書く形（例 `focus_history_back = "cmd+["`）を ikesu は配列しか受け付けず、**設定ファイル全体の読み込みに失敗する**。
- `[[keys.command]]` の `type = "pane"` と `type = "popup"`（`width`・`height` 付き）を ikesu は「対応していない種類」として、**設定ファイル全体の読み込みに失敗する**。
- `keys.prefix`（店長は `ctrl+t`）、`keys.switch_workspace = "cmd+1..9"`、`keys.next_workspace`・`keys.previous_workspace`（cmd+j/k）、`theme.auto_switch`、`agent_restore.enabled`、`[agent_restore.commands]`、`onboarding` を ikesu は読まずに無視する。

## 0. 作り方の決まり（最優先）

- herdr のソースコード（`~/src/github.com/kazuph/herdr` の `src`・`tests` などの実装とテスト）を読まない。
- 仕様の正は次の3つ。`herdr` → `ikesu` と読み替える。
  - `herdr --default-config` の出力（herdr 0.2.24、`.artifacts/config-compat/herdr-0.2.24-default-config.toml`。全部の鍵・既定値・説明のコメント付き）
  - herdr の利用者向け説明 `~/src/github.com/kazuph/herdr/docs/next/website/src/content/docs/` の `configuration.mdx`・`keyboard.mdx`・`config-reference.mdx`（英語版）
  - `~/src/github.com/kazuph/herdr/SPEC.md` の設定に関わる節
- 細部（キーの書き方の受け付ける範囲、読めない値のときの警告の文言、設定の再読み込みでの反映）は、mini の herdr 0.2.24（`~/bench-terrace.noindex/work/herdr-0.2.24`）を一時の設定フォルダで動かして観察する。店長の本物の設定・稼働中の herdr と ikesu には触らない。

## 1. やること

1. **herdr の設定の鍵を全部読めるようにする**: `herdr --default-config` に出てくる全部の節と鍵（`[theme]`・`[terminal]`・`[update]`・`[keys]`・`[ui]`・`[ui.toast]`・`[ui.toast.*]`・`[ui.sound]`・`[session]`・`[remote]`・`[experimental]`・`[advanced]`、と説明書にある `[[keys.command]]`・`[agent_restore]`・`[agent_restore.commands]`・`[agent_start.commands]`・`[ui.sidebar.*]`・`[theme.custom]`）を、herdr と同じ型・既定値・受け付ける値で読む。キーの値は文字列でも配列でも受け付ける。
2. **読んだ設定を動きに反映する**: 少なくとも店長の設定に出てくるもの（`keys.prefix`、`switch_workspace = "cmd+1..9"`、`next_workspace`・`previous_workspace`・`focus_history_back`・`focus_history_forward` の cmd+ 系のキー、`[[keys.command]]` の `pane` 型と `popup` 型（`description`・`width`・`height`）、`theme.auto_switch`、`agent_restore.enabled`、`[agent_restore.commands]` の `{session_id}` 置き換え、`onboarding`）は、herdr 0.2.24 と同じに動くことを実物の観察と比べて確かめる。cmd+ 系のキーは、端末が送ってくる形（kitty の keyboard protocol など）を herdr がどう受けているかを観察して合わせる。それ以外の鍵も、ikesu に対応する機能がある鍵は反映する。対応する機能が ikesu にまだ無い鍵（例 `experimental.kitty_graphics`、`[update]` の自動更新。自動更新は G8 の方針で無効のまま）は、読み込んでも失敗にせず、その鍵だけを警告する。
3. **読めない値があっても設定全体を捨てない**: 型の違い・知らない種類・知らない値は、その項目だけを既定値に戻して警告し、ほかの項目は効かせる。警告の出し方（画面の上の警告表示の文言など）は herdr 0.2.24 の実物に合わせる。今の「1か所でも違うと設定全体を読まない」動きは無くす。
4. **店長の設定で確かめる**: `user-herdr-config.toml` をそのまま ikesu の一時の設定フォルダに置いて起動し、警告なしに読めて、上の各設定が herdr 0.2.24 と同じに効くことを実際の操作（ptyrun）で示す。

### 作らないもの

- `experimental.kitty_graphics` の画像表示そのもの（土台が大きいので別の段階。今回は読めて警告する所まで）。
- 自動更新（G8 の方針で無効）。Windows 向けのもの。

## 2. 守ること

- これまでの段階と同じ: 1ファイル 800 行まで、推測で埋めない、未対応は未対応と書く（似た値で代用しない）、依存ライブラリを足さない（TOML は今の BurntSushi/toml）、計画1章に無い機能は作らない。
- ikesu が今読める設定の書き方（配列のキー、ikesu が書く Settings の保存形式）はそのまま読めること。今ある設定ファイルを壊さない。
- go build はメインの Mac でしてよい。テスト・race・ptyrun の観察は mini（同期先 `src/config`）。自分で起動したサーバーは確かめが終わるたびに止める。
- 性能計測（TestRelative）は親が最後にまとめて測る。区切りごとに測らない・待たない。

## 3. 進め方と確認

- 区切り: 設定の読み込み（全部の鍵・型の幅・部分的な失敗）→ キー（prefix・cmd+・keys.command）→ テーマと agent_restore と onboarding → 残りの鍵の反映・警告 → 店長の設定での通し確認。区切りごとに mini で gofmt・go build・go vet・go test を通して積み上げコミット。最後に race。
- `.artifacts/config-compat/RESULT.md` に、鍵ごとの表（鍵・herdr の既定値と受け付ける値・ikesu で読めるか・動きに反映したか・確かめたテストか画面）を書く。確かめていない鍵は未達と書く。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
