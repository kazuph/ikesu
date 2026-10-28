# ikesu を herdr fork の最新版（0.2.24）の見た目と操作にそろえる段階の計画

作成: 2026-10-10（親 Claude）。

## 依頼

店長（2026-10-10、ikesu の画面のスクリーンショット `.artifacts/look-0224/user-screenshot-ikesu.png` を添えて）: 「fork の herdr の最新の見た目に追いついてないかも。基本的に全部背景透過です。」

親の調べ:

- これまで見本にしていた mini の herdr は 0.2.8 で、店長が使っている herdr は 0.2.24 だった。0.2.24 の配布用ビルド（店長の Mac の Homebrew 版をそのままコピーしたもの）を mini の `~/bench-terrace.noindex/work/herdr-0.2.24` に置いた。**この段階からの見本はこれ**。
- 店長の herdr の透過は、設定 `[theme.custom] panel_bg = "transparent"`（ほかに `surface_dim`・`surface0`・`overlay0`・`overlay1` の上書き）と `[ui] transparent_sidebar_cards = true` で作られている。ikesu はどちらもまだ持っていない。
  - `[theme.custom]`: herdr の利用者向け説明 `configuration.mdx` の 262 行目付近（色は hex・名前・`rgb(r,g,b)`・`reset`/`default`/`none`/`transparent` を受け付ける）。
  - `transparent_sidebar_cards`: herdr `SPEC.md` の G11「endpoint clientでのfork操作と入力の保持」の節（既定 false、true でサイドバーの space・agent のカードを塗らず、選択中は左の accent 色の縦線）。

## 0. 作り方の決まり（最優先）

- herdr のソースコード（`~/src/github.com/kazuph/herdr` の `src`・`tests` などの実装とテスト）を読まない。挙動は仕様の文、利用者向け説明（`docs/next/website/src/content/docs/` の英語版）、`docs/ref/`、mini の herdr 0.2.24 を隔離して動かした観察だけで決める。店長が使っている稼働中の herdr とその設定には触らない（観察用の設定は一時フォルダに書く）。
- 細かい操作と見た目は herdr 0.2.24 の実物を正とする。安全のために SPEC がデグレ判定で明示している決まり（入力前の拒否、fail closed、誤配送の防止、done の既読規則など）は変えずに親へ報告する。
- コピーの比較は、これまでどおり sandbox-exec でクリップボードへの接続を禁止して行い、ハッシュの確認と「コピーした内容と一致したら止める」規則を守る。

## 1. やること

1. **`[theme.custom]` の色の上書き**を作る。説明書にある全部の鍵と値の形を受け付け、`transparent` などの「塗らない」値は端末の既定背景のまま描く。既存のテーマ（13テーマ）と組み合わせて効くこと、設定の再読み込みで反映されること。
2. **`[ui] transparent_sidebar_cards`** を作る（SPEC の受け入れ条件どおり。false のときは今の塗りのまま）。
3. **見本を herdr 0.2.24 に替えて、`tests/parity/` の比較を全部流し直す**。0.2.8 から変わった見た目・操作の差を全部直す。比べるときは、店長の herdr の設定と同じ見た目の設定（`[theme] name = "tokyo-night"`、上の `[theme.custom]`、`transparent_sidebar_cards = true`、`workspace_panel_density = "slim"`、`agent_panel_scope = "sort"`、`agent_panel_sort = "priority"`、`show_tab_bar = true`、`show_agent_labels_on_pane_borders = true`）の場合と、何も設定しない場合の両方で比べる。設定ファイルは herdr・ikesu それぞれの一時フォルダに書く。
4. **前の段階の発見事項2件**を 0.2.24 と比べて合わせる: (a) ペインの幅を縮めたときに右側の文字が消え、読み取りでも戻らない件、(b) 全画面アプリ（alternate screen）の高さを縮めたときに recent の読み取りで先頭行が抜ける件。実物がどう振る舞うかを観察してから、それに合わせる。
5. `tests/parity/` の比較手順の見本の場所を 0.2.24 に替え、どの版と比べたかを結果に残す。

## 2. 守ること

- これまでの段階と同じ: 1ファイル 800 行まで、推測で埋めない、未対応は未対応と書く、依存ライブラリを足さない（`third_party/vt` はこのリポジトリの中の部品なので直してよいが、直した理由を書く）、計画1章に無い機能は作らない。
- go build はメインの Mac でしてよい。テスト・race・性能計測・ptyrun の観察は mini（同期先 `src/look`）。自分で起動したサーバーは確かめが終わるたびに止める。
- 性能: 区切りごとに mini で `go test -tags perf ./tests/perf -run TestRelative`（基準は main b0a18b0 の同条件ビルド）を全項目合格にする。**mini の混み具合が4以上で計測を待つことになったら、待ち続けずに親へ報告して止まる**（ゴールの自動続行が空回りするため。計測は親が引き取る）。

## 3. 進め方と確認

- 区切り: theme.custom と transparent_sidebar_cards → 見本の差し替えと比較の流し直し → 差の修正（要素ごとに積み上げコミット）→ 発見事項2件 → 仕上げ。区切りごとに mini で gofmt・go build・go vet・go test（最後は -race も）。
- `.artifacts/look-0224/RESULT.md` に、差の表（画面要素・操作・herdr 0.2.24 の結果・ikesu の結果・直したか・保存画面）を書く。確かめていない操作は未達と書く。透過の見た目は、店長の設定と同じ設定で herdr と ikesu の画面（セルの背景色が「既定」になっているか）を比べた結果を必ず入れる。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
