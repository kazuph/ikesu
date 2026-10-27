# herdr 仕様書の 0.2.40〜0.2.49 の増分を ikesu に入れる計画

作成: 2026-10-14 夜（親 Claude）。前の計画 `docs/PLAN-spec-delta-1014.md`（herdr 0.2.40 時点の SPEC まで）の続き。

## 依頼

店長方針: herdr fork の `SPEC.md` を ikesu に全部実装する（優先順位なし）。herdr の修正が落ち着いたので追従を再開する。herdr の SPEC.md を `kazuph-v0.2.40` と `origin/main`（0.2.49 相当）で比べた増分のうち、ikesu に足りないものを2段階に分けて、別々の worktree で並行して作る。

作らないもの: slack-decisions plugin 側の動き（Slack 投稿の作り方・使い回し・削除・`resolveHerdrBin` の探し方。herdr の plugin のコードであって ikesu の本体ではない。ikesu 側は plugin へ渡す判断待ちのデータと環境変数を SPEC どおりにする）、以前からの対象外（単発の `--remote`、manifest の配信系、旧形式の移行、marketplace、自動更新、共有 remote cache、Windows）。

## 0. 作り方の決まり（両段階共通・前の計画と同じ）

- herdr のソースコード（`src/**` など実装・テスト）と saberante などほかのプロジェクトのソースを読まない・移植しない（AGENTS.md）。挙動は herdr fork の `SPEC.md`、herdr の利用者向け文書、CLI の help、公開 manifest（`website/agent-detection/*.toml` は仕様データとして読んでよい）、herdr 実物（brew の 0.2.49 を一時 HOME・名前付き session で）の観察、本物の Claude Code・Codex の画面の観察で決める。
- 店長の稼働中の herdr・ikesu・本物の設定と state・クリップボードに触らない。本物の Claude Code の確認は、server は一時 HOME、pane の Claude だけ `HOME=/Users/kazuph` で一時フォルダを cwd にし、`--permission-mode default`（bypass 系なし）。ログイン情報を一時 HOME へコピーしない。確認後は終了させる。
- テスト・race・結合テストは Mac mini（`ssh mini`、`~/bench-terrace.noindex`）か ubuntu-3090（`ssh ubuntu`、`. ~/bench-terrace/env.sh`、作業は `~/bench-terrace/work/<名前>`）。メインの Mac では go build・go vet・軽い go test だけ。スクリプトの先頭で `export PATH=/usr/bin:/bin:<go の場所>` にしてから何かを消す（見張りの自己再帰の事故を防ぐ）。
- 変えてはいけないもの（変えたいときは実装前に親へ報告して止まる）: go.mod、計画に書いていない既存の API・CLI・保存形式・endpoint generation・protocol。1ファイル800行まで。計画に無い機能を作らない。未対応の部分は似た値で埋めず、未対応として未達に書く。
- 各項目は、まず ikesu に既にあるかを確かめ、あれば「既にある」と証拠（テスト名か出力）を書いて作らない。

## 1. 段階C: 画面の質問と判断待ち（worktree `feat/spec-delta-c`）

1. **判断待ちの出どころの名前**（SPEC「判断待ち」挙動・受け入れ）: 作成時（`decision.create` と画面の質問の判断待ち）に `origin.pane_id` を手元の pane へ解決できたら、`origin.workspace_label`（スペース名）・`origin.tab_label`（タブ名）・`origin.pane_label`（pane の label か端末の題名）を補う。呼び出し側が指定した値は保つ。解決できない pane では3つとも出さない。古い payload も読める。`decision watch`/`decision get` の JSON に出る。
2. **`ikesu decision answer <id> <option_id> [<option_id>...] [--text <text>]`**: 複数の選択肢を `option_ids` で送れる。1件だけの書き方は変わらない。
3. **画面の質問の走査の追加分**（SPEC 工程6 の追加の文と受け入れ条件すべて）:
   - 検出用の画面に、表示領域の上の scrollback 24行を含める。pane より高い許可確認の箱でも、箱の見出し・コマンド全文・直上の `⏺ Tool(...)` 行が題名に入る。`⏺` 行を足すのは許可確認だけで、AskUserQuestion（チェック見出しか自由入力欄がある質問）には足さない。
   - 揮発表示（`in M:SS`・`(Ns)`・スピナー文字）を署名の前に正規化する。Claude Code の自動拒否の警告（狭い pane で複数行に割れても同じ）は質問文から外し、数字を除いた文を `body` に載せる。
   - 揮発表示だけが変わった同じ質問は、同じ `decision_id`・作成時刻のまま内容を更新し、作成・解決のイベントを出さない。同じ pane が30秒以内に3回を超えて取り消し・作り直しを繰り返したら、選択肢が同じ作り直しもその場更新に切り替える。
   - 外部入力の記録: TUI のキー・貼り付け・マウス転送、remote/headless クライアントの入力、attach 端末の入力、`pane send-text`/`send-input`/`send-keys`・`agent send`・msg の注入を受けた時刻を pane ごとに記録する（ikesu 自身が画面の質問のために送るキーは含めない）。直前3秒以内に外部入力があった pane には、画面の質問由来のキー（プレビュー走査の矢印、回答の配送、遅延書き込み、確定画面の自動 Submit）を送らない。走査中に入力が来たら打ち切り、復帰の矢印も送らない。判断待ちの作成・取り消し・その場更新も止め、入力が止まってから1回だけ反映する。その間の回答は `pane_user_active` で拒否し、判断待ちは pending のまま。回答確定後に入力が割り込んだら送らずに捨てる。
   - プレビュー走査の「1回きり」は質問文と選択肢の文面の組で判定する。人の入力中に初めて見えた質問は走査済みとして記録し、後から走らない。
   - 送るだけの確定画面（`Submit answers`/`Cancel` の最終確認、またはタブ行が画面外に出た後の「回答を送信する」質問と `Submit…` の選択肢だけの画面）は判断待ちにせず、走査もキーも送らない。同じタブ組の全質問が ikesu 経由の回答で答えられていて、回答開始後に外部入力が無い時だけ、Submit を1回送る（選択中なら Enter、そうでなければ番号キー。矢印は使わない）。送信失敗はログに書き、送信済みにしない。
   - 自由入力欄に文字が打たれて選択肢の文面が変わっても、区切り線と `Chat about this` の位置で自由入力欄と認識する。打ち途中の文字は選択肢・説明文・プレビュー・署名に入れない。
