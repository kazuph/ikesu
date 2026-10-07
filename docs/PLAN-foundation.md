# gtx 土台段階の計画（サーバー／画面の分離・操作コマンド・タブ）

作成: 2026-10-07（親 Claude）。店長の決定「SPEC.md の機能は土台から足す」に基づく。

## 0. 何を作るか・なぜ

gtx はいま 1 プロセスの画面アプリで、画面を閉じるとペインも消える。herdr fork の SPEC.md
（`~/src/github.com/kazuph/herdr/SPEC.md`、読むだけ）の大半（エージェント会話の復元、ペインへの
指示送信、メールボックス、`herdr run`、通知）は、次の3つが前提になっている。

1. **裏で動くサーバーと画面役のクライアントの分離**: サーバーが PTY・端末エンジン・状態を持ち、
   画面（クライアント）は表示と入力だけを受け持つ。画面を閉じて（デタッチ）もペインは生き続け、
   もう一度 `gtx` を起動すると同じ画面に戻る（アタッチ）。
2. **ペイン操作コマンド**: `gtx pane list / get / current / read / run / send-text / send-keys / split / close ...`
   を、ほかの端末や AI から叩ける。herdr と同じ書式・同じ JSON を返す。
3. **タブ**: 1つのワークスペースの中に複数のタブ（それぞれ独立した分割レイアウト）を持てる。

この段階では上の3つだけを作る。SPEC.md の各機能（G1〜G11）は次の段階以降。

## 1. 決まっていること（変えない）

| 項目 | 決定 |
|---|---|
| 起動 | `gtx`（引数なし）= セッションのサーバーに接続。無ければサーバーを裏で起動してから接続する。`gtx --no-session` = 今の 1 プロセス動作（残す。性能比較の基準にもする） |
| セッション | `--session <名前>`（既定 `default`）。置き場所は `$GTX_STATE_DIR`（既定 `~/.local/state/gtx`）の下の `<名前>/`。ソケットは `gtx.sock`（操作用 API）と `gtx-client.sock`（画面用）の2つ |
| サーバーの起動 | 隠しサブコマンド `gtx server --session <名前>` を `setsid` 相当で切り離して起動し、標準出力・標準エラーは同じフォルダの `server.log` へ。二重起動はロックファイルで防ぐ |
| 画面の分担 | 描画はサーバー側で今の `render` を使って行い、差分の ANSI バイト列をクライアントへ送る（herdr と同じ方式）。クライアントは raw モード・画面の出入り（alternate screen・マウス・貼り付けモード）・入力バイトの転送・サイズ変更の通知だけを行う |
| 同時に繋ぐ画面 | この段階では1つ。新しい画面が繋いだら古い画面は「別の画面が接続しました」と出してデタッチする（複数画面の同時表示は herdr の UP-MULTICLIENT で後の段階） |
| デタッチ | プレフィックス（今の Ctrl-B）→ `d`。サーバーとペインは残る。全ペインが終了したらサーバーも終了する |
| API の通信形式 | herdr と同じ「1行1 JSON」: 要求 `{"id":..., "method":"pane.list", "params":{...}}`、成功 `{"id":..., "result":{...}}`、失敗 `{"id":..., "error":{"code":"pane_not_found","message":...}}` |
| API のメソッド名・項目名 | herdr の API スキーマ（`docs/ref/herdr-api-schema.json`、herdr 0.2.10・protocol 19 から保存）と同じ名前・同じ項目名・同じ型にする。作るのは下の 3章の一覧だけ |
| CLI の書式 | herdr の CLI（`docs/ref/herdr-cli-*.txt`）と同じ引数・同じ出力（`{"id":"cli:pane:list","result":...}`）。コマンド名だけ `herdr` → `gtx` |
| ID | herdr と同じ形: ワークスペース `sN`、タブ `sN:tM`、ペイン `pN`。ペインの指定は `pN`・`p_N`・`%N` を全部受け付け、応答は `pN` で返す |
| 環境変数 | ペイン内に `GTX_PANE_ID=p_N`・`GTX_SOCKET_PATH=<gtx.sock の絶対パス>` を出す（今の `GOTERRACE=1`・`GOTERRACE_PANE_ID` は残す）。`gtx` を gtx のペインの中で起動したら、入れ子を防ぐため拒否する（`--no-session` 以外） |
| `pane current` | `GTX_PANE_ID` があればそれ。無ければ呼び出し元プロセスの親をたどって、どのペインの子孫かで決める。決められなければ推測せずエラー（SPEC.md G5「fail-closedなcurrent pane解決」） |
| 依存 | 標準ライブラリと今の依存だけで作る（JSON は `encoding/json`、ソケットは `net`）。cgo を使わない |

## 2. 守る設計方針（今の計画と同じ）

- 状態（`model`）は PTY もソケットも持たない純粋なデータのまま。サーバーは「model ＋ runtime ＋ 画面1つ分の
  表示状態（スクロール位置・選択・メニュー）」を持ち、コマンド（`model.Cmd*`）で状態を変える。
  API の要求も画面の操作も、同じコマンドを通して状態を変える（API 用の別の書き換え経路を作らない）。
- 1ファイル 800 行を超えそうなら分ける。新しいパッケージは `internal/server`（サーバー本体・API）、
  `internal/client`（画面側）、`internal/api`（要求・応答の型と JSON、CLI からも使う）、`internal/cli`
  （サブコマンドの解析と表示）を想定する。
