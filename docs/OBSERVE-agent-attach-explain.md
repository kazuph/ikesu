# agent attach / explain の実物観察（2026-10-13）

ubuntu-3090 の配布済み herdr 0.2.22 を使用。毎回 `tempfile.mkdtemp` の HOME と XDG ディレクトリ、`--session observe` を指定し、HERDR_/GTX_ 環境変数を除外した。本物のサーバーには接続していない。終了は同じ名前付き session の `server stop` のみ。

- `agent explain --file PATH --agent codex --json` は説明オブジェクトのみを stdout に1行で出し、終了0。既知のルールに一致しない空画面は idle、fallback_reason は `default_known_agent_idle_fallback`。
- 未知ラベルも終了0。state=unknown、fallback_reason=`unknown_agent`、manifest_source/manifest_version/matched_rule=null、evaluated_rules=[]。
- 遠隔更新に関する cached_remote_version/remote_update_status/remote_update_error/warning は null。local_override_shadowing_remote=false。
- ルールの項目は id/state/priority/region/matched/evidence。evidence は all_count/any_count/not_count/contains/regex/line_regex/region_bytes/region_preview。
- 通常表示は `agent: ...`、`state: ...`、`manifest: SOURCE VERSION`、`rule: none`、必要なら `fallback_reason: ...`。
- 名前だけ付けた普通の shell に explain すると `agent_explain_unavailable`（終了1）。説明には検出された agent label が必要。
- 不在の対象は `agent_not_found`、終了1。explain の要求IDは `cli:agent:explain`、attach の解決要求IDは `cli:agent:attach:resolve`。
- attach の引数不足は usage と終了2、不明オプションは `unknown option: ...` と終了2。
- 有効な shell を rename して attach の解決に成功。TTY がない呼び出しは herdr の端末初期化が panic（終了101）。TTY での成功・競合・takeover の確認は attach 実装区切りで追加する。
- 利用者向け agents.mdx / cli-reference.mdx の契約: 対象端末のみを表示、ctrl+b q で detach、ctrl+b ctrl+b で literal prefix、PageUp/PageDown・wheelで scroll、通常入力で最下部へ戻る。controller は1本、--takeover で所有者を交代する。

生出力は `.artifacts/agent-attach-explain/herdr-*-observations.jsonl`（非追跡）。herdr の実装・テストソースは読んでいない。