4. **判断待ちの pane を blocked に見せる**（SPEC「判断待ちペインへの注意表示投影」）: pending な判断待ちの `origin.pane_id` が手元の pane を指す間、その pane の状態を blocked に重ねる（画面判定・hook の元の値は書き換えない）。作成・解決・起動時・live handoff 復元時に同期する。サイドバーの赤い丸・Request 音・通知・endpoint の画面情報は既存の経路で出る。画面で既に blocked なら遷移・音・toast を重ねない。エージェントのいない shell の pane でも同じ。解決できない pane id は手元の pane へ当てない。API・イベント・protocol は変えない。

## 2. 段階D: そのほか（worktree `feat/spec-delta-d`）

1. **通知なしの `ikesu run` はエージェントのいない pane からも起動できる**（SPEC「通知なしのherdr run」）: `--completion none` は存在する pane なら呼び出し元にできる。`summary`/`full` はメールボックスを持つエージェントの pane でなければ `agent_not_found`。存在しない pane は `none` でも拒否し、job を作らない。（いま ikesu は `summary` でも pane id を宛先にして受け付けているので、SPEC に合わせて拒否する。ikesu 内で `summary` を使っている呼び出しが壊れないかも確かめる。）
2. **Claude の質問の footer が改行されても blocked**（SPEC agent manifests）: Enter・移動・Esc の操作案内が端末幅で改行されても同じ blocked 判定にする。案内全体が続く footer だけが対象で、本文の別々の「esc to」と「cancel」では blocked にしない。複数タブ・multiSelect の質問も狭い pane で blocked になり、画面の質問の判断待ちが作られる。広い pane の判定と通常の idle は変えない。公開 manifest `website/agent-detection/claude.toml`（2026.10.14.1）を仕様データとして参照してよい。
3. **plugin に渡す実行ファイルのパス**（SPEC「判断待ち」挙動）: plugin の runtime・pane・action に渡す `IKESU_BIN_PATH` は、server の実行ファイルが Homebrew の Cellar の版番号入りパスにある時、`<prefix>/opt/<name>/bin/<bin>`、無ければ `<prefix>/bin/<bin>`、どちらも無ければ元のパスにする。
4. **Tailscale の承認ページは同じマシンにつき全クライアントで1回だけ開く**（SPEC 保存マシン）: 開いたクライアント・URL・時刻を state dir の共有記録に残し、記録がある間は別クライアントや自動再接続では開かない。接続が完了したら記録を消す。承認されずに待ちが終わったマシンの表示を「承認待ち。クリックで承認ページを開く」にする。見出しのクリックで記録を更新してつなぎ直し、新しい URL を1回開く（試行中の待ちならその ssh を切ってすぐつなぎ直す）。
5. **見張りの `guard exec` は全部の shim を PATH から外す**（SPEC 見張り）: `<state>/guard/bin` の下のすべての shim のディレクトリ（自分のものを含む）を PATH から外して本物を探す。shim のディレクトリを示す環境変数が無く、PATH にだけ shim が残る子プロセスでも本物を実行し、自分を呼び続けない。

## 3. 進め方と確認（両段階共通）

- 区切りごとに gofmt・go build・go vet・go test を通して積み上げコミット。最後に go test -race ./...。TestRelative・TestAbsolute は実行しない（親が取り込み前に測る）。
- `.artifacts/<ブランチ名>/RESULT.md` に、計画の各項目を受け入れ条件ごとの表（条件・確かめたテスト名か保存した出力・状態）で書く。確かめていないものは未達と書く。失敗側の条件（拒否・`pane_user_active`・`decision_stale`・`agent_not_found`・自己再帰しないこと）も確かめる。画面の質問と注意表示は本物の Claude Code で実地確認する。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
