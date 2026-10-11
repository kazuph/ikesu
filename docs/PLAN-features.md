# 「未対応」と警告している機能と、一部だけの機能を作る段階の計画

作成: 2026-10-11（親 Claude）。

## 依頼

店長（2026-10-11）: SPEC.md を全部実装する方針のもと、段階を取り込んだら止まらず次へ進めてよい。判断待ちの段階が main 1d8e014 に入ったので、残項目一覧（`.worktree/feat/endpoint/.artifacts/endpoint/SPEC-REMAINING.md`）のうち、設定を読んでも「未対応」と警告するだけの機能と、一部だけの機能をこの段階で作る。

## 0. 作り方の決まり（最優先）

- herdr のソースコードを読まない・移植しない（AGENTS.md）。
- 仕様の正: `~/src/github.com/kazuph/herdr/SPEC.md` の該当節、`herdr --default-config` の説明（`.worktree/feat/config-compat/.artifacts/config-compat/herdr-0.2.24-default-config.toml`）、利用者向け説明 `docs/next/website/src/content/docs/`。細部は mini の herdr 0.2.24（`~/bench-terrace.noindex/work/herdr-0.2.24`）を一時の HOME・設定・state で動かして観察する。
- **音を鳴らさない（最重要）**: 確かめで音をスピーカーから鳴らしてはならない。再生する仕組みは、再生に使うコマンドを差し替えられるようにし、テストでは差し替えたコマンドが「どのファイルで何回呼ばれたか」を記録して確かめる。herdr 実物の観察も、音が出ないように（`HERDR_DISABLE_SOUND=1` や再生コマンドの差し替え）して行う。
- **店長の環境を変えない**: 本物の `~/.ssh/config`、OS の入力ソース、通知の設定、クリップボードに触らない。ssh config の管理は一時の HOME の中だけで確かめる。入力ソースの切り替えは、切り替えに使う手段を差し替えて「呼ばれたこと」を確かめ、本物の入力ソースは切り替えない。OS 通知の表示も同じく差し替えて確かめる。

## 1. やること（残項目の番号は SPEC-REMAINING.md の番号）

1. `ui.pane_gaps = false`（残項目01）: pane の間の隙間を無くす描画。
2. 通知と音（残項目04〜08）: 通知本文の入力を直近80本の折り返し前の論理行にする。headless や保存マシンの OS 通知を、接続している手元の画面へ届けて対象 pane へ戻れるようにする。OS 通知のクリックで対象のターミナルアプリも前面に出す。音の再生（終了・質問・agent 別）と `ui.sound.path`・`done_path`・`request_path`・`ui.sound.agents.*` の適用。
3. `theme.custom.blue`（残項目09）: 実画面の使用箇所へ適用（herdr 実物で blue が使われる箇所を観察して特定する）。
4. `advanced.scrollback_limit_bytes`（残項目10）: scrollback の保持量をバイト上限で制御する。
5. CJK IME（残項目11・12）: `experimental.reveal_hidden_cursor_for_cjk_ime`・`cjk_ime_agents`・`cjk_ime_cursor_shape`（隠れたカーソルを指定の形で見せる）、`switch_ascii_input_source_in_prefix`（prefix 操作で入力ソースを ASCII に切り替える）。
6. `remote.manage_ssh_config`（残項目23）: ssh config の管理。
7. `keys.remote_image_paste`（残項目24）: 設定したキーから保存マシンへの画像貼り付け。
8. `experimental.pane_history`（残項目25）。
9. `session_start_source = "resume"` の記録（残項目02）。
10. pane/agent read の `format=ansi`（残項目31）。

それぞれ作ったら、設定の互換の段階で出していた「未対応」の警告をその鍵だけ外す（作っていないものは警告を残す）。

### 作らないもの
- 自動更新（残項目13、G8 の方針で無効）、旧形式の保存データの読み替え（03）、marketplace worker（22）、agent attach/explain（30）、共有の保存場所（16）。
- 危険 command の判定（G12）、live handoff と kitty 画像（別の段階）。

## 2. 守ること

- これまでの段階と同じ: 1ファイル 800 行まで、推測で埋めない、未対応は未対応と書く、依存ライブラリを足さない、計画1章に無い機能を作らない。
- SPEC と実物が食い違い、SPEC が「〜してはならない」と書く挙動に当たったら、実装前に親へ報告して止まる。SPEC にも実物にも決め手が無いもの（例: 画像の一時ファイルの後始末）は親へ報告する。
- go build はメインの Mac で可。テスト・race・ptyrun は mini。性能（TestRelative、基準 main 1d8e014）は親が最後に測る。大量出力中の処理を増やさない（scrollback 上限の制御を足すときは特に注意し、増えるなら親へ報告して止まる）。

## 3. 進め方と確認

- 区切り: 描画（01・09）→ 入力と scrollback（10・11・12・25）→ 通知と音（04〜08）→ 保存マシンまわり（23・24）→ 記録と read（02・31）。区切りごとに mini で gofmt・go build・go vet・go test を通して積み上げコミット。最後に race。
- `.artifacts/features/RESULT.md` に、項目ごとの表（項目・確かめたテスト名か herdr 実物と比べた画面・状態）を書く。確かめていない項目は未達と書く。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
