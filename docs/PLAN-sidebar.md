# gtx サイドバーとワークスペース UI の段階の計画（SPEC G2）

作成: 2026-10-08（親 Claude）。店長の方針「SPEC.md のとおりに全部実装する。優先順位は無い」と、リポジトリの `AGENTS.md`「仕様から実装する。移植しない」に基づく。

## 0. 作り方の決まり（最優先）

- herdr のソースコード（`~/src/github.com/kazuph/herdr` の `src`・`tests` などの実装とテスト）を読まない。
- 仕様の正は `~/src/github.com/kazuph/herdr/SPEC.md` の G2 の文章、`docs/ref/` の API スキーマと CLI help。`herdr` → `gtx`、`HERDR_*` → `GTX_*` と読み替える。
- 文章で決まらない見た目や操作の細部は、mini で herdr の配布用ビルドを別に動かして観察する（`~/bench-terrace.noindex/work/herdr-target/release/herdr --no-session` を ptyrun の中で動かす。設定は `~/bench-terrace.noindex/work/hx/` を使い回す）。店長が使っている稼働中の herdr には触らない。それでも決まらなければ親へ質問する。
- 各節の「受け入れ条件」と「デグレ判定」を、そのまま gtx の受け入れ条件として扱う。「本家」（ogulcancelik/herdr）の機能が前提の部分も、観察した挙動に合わせて gtx 独自に作る。

## 1. 作る節（G2 の全節）

「サイドバー幅プリセット」「Workspace Sections」「workspace次/前移動はサイドバー表示順」「サイドバー空白メニューと危険操作確認」「Workspace複製」「ワークスペースGit統計表示」「Slim密度とAgents並び替え」「Pane target表示」「Workspaceメニューとサイドバー操作」「Workspaceセクションと幅プリセット」「Worktree名とコピー状態」「sidebar・mobile・dragの微細契約」「workspace ownerにスコープされたpopup terminal」。

「Space cardのjob dot」は保存と復元の段階で作り済み。この段階で受け入れ条件を確かめ直し、足りなければ直す。

## 2. 守ること

- これまでの段階と同じ: 状態は純粋なデータ、API と画面は同じコマンドで状態を変える、1ファイル 800 行まで、推測で埋めない、未対応は未対応と書く、計画1章に無い機能は作らない、依存ライブラリを足さない。
- API・CLI の名前と項目は `docs/ref/herdr-api-schema.json` と同じ（例 `workspace.duplicate`・`workspace.move`・`workspace.set_section`・`workspace.report_metadata`）。
- 受け入れ条件は1つずつ、SPEC の文どおりの入力と期待値を直接確かめるテスト、または ptyrun で保存した画面と自動判定で確かめる。
- Git の統計やワークツリー名は、一時フォルダに作った git リポジトリで確かめる。店長の本物のリポジトリを触らない。店長のクリップボードを触らない。
- 性能: 区切りごとに mini で `go test -tags perf ./tests/perf -run TestRelative`（基準は main 37b5b3b のビルド）を全項目合格にする。

## 3. 進め方と確認

- 区切りは自分で決めてよい（目安: 幅と密度・並び替え → セクション → メニュー・複製・危険操作確認 → Git 統計・worktree 名 → 微細契約・popup → 仕上げ）。区切りごとに mini で gofmt・go build・go vet・go test（最後は -race も）を通してから積み上げコミット。
- 計測・結合テスト・race は mini（`ssh mini`、`~/bench-terrace.noindex`、自分専用の同期先フォルダ）。メインの Mac では go build と軽い go test だけ。
- `.artifacts/sidebar/RESULT.md` に、SPEC の節ごと・受け入れ条件ごとに「確かめたテスト名、または保存した画面・出力」の表を書く。確かめていない条件は未達と書く。gtx に無い仕組みが前提の条件は、理由を書いて一覧にし、最後の報告で親に出す。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
