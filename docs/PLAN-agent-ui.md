# ikesu 通知・エージェント操作・実行環境の残りの段階の計画（SPEC G4・G5・G9 の残り）

作成: 2026-10-09（親 Claude）。店長の方針「herdr の SPEC.md のとおりに全部実装する。優先順位は無い」と、リポジトリの `AGENTS.md`「仕様から実装する。移植しない」に基づく。

## 0. 作り方の決まり（最優先）

- herdr のソースコード（`~/src/github.com/kazuph/herdr` の `src`・`tests` などの実装とテスト）を読まない。
- 仕様の正は `~/src/github.com/kazuph/herdr/SPEC.md` の下記の節の文章、`docs/ref/` の API スキーマと CLI help。`herdr` → `ikesu`、`HERDR_*` → `IKESU_*` と読み替える。
- 文章で決まらない見た目や操作の細部は、mini で herdr の配布用ビルドを別に動かして観察する（`~/bench-terrace.noindex/work/herdr-target/release/herdr --no-session` を ptyrun の中で動かす。設定は `~/bench-terrace.noindex/work/hx/` を使い回す）。店長が使っている稼働中の herdr には触らない。それでも決まらなければ親へ質問する。
- 各節の「受け入れ条件」と「デグレ判定」を、そのまま ikesu の受け入れ条件として扱う。仕様の文どうしが食い違って見えるときは mini の herdr 実物の画面を正とし、RESULT.md に書く。

## 1. 作る節

すでに一部が main にある節（ペイン枠のタイトル、フォーカス履歴、トースト、`ikesu run` の `next` 案内、描画済み通知）は、受け入れ条件を1つずつ確かめ直し、足りない分を作る。「作り済み」を理由に確かめを省かない。

| SPEC の節 | 作る範囲 |
|---|---|
| G4「render drain通知の正確化」 | 画面の書き込みが成功した後にだけ「描画済み」を通知する。受け入れ条件の3つ（読めた後に届く、書き込み失敗で先に送らない、通常の受信経路と終了間際の短い待ちの経路の両方）を直接確かめるテストを置く |
| G5「右クリックからのAgent起動とpane移動」 | 全部。ワークスペースとペインの右クリックメニューの先頭に `New Claude Code agent`・`New Codex agent`・`New agy agent`、ペインメニューの `Move to left/right/upper/lower split`、1ペインのとき出さない項目。起動するコマンド名は設定で変えられる（SPEC の「configurable preset」） |
| G5「paneタイトル常時表示とgit branch表示」 | 全部（1ペインでも常時、`%N <label>`、`ZOOM`、タスク名 > OSC タイトル、ペインの作業フォルダの git ブランチ、Settings にタイトル表示の切り替えを置かない、Settings の節の順） |
| G5「agent task titleとfooter labelの整理」 | 全部（`pane report-agent --title` の前後空白の除去、ワークスペース名 `<フォルダ名>-<タスク名>`、シェルの OSC タイトルを混ぜない、下部バーの大文字表記） |
| G5「dialog・menu・tab・settingsの微細契約」 | 全部（入力欄の本物のカーソル位置、区切り行 ` --` と色、タブの印 `!` `>` `+` と優先順位と幅、Settings の `↵ apply` と `esc close`） |
| G9「alternate binary session隔離」 | 全部。実行ファイル名（例 `ikesu-next`）から別の保存先・ソケットを決める。親から漏れた環境変数だけでは通常のサーバーにつながず、明示指定のときだけつなぐ。別名のサーバーが作ったペインには、そのソケットと明示指定の印を渡す |
| G9「user shell PATHでのagent command起動」 | 全部（ログインシェルの PATH を1回だけ求めて使い回す、失敗時に推測の PATH を作らない、引数と作業フォルダを変えない） |
| G9「top-right toast anchor」 | 全部（右上、設定の警告表示の下、幅は `max(タイトルのバイト長, 本文のバイト長) + 6` を画面幅で抑える。バイト長基準をテストで固定する） |
| G9「pane focus history」 | 全部（2ペイン・ワークスペースをまたぐ・タブをまたぐ往復、閉じたペインを飛ばす、Vim 風モードと独立、既定はキー割り当てなし） |
| G9「inactive agent rowの可読性」 | 全部（非アクティブ行と アクティブ行の背景・文字色・太字、DIM を付けない） |
| G9「macOS Ghostty buildとparallel testの安定化」 | Ghostty のビルド指定は ikesu に Ghostty が無いので対象外と RESULT.md に書く。もう半分（プロセス全体の状態を変えるテストは共有の鍵と明示的な戻しを使い、並列で繰り返しても落ちない）は作る。`go test -race -count=5 ./...` が通ることで確かめる |
| G9「herdr runの完了通知案内」 | 受け入れ条件の4つを確かめ直す（JSON の `next` が既存の項目の後ろにある、を含む） |
| G9「agent prompt（本家0.9.1準拠）」 | 全部。`ikesu agent prompt <target> <text> [--wait] [--until STATUS]... [--timeout MS]` と API `agent.prompt`。入力前の拒否、ブラケットペースト、Enter を書いた後にだけ成功、`--wait`、起動直後の段階（3秒の落ち着き待ち、30秒の起動期限）。`agent send`・`send --room`・ペイン ID の表記は今のまま |
| G9「agent検出manifestの共有cache反映とfork所有manifest」 | 店長の決定（2026-10-09）: 検出ルールのダウンロードは無効のまま（G8 を優先）。同梱ルールだけを使い、`fork_owned = true` の Devin のルールが同梱のまま使われること、同梱の Claude のルールが SPEC の挙動（ターン後の裏のシェルだけでは working にしない、MCP の質問は blocked）、Devin の作業中・待ち行列だけの idle の判定を確かめる。ダウンロードに関わる条件は RESULT.md に「方針により対象外（店長決定 2026-10-09）」と書く |
| G9「問い合わせとjob記録によるserver負荷の抑制」 | 全部（前面ジョブ調査の1.5秒の使い回しと条件、24時間でのジョブ記録・ログの掃除と返信先の外し方、ジョブ一覧の更新間隔1秒／10秒と即時更新、索引、複数画面への配信で session 全体の読み取りを1回にする、掃除がほかのプロセスの書き込みを最長5秒待つ） |

