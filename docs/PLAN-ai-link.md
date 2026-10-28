# ikesu AI 連携段階の計画（メールボックス・裏方ジョブ・agent コマンド・通知）

作成: 2026-10-07（親 Claude）。店長の決定:
- 土台（サーバーと画面の分離・ペイン操作コマンド・タブ）の次は「AI の連携機能」（SPEC.md の G9・G5・G4）。
- 保存は純 Go の SQLite（`modernc.org/sqlite`、cgo を使わない）。

仕様の正は herdr fork の `~/src/github.com/kazuph/herdr/SPEC.md`（読むだけ）の該当節。この計画は「どの節を・どこまで」作るかと、
ikesu 向けの読み替えを決める。各節の「受け入れ条件」「デグレ判定」はそのまま ikesu の受け入れ条件として扱う（`herdr` → `ikesu`、
`HERDR_*` → `IKESU_*` と読み替える）。

## 0. 何を作るか・なぜ

店長と AI が herdr で毎日使っている「ペイン同士の連絡（send / inbox）」「待たずに走らせて終わったら知らせる裏方ジョブ（run）」
「agent の起動・読み取り・送信（agent start / read / send）」「質問待ち・完了の通知」を ikesu に足し、ikesu を herdr の代わりに試せる状態にする。

## 1. 作る範囲（SPEC.md の節ごと）

| SPEC.md の節 | 作る範囲 |
|---|---|
| G9「herdr msg durable mailbox」 | 全部。`ikesu send <target> <message> [--room R]`・`ikesu inbox [--room R]`・`ikesu msg history`・`ikesu msg rooms`、API `msg.send` / `msg.inbox` / `msg.history` / `msg.rooms` |
| G9「normal mailのPTY全byte書込み確認」 | 全部（queued → in_flight → delivered/read、PTY への全バイト書き込み完了で delivered、失敗で queued に戻す、二重送信しない、App ループを止めない） |
| G9「pane-less background jobs」 | 全部。`ikesu run [--label] [--cwd] [--caller] [--completion summary\|full\|none] -- <command...>`・`ikesu run list`・`ikesu run cancel <job>`・`ikesu log <job>`・`ikesu log --db` |
| G9「unified communication dispatcher」 | 全部（send / inbox / log / run が同じ保存先・同じ宛先解決・room・reply_to） |
| G9「herdr run のsame-space pane生成」「herdr run paneのexit時close契約」 | 全部（`ikesu run --pane [--split right\|down] [--close-on-success]`） |
| G5「fail-closedなcurrent pane解決」 | 土台で作った `pane current` を節の受け入れ条件と照合し、足りない分（引数付きで usage・exit 2、`IKESU_PANE_ID` のサーバー検証、process session の照合）を足す |
| G5「短いpane targetとAI向けhelp」 | `ikesu help` と `ikesu --help` を同一にし、AI 向けの help 本文（When To Use / Agent Rules / Usage / Commands / Essential Agent Recipes / Options / More Help）を herdr の構成で書く。pane target は `N`・`%N`・`pN`・`p_N` を受け付ける。旧 ID の別名・snapshot 関連は作らない |
| G5「agent sendとheadless入力の確実な送信」 | `agent send`（文字 → 500ms → Enter）、`pane.send_input` の text と keys の間の 500ms、`pane run` は1要求で送る。headless 入力の部分は土台の画面転送で満たしているかを確認するだけ |
| G5「leaked HERDR_ENVを無視するnested guard」 | 全部（環境変数だけでは拒否せず、祖先に ikesu サーバーのプロセスがいるときだけ入れ子として拒否。設定 `[experimental] allow_nested`） |
| agent コマンド（herdr の `agent` help、docs/ref/herdr-cli-ai.txt） | `agent list / get / read / send / start / rename / focus / wait`。`agent prompt / attach / restore / explain` は作らない |
| G4「agent通知タイトルと本文抽出」 | 全部（画面内のトースト、タイトル `<workspace番号> <workspace名>`、本文抽出の規則、設定 `[ui.toast] delivery = "off"` が既定。値は `off` / `ikesu`（画面内）/ `terminal` / `system`） |
| G4「agent通知rate limit」 | 全部 |
| G4「通知クリックでpaneへ移動」 | トーストのクリック、`keys.open_notification_target`（既定 prefix+o）、macOS の `terminal-notifier -execute '<ikesu> pane focus <pane>'`。Antigravity（agy）の検出は ikesu の検出表（`internal/detect` の manifest）に agy が無ければ足す |
| G4「render drain通知の正確化」 | 土台の画面転送で、画面への書き込み成功後にだけ「描画済み」とみなすことを確認・必要なら直す |

作らないもの: G3（会話の復元）、G1・G2・G6（画面の機能）、G9 の「local overlay pane」「alternate binary session隔離」以降、G11（複数マシン）、`--machine`。

## 2. 決まっていること

