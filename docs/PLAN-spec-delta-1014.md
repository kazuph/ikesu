# herdr 仕様書の 10/11 以降の増分を ikesu に入れる計画

作成: 2026-10-14（親 Claude）。

## 依頼

店長方針: herdr fork の `SPEC.md` を ikesu に全部実装する（優先順位なし）。2026-10-11〜10-13 に herdr の SPEC.md へ増えた契約を ikesu と突き合わせた結果、次の不足が見つかった。2つの段階に分け、別々の worktree で並行して作る。

突き合わせで「ikesu に既にある」と確かめたもの（作らない）: 保存マシンの画面中継を ssh のコマンド実行の stdio でつなぐこと（ikesu は `__endpoint`）、Tailscale の承認待ちが終わったマシンを「承認待ち。クリックで再試行」で止めること、`decision watch` の Tailscale 認証 10 分待ち、TUI の判断待ちダイアログの廃止（009d81f）、見張り（guard）の enforce・record・差し込み（42aaeca）。

## 0. 作り方の決まり（両段階共通）

- herdr のソースコード（`src/**` など実装・テスト）を読まない・移植しない（AGENTS.md）。saberante など他プロジェクトのソースも読まない。挙動は herdr fork の `SPEC.md`、herdr の利用者向け文書（`~/src/github.com/kazuph/herdr/docs/` の実装以外）、CLI の help、herdr 実物の観察、本物の agent（Claude Code・Codex）の画面の観察で決める。
- herdr 実物は 0.2.40（ubuntu-3090 の `/usr/local/bin/herdr` は 0.2.22 で古い。GitHub release `kazuph-v0.2.40` の `herdr-linux-x86_64` を一時フォルダへ置いて使うか、Mac の brew 版 `herdr` を一時の HOME・名前付き session で使う）。店長の稼働中の herdr・ikesu・本物の設定と state に触らない。
- 本物の Claude Code・Codex の画面観察と実地確認は Mac で行う（3090 には入っていない）。一時フォルダ・名前付き session の ikesu/herdr の中で起動し、`--permission-mode default`、bypass 系オプションは使わない。
- テスト・race・結合テストは ubuntu-3090（`ssh ubuntu`、`. ~/bench-terrace/env.sh`、作業は `~/bench-terrace/work/<名前>`）。メインの Mac では go build と go vet と軽い go test だけ。
- 変えてはいけないもの（変えたいときは実装前に親へ報告して止まる）: go.mod、計画に書いていない既存の API・CLI・保存形式・endpoint generation。1ファイル800行まで。計画に無い機能を作らない。未対応の部分は似た値で埋めず、未対応として未達に書く。

## 1. 段階A: 子プロセスの環境・session 観測・宛先の厳密化（worktree `feat/spec-delta-a`）

1. **子プロセスへ agent session の印と見張りの状態を引き継がない**（SPEC「inherited agent-session環境変数をpane childへ引き継がない」）。ikesu が子プロセスを作る全経路（新規・split・復元の pane、`ikesu run` の job、plugin の pane/process、その他）で、SPEC に列挙された session の印（`CLAUDE_CODE_CHILD_SESSION`・`CODEX_THREAD_ID`・`HERDR_AGENT` ほか。ikesu 固有の同等の変数があればそれも）と見張りの状態変数（ikesu での同等の名前）を消す。server 起動時（`ikesu server` と単体起動）は自分の環境からも先に消す。設定系の変数（`CLAUDE_CONFIG_DIR`・`CODEX_HOME`・`CLAUDE_EFFORT` など）は消さない。呼び出し元が明示した pane の env と見張りの差し込みは消した後に入れる。`BASH_ENV`・`ZDOTDIR`・`PROMPT_COMMAND` は見張りが入れた値だと確証できる時だけ消す。受け入れ: `CLAUDE_CODE_CHILD_SESSION=1` などを持つ環境から起動した ikesu server の pane で本物の Claude Code を起動すると、transcript 保存が止まらず `~/.claude/sessions/<pid>.json`（一時 HOME 内）ができ、ikesu の agent session にその id が記録される。
2. **Codex の session id 観測の強化**（SPEC G3 の Codex rollout scan）。実行中の Codex が開いている `.jsonl` を同じ基準で見る。rollout の候補は file の mtime と `session_meta.payload.timestamp` が process 開始 −120 秒以降のものに限り、窓の中に読めない・判定できない rollout が1つでもあれば未確定にする。`codex resume --last` の `--last` を id として記録しない。
3. **同名 agent 宛の送信と `ikesu run` の完了通知の宛先を厳密にする**（SPEC の mailbox と `herdr run` の節）。同じ agent 名が複数 pane にある時の名前宛 `send` は拒否し、本文・宛先・理由を failed として記録する。解決済みの宛先は global pane ID で保存する。`ikesu run` の caller は作成時に pane ID で保存し、完了通知とキャンセル通知はその pane ID にだけ送る。名前宛の完了通知は拒否して failed に保存する。server 不在時は確定済みの caller pane ID へ durable queue する。caller 不明・消失・曖昧は配送せず failed に残す。旧形式の行（不変 caller の証拠が無いもの）は caller 空として扱い、旧 queued の名前宛 job 通知は本文を残して failed にし、再送しない。
4. **`decision watch --all-machines` で decision を知らない保存マシン**（SPEC 工程3A）。`unknown command: decision` 等を見たらそのマシンを未対応とし、未対応と書いた reason の `machine_down` を1回だけ出し、そのマシンだけ30分おきに再試行する。実際の watch 行が来たら未対応を解除する。他マシンの再試行間隔は変えない。