### この段階で作らない節（後の段階へ回す。理由は親が店長へ報告済み）

- G9「local overlay pane command」と、G1 から移した overlay の条件: overlay は SPEC 上プラグインのペイン（API `plugin.pane.open` の配置 `overlay`）として定義されており、プラグインの土台は G10「UP-AUTOMATION」で作るため、その段階で作る。
- G9「live handoffでのmouse報告状態の引継ぎ」「kitty画像対応paneのXTVERSION」: ikesu にサーバーの引き継ぎ（live handoff）と kitty 画像の土台が無い。土台ごと作る段階を別に設ける。
- G9「問い合わせとjob記録によるserver負荷の抑制」のうち「複数clientが接続していても session 全体の読み取りは1回」の条件: 土台の段階（docs/PLAN-foundation.md「同時に繋ぐ画面」）で、同時に繋ぐ画面は1つ・新しい画面が古い画面を切り離すと決め、複数画面の同時表示は G10「UP-MULTICLIENT」の段階で作ることにしている。この条件はその段階で確かめる（親判断 2026-10-09）。今の「新しい画面が古い画面を切り離す」動きと TestNewClientDisplacesOld は変えない。同じ節のほかの条件はこの段階で作る。
- G9「herdr run --machine」: G11 の保存マシン一覧が前提なので G11 の段階で作る。
- G4「paneコマンド終了通知」: SPEC で破棄済み。作らない。
- G9「SPEC・運用規則・review evidenceの来歴」「SPEC未記載コミットの統合注記」: herdr の履歴の記録で、ikesu の機能ではない。

## 2. 守ること

- これまでの段階と同じ: 状態は純粋なデータ、API と画面は同じコマンドで状態を変える、1ファイル 800 行まで、推測で埋めない、未対応は未対応と書いてエラーを返す（似た値で代用しない）、計画1章に無い機能は作らない、依存ライブラリを足さない。
- API・CLI の名前と項目は `docs/ref/herdr-api-schema.json` と `docs/ref/herdr-cli-*.txt` と同じ（例 `agent.prompt`、`pane.report_agent` の `title`）。
- 受け入れ条件は1つずつ、SPEC の文どおりの入力と期待値を直接確かめるテスト、または ptyrun で保存した画面と自動判定で確かめる。
- git ブランチ表示は一時フォルダに作った git リポジトリで確かめる。ログインシェルの PATH は一時的な `SHELL` と一時フォルダの偽の agent コマンドで確かめる。店長の本物のリポジトリ・シェル設定・クリップボードを触らない。
- 性能: 区切りごとに mini で `go test -tags perf ./tests/perf -run TestRelative`（基準は main 73f8211 のビルド）を全項目合格にする。サーバー経由のメモリが段階ごとに少しずつ増えているので、増えた値を RESULT.md に書く。

## 3. 進め方と確認

- 区切りは自分で決めてよい（目安: 描画済み通知・トースト・非アクティブ行 → ペインタイトル・タスク名・下部バー → 右クリック起動と移動・微細契約 → 別名サーバーの隔離・ログインシェルの PATH → フォーカス履歴・agent prompt → サーバー負荷の抑制・並列テスト → 仕上げ）。区切りごとに mini で gofmt・go build・go vet・go test（最後は -race と -count=5 も）を通してから積み上げコミット。
- 計測・結合テスト・race は mini（`ssh mini`、`~/bench-terrace.noindex`、自分専用の同期先フォルダ `src/agent-ui`）。メインの Mac では go build と軽い go test だけ。
- `.artifacts/agent-ui/RESULT.md` に、SPEC の節ごと・受け入れ条件ごとに「確かめたテスト名、または保存した画面・出力」の表を書く。確かめていない条件は未達と書く。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