| 項目 | 決定 |
|---|---|
| 保存先 | `modernc.org/sqlite`。DB は `$IKESU_STATE_DIR/ikesu.db`（セッションをまたいで1つ。herdr の `~/.config/herdr/herdr.db` に相当）。WAL モード |
| 表の形 | herdr の表（`docs/ref/herdr-db-schema.sql`、`actors`・`dispatches` と索引・ビュー）と同じ列名・同じ状態名にする。ikesu に要る分だけ列を足すのはよいが、herdr にある列の意味を変えない |
| DB への書き込み | サーバーだけが書く（CLI は API 経由。SPEC「CLIはHerdr DBへの書込権限を必要としない」） |
| ジョブの完了通知 | 呼び出し元ペインへ `[ikesu run] exit=<code> label=<label> job=<id>` の1行（`--pane` のときは `pane=<pane>` も）。herdr と同じく `ikesu-jobs` は通常のメールと別の経路（作業中でも直接入れて既読にする） |
| ジョブのログ | `$IKESU_STATE_DIR/job-logs/<job_id>.log` |
| 設定ファイル | `~/.config/ikesu/config.toml`（`$IKESU_CONFIG` で上書き）。この段階で読むのは `[ui.toast] delivery`・`[experimental] allow_nested`・`[keys] open_notification_target` だけ。TOML は既存の依存 `BurntSushi/toml` |
| 環境変数 | ペインには土台の `IKESU_PANE_ID=p_N`・`IKESU_SOCKET_PATH` に加え、herdr と同じく `IKESU_WORKSPACE_ID=sN`・`IKESU_TAB_ID=sN:tM` を出す |
| 依存 | 新しく足してよいのは `modernc.org/sqlite`（とそれが引き込むもの）だけ。cgo を使わない |

## 3. 守る方針

- 土台の方針（状態は純粋なデータ、API も画面も同じコマンドで状態を変える、1ファイル 800 行まで）を守る。
- メール配送は「送信時・agent の状態が Idle/Working/Done に変わった時・起動時1回」のイベントでだけ動かす。通常の API 要求のたびに全 agent を走査しない（SPEC のデグレ判定）。
- 推測で埋めない: 宛先・呼び出し元ペインが決まらないときは送らず、ジョブも始めずにエラーで止める。
- 性能: 区切りごとに mini で200万行（`tools/bench/bench_screen.sh`、サーバー経由と `--no-session`）を測る。基準は「main（84a112b）と同じ時間帯に交互に測って、中央値で 5% 以上遅くならない」。mini の負荷が10以上のときの値は使わない。
- ビルド: SQLite を足す前後で、mini の「最初から」と「1ファイル変更後」のビルド時間（開発用・配布用）と配布用バイナリの大きさを測って RESULT.md に書く（店長への報告に使う）。

## 4. マイルストーン（この順。各区切りで gofmt・go build・go vet・go test（最後は -race も）が mini で通ってからコミット）

1. **保存と宛先の土台**: SQLite の組み込み（ビルド時間の前後計測を含む）、`actors`・`dispatches`、宛先の解決（agent 名・`pN`・`%N`・`N`）、設定ファイルの読み込み、`IKESU_WORKSPACE_ID`・`IKESU_TAB_ID`。
2. **メールボックス**: `send`・`inbox`・`msg history`・`msg rooms`、配送の規則と PTY 全バイト書き込み確認。
3. **裏方ジョブ**: `run`（裏方・`--pane`）・`run list`・`run cancel`・`log`・完了通知。
4. **agent コマンドと CLI の安全性**: `agent list/get/read/send/start/rename/focus/wait`、`pane current` の不足分、入れ子の判定、AI 向け help。
5. **通知**: トースト、本文抽出、rate limit、クリックとキーでの移動、terminal-notifier。
6. **仕上げの確認**: 5章の動作確認、性能とビルド時間の計測。

## 5. 受け入れ条件（親がレビューで確かめる）

- 1章の各節について、SPEC.md の「受け入れ条件」を1つずつ確かめた記録（テスト名または実物の画面・出力）が RESULT.md にある。確かめられなかったものは未達として理由を書く。
- 実物確認: ikesu の中で2つのペインに claude と codex（または Devin）を起動し、`ikesu send <相手> "..."` で作業中・待機中の両方に本文が届くこと、`ikesu run --label t -- sleep 2; echo done` の完了通知が呼び出し元に1行で届くこと、`ikesu agent start` で起動した agent が `ikesu agent list` に出ることを、ptyrun で保存した画面で示す。
- サーバーを止めて起動し直しても、未配送のメールと、ジョブの状態・ログが残っている。
- 性能とビルド時間が 3章の基準を満たし、数値が RESULT.md にある。

## 6. 動作確認・やってはいけないこと

- 計測・結合テスト・race は mini（`ssh mini`、`~/bench-terrace.noindex`）で行う。メインの Mac では go build と軽い go test 以外の重い処理をしない。
- 稼働中の herdr と herdr fork を変更しない。herdr の DB（`~/.config/herdr/herdr.db`）を開くのは読み取り専用（比較のため）だけ。
- 1章に無い機能を作らない（発見事項に書くだけ）。push・amend・reset・force push しない。