## 2. 段階B: 判断待ちの選択肢の説明と、agent 画面の質問の判断待ち化（worktree `feat/screen-prompt-decisions`）

1. **選択肢の説明**: 判断待ちの `options[]` に任意の `description`（無ければ JSON に出さない、古い payload も読める）。`ikesu decision ask --option-desc <id:text>`（繰り返し可、未知の id はエラー）。`decision watch` の行にも載る。
2. **複数選択と付加項目**: 判断待ちの `multi_select`、回答の `option_ids`（単一 `option_id` との互換を保ち、`option_ids` の無い古い payload も読める）、選択肢の `preview` と `preview_unavailable`、判断待ちの `question_label`。項目名・形は herdr 0.2.40 の API（`decision.get` などの実出力と herdr の利用者向け文書・生成された API スキーマ）にそろえる。
3. **agent 画面の質問の判断待ち化**（SPEC 工程6 全部）: `[decisions] screen_prompts`（既定 true、live reload で false にすると全 watch を止めて pending をキャンセル）。blocked な agent pane の検出用の画面（soft wrap を戻した論理行）を走査し、Claude Code の AskUserQuestion（単一・複数タブ・multiSelect・プレビュー付き）と許可確認、Codex の許可確認、Devin の確認画面を `agent_prompt` / `tool_permission` の判断待ちにする。選択肢の説明、フル幅ハイライトで接着された行の分割、自由入力選択肢の `allow_text` 化、`Next`/`Submit` 行、プレビューの矢印キーだけの1回きりの走査と復帰、タブ行の `question_label`。回答のキー送り（ショートカット・番号・text+Enter・複数選択の順送りと確定）。画面署名と2回連続不一致でのキャンセル、同一内容の再利用、別経路の決着で再提示しない、回答直前の画面確認と `decision_stale`、idle で loop を起こさない、`ping` と `ikesu status --json` の `screen_prompt_decisions: true`。
   - 画面の読み取り規則は SPEC の文と、本物の Claude Code・Codex の画面と herdr 0.2.40 の実際の出力（同じ画面から herdr が作る判断待ちの JSON）の観察で決める。Devin の確認画面は Devin を起動できない（店長方針で使わない）ため、herdr 実物の観察か SPEC の記述だけで作り、実地確認は未達と書く。

## 3. 進め方と確認（両段階共通）

- 区切りごとに 3090 で gofmt・go build・go vet・go test を通して積み上げコミット。最後に 3090 で go test -race ./...。TestRelative・TestAbsolute は実行しない（親が取り込み前に測る）。メインの Mac で go build ./... と go vet ./...。
- `.artifacts/<ブランチ名>/RESULT.md` に、計画の各項目を受け入れ条件ごとの表（条件・確かめたテスト名か保存した出力・状態）で書き、herdr 0.2.40 と ikesu の出力の比較を載せる。確かめていないものは未達と書く。失敗側の条件（拒否・キャンセル・`decision_stale`・failed 記録）も確かめる。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