- 性能: 200万行の計測（`tools/bench/bench_screen.sh`「最後の行が画面に出るまで」）を、
  サーバー経由の `gtx` と `gtx --no-session` の両方で、各マイルストーンの最後に mini で測る。
  基準は main（5f7b5ea）の 2.85〜2.98秒から悪化しないこと（負荷のぶれを考え、3回の中央値が 3.1秒以下）。

## 3. 作る API・CLI（この段階で全部）

| herdr の API メソッド | CLI | 内容 |
|---|---|---|
| `ping` | ― | 生存確認 |
| `server.stop` | `gtx server stop` | サーバー停止（全ペインを終了） |
| `workspace.list` / `get` / `create` / `focus` / `rename` / `close` | `gtx workspace ...` | herdr-cli-workspace.txt のうち `report-metadata` 以外 |
| `tab.list` / `get` / `create` / `focus` / `rename` / `close` | `gtx tab ...` | herdr-cli-tab.txt のとおり |
| `pane.list` / `get` / `current` / `focus` / `rename` / `close` | `gtx pane ...` | 同上 |
| `pane.read` | `gtx pane read <id> [--source visible\|recent\|recent-unwrapped] [--lines N]` | `--format ansi` はこの段階では作らない（text のみ） |
| `pane.split` | `gtx pane split ... --direction right\|down [--ratio] [--cwd] [--env] [--focus\|--no-focus]` | |
| `pane.send_text` / `pane.send_keys` | `gtx pane send-text` / `send-keys` | キー名は herdr と同じ表記（Enter, C-c, Up など。スキーマの定義に合わせる） |
| （CLI のみ） | `gtx pane run <id> <command>` | herdr と同じく「文字を送って Enter」。herdr の実装（send_text ＋ Enter の送り方）を読んで合わせる |
| `pane.report_agent` | `gtx pane report-agent ...` | `--source --agent --state --title --session-id` を受け取り保存し、`pane.get`/`list` とサイドバーに反映 |

`pane.list`・`pane.get` の応答の項目は herdr と同じ名前にする（`pane_id`・`workspace_id`・`tab_id`・`cwd`・
`foreground_cwd`・`focused`・`agent`・`agent_status`・`terminal_title`・`scroll` など、gtx が持っている情報の分。
持っていない項目は出さない。値を作り話で埋めない）。

## 4. マイルストーン（この順。各マイルストーンの最後に gofmt・go build・go vet・go test（最後は -race も）が通ってからコミット）

0. **既存の結合テストの直し**（親の判断で追加。店長のルール「落ちているテストを見つけたらその場で直す」に基づく）:
   `tests/e2e` の `TestWheelScrollback`・`TestDragSelectCopy` が、プロンプトに「❯」が出るのを待つ作りのため、
   プロンプトが違う環境（mini）で時間切れになる。テスト用のシェル設定（例: `ZDOTDIR` を空のフォルダにして
   `PS1` を固定）でどの環境でも通るようにする。
1. **API の型と CLI の骨組み**: `internal/api`（要求・応答・エラーの型、ID の解析）と `internal/cli`。
   サーバーが無いときは herdr と同じ形のエラーを返す。
2. **サーバーとクライアントの分離**: `gtx server`・ソケット・ロック・デタッチ／アタッチ・入れ子の拒否。
   `gtx --no-session` は今の動作のまま。
3. **API の実装（ワークスペース・ペイン）**: 3章のうちタブ以外。`pane current` の親プロセスたどりを含む。
4. **タブ**: model にタブを足し（ワークスペース → タブ → レイアウト）、画面にタブの帯を出す。見た目と操作は herdr の
   `src/ui/tabs.rs`・`src/ui/tab_surface.rs`（読むだけ）に合わせる。`tab.*` の API と CLI。
5. **仕上げの確認**: 6章の動作確認と性能計測。

## 5. 受け入れ条件（親がレビューで確かめる）

- `gtx` を起動 → 分割してシェルで `sleep 1000` → デタッチ → もう一度 `gtx` → 同じ分割・同じ `sleep` が見える。
- ほかの端末から `gtx pane list`・`gtx pane read <id> --lines 5`・`gtx pane run <id> "echo hi"` が動き、
  出力の JSON が herdr の同じコマンドと同じ項目名・同じ形になっている（herdr の実物と並べて比べた記録を残す）。
- ペインの中で `gtx pane current` が自分のペインを返し、`GTX_PANE_ID` を消して親プロセスたどりでも同じ結果。
  決められない場合はエラーで止まる。
- タブを作る・切り替える・閉じる・名前を変えるが、画面のマウス操作と CLI の両方でできる。
- 200万行の計測が 2章の基準を満たす（サーバー経由と `--no-session` の両方）。
- 既存の機能（分割・ワークスペース・サイドバー・エージェント状態表示・マウス操作・コピー）が、サーバー経由でも
  今と同じに動く（既存の結合テストをサーバー経由でも走らせる）。

## 6. 動作確認（実装役が必ず行う）

- 計測・結合テストは mini（`ssh mini`、`~/bench-terrace.noindex`）で行う。メインの Mac では go build と軽い go test 以外の重い処理をしない。
- 画面の確認は `tools/bench/ptyrun`（手順書でキー・クリックを送り画面を保存する道具）で行い、保存した画面を `.artifacts/` に残す。

## 7. やってはいけないこと

- この計画に無い機能（SPEC.md の各機能、`--format ansi`、複数画面の同時表示、セッションの保存と復元、メールボックス、`run`）を作らない。必要だと思ったら発見事項に書く。
- herdr fork（`~/src/github.com/kazuph/herdr`）と稼働中の herdr サーバーを変更しない（読むだけ）。herdr のコードを1行ずつ写さない（どう動くべきかの参考にする）。
- push しない。amend・reset・force push しない。
