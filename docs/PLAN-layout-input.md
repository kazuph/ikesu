# gtx レイアウト・入力・コピー・worktree の段階の計画（SPEC G1・G6・G7）— 仕様からの作り直し版

作成: 2026-10-08（親 Claude）。店長の方針「SPEC.md のとおりに全部実装する。優先順位は無い」に基づく。

仕様の正は `~/src/github.com/kazuph/herdr/SPEC.md`（読むだけ。`herdr` → `gtx`、`HERDR_*` → `GTX_*` と読み替える）。
各節の「UI挙動」「受け入れ条件」「デグレ判定」をそのまま gtx の受け入れ条件として扱う。
**この段階はリポジトリの `AGENTS.md`「仕様から実装する。ほかのプロジェクトの実装を移植しない」に従って作る（2026-10-08 改訂）。**
herdr のソースコード（`~/src/github.com/kazuph/herdr/src` など）と、前の試作ブランチ `feat/layout-input` のコードは読まない。
仕様の正は SPEC.md の文章、`docs/ref/` の API スキーマと CLI help。文章で決まらない細部は、mini で herdr の配布用ビルドを別に動かして
（`~/bench-terrace.noindex/work/herdr-target/release/herdr --no-session` を ptyrun の中で。設定は `~/bench-terrace.noindex/work/hx/` を使い回す。店長が使っている稼働中の herdr には触らない）画面や出力を観察し、その挙動に合わせる。
それでも決まらないときは親へ質問する（推測で埋めない・ソースを読まない）。
「本家採用へ移行 (A)」「本家基盤＋fork差分保持 (B)」の節で本家の機能を前提にしている部分も、同じく観察した挙動に合わせて gtx 独自に作る。

## 1. 作る節（全部、節の内容どおり）

| グループ | 節 |
|---|---|
| G1 レイアウト操作 | 「tab・workspace間pane移動」「pane menuのレイアウト操作」「レイアウト cycle」「非明示pane追加は既存順の末尾へ追記」「pane 回転と target 安定化」「bottom pane action bar と pane title zoom」「pane描画・resize・mouse routingの微細契約」 |
| G6 入力・コピー | 「pane copy mode本体」「右角括弧でもcopy modeに入る」「tmux風split default keybind」「Copilot CLI向けraw LFとGhostty入力互換」「pane内NO_COLORを引き継がない」「mouse routing・overlay・helpの微細契約」「SGR 1016 paneへのcell中心pixel配送」 |
| G7 worktree操作 | 受け入れ条件のうち Windows 以外の全部（サイドバー・CLI・API からの作成／既存 branch の checkout／安全な削除／削除後の focus／workspace group／event／bare repository）。CLI・API の形は `docs/ref/herdr-api-schema.json` の `worktree.*` と `docs/ref/herdr-cli-worktree.txt`（`herdr worktree --help` の写し）に合わせる |

作らない: G6「Vim風ターミナル操作モード」（SPEC で破棄）。

## 2. 守ること

- これまでの段階の方針（状態は純粋なデータ、API と画面は同じコマンドで状態を変える、1ファイル 800 行まで、推測で埋めない、計画に無い機能は作らない）。
- API・CLI を足すときは herdr の API スキーマ（`docs/ref/herdr-api-schema.json`）と同じメソッド名・項目名にする（例 `pane.move`・`pane.swap`・`pane.zoom`・`layout.*`・`worktree.*`・`pane.copy_*`）。
- 性能: 区切りごとに mini で `go test -tags perf ./tests/perf -run TestRelative`（`GTX_PERF_BASELINE_BIN` は main 62474d6 のビルド）を流し、全項目合格を RESULT.md に書く。1回目で僅差の不合格が出たら、もう一度流して両方の結果を書く（片方だけを選んで書かない）。
- 店長の本物のリポジトリで worktree を作らない。worktree のテストは一時フォルダに作った git リポジトリ（bare を含む）で行う。

## 3. 進め方と確認

- 区切り: (1) G1 のレイアウト操作と API、(2) G1 の action bar・zoom・描画の微細契約、(3) G6 の copy mode とキー割り当て、(4) G6 の入力互換・NO_COLOR・マウスの微細契約・SGR 1016、(5) G7 worktree、(6) 仕上げ（実物確認・性能）。
  各区切りで gofmt・go build・go vet・go test（最後は -race も）が mini で通ってから積み上げコミット。
- 実物確認は ptyrun で画面を保存（ペインのメニュー操作、cycle・回転・zoom、copy mode での選択とコピー、worktree の作成・削除）。
- 計測・結合テスト・race は mini（`ssh mini`、`~/bench-terrace.noindex`、自分専用の同期先フォルダ）。メインの Mac では go build と軽い go test だけ。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。店長のクリップボードを読まない・書き換えない（コピーのテストは gtx 内部のコピー先か一時的な置き換えで行う）。
