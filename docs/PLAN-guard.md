# 危険コマンドの見張り（command guard）の段階の計画

作成: 2026-10-11（親 Claude）。

## 依頼

店長（2026-10-11）: SPEC.md を全部実装する方針のもと、段階を取り込んだら止まらず次へ進めてよい。前の段階が main 473a2b4 に入ったので、残項目一覧（`.worktree/feat/endpoint/.artifacts/endpoint/SPEC-REMAINING.md` の残項目26〜29）の G12「危険コマンドの見張り」をこの段階で作る。

店長の決定（2026-10-11）:
- 判定規則は、店長の dotfiles の ai_guard（`~/dotfiles/.ai_guard.zsh`、確かめ用 `~/dotfiles/tools/test-ai-guard-approval.zsh`）を読んで、同じ判定を gtx のコードとして書いてよい。dotfiles は読むだけで変えない。
- 見張りが効いている pane の印は `GTX_GUARD_ENFORCING=1`（と印ファイルを指す `GTX_GUARD_MARKER`）。dotfiles の `~/dotfiles/.config/shell/load-ai-guard.sh` が gtx の印でも自分を読み込まないようにする変更は、実装役は行わず、差分の案を RESULT.md に書いて親へ渡す（親が店長に見せてから入れる）。

## 0. 作り方の決まり（最優先）

- herdr のソースコードを読まない・移植しない（AGENTS.md）。ai_guard は herdr ではないので読んでよい。
- 仕様の正: `~/src/github.com/kazuph/herdr/SPEC.md` の G12 の4節（判定部品、ペインへの差し込み、エージェントペインだけの判定と記録のみモード、既定の enforce モード、ai_guard との突合）。`herdr` → `gtx`、`HERDR_GUARD_*` → `GTX_GUARD_*` と読み替える。
- 判定の細部（対象コマンド一覧・文言・一時許可キーの形・日付キー・ログの形）は ai_guard に合わせる。SPEC と ai_guard が食い違ったら親へ報告して止まる。
- 見本の herdr 0.2.24 に `guard` が入っているかは実物で確かめる（入っていれば一時の state で観察してよい。無ければ SPEC と ai_guard だけで作る）。

## 1. やること

1. **判定部品**（残項目26）: `gtx guard check`・`gtx guard exec`。コマンド名・引数・cwd・起動ディレクトリ・環境から `allow`/`block`/`ask` と理由。`[guard]` の `targets`・`danger_words`・`protected_patterns`・`wrangler_allow` で調整。1日許可と一時許可のキーは ai_guard と同じ。
2. **ペインへの差し込み**（残項目27）: pane ごとの `<state>/guard/bin/<p_N>` を PATH の先頭に置き、印が無い間は空。zsh の ZDOTDIR と bash の BASH_ENV の連鎖（利用者の起動ファイルと HISTFILE の場所を変えない、利用者の同名の関数・エイリアスを上書きしない）。`[guard].enabled = false` では何も差し込まない。pane 識別の無い spawn（popup など）には差し込まない。
3. **エージェントペインだけの判定と記録のみモード**（残項目28）: `<state>/guard/agent-panes/<p_N>.json` の印をエージェントの開始・解除・pane 終了・close に同期。人の pane では判定も記録もしない。`decisions.jsonl` の1行1JSON。`mode = "record"` は判定と記録だけでコマンドを止めない。
4. **既定の enforce モード**: `block` は ai_guard と同じ文言で止める（終了 code 1）。`ask` は申請文（`AI_GUARD_REQUEST_REASON` など、ai_guard と同じ検査）を確かめてから、前の段階で作った判断待ち（`command_guard`、選択肢 承認・1日許可・却下、自由記述、`[guard].ask_timeout_seconds` 既定300秒）を作って待つ。承認・1日許可は実行して許可キーを書く、却下・期限切れ・取消・サーバー不通は実行しない（終了 code 1）。enforce の pane には `GTX_GUARD_ENFORCING=1`。
5. **ai_guard との突合**（残項目29）: `gtx guard compare`。`decisions.jsonl` と ai_guard のログを時刻窓で突き合わせ、AGREE/DIVERGE/GTX-ONLY/AI-GUARD-ONLY/OUT-OF-WINDOW、DIVERGE があれば終了 code 1。
6. **dotfiles の変更案**: `load-ai-guard.sh` が `GTX_GUARD_ENFORCING` と `GTX_GUARD_MARKER` でも自分を読み込まないようにする差分を RESULT.md に書く（適用しない）。

## 2. 守ること

- **店長の本物の環境に触らない（最重要）**: 本物の `~/.ai_extreme_confirm.log`、ai_guard の承認ファイル・許可キー、`~/.zshenv` などの起動ファイル、dotfiles、店長の state・設定・稼働中の herdr と gtx に書き込まない。確かめは一時の HOME・state・ログの場所で行う（必要なら場所を環境変数などで差し替えられるようにする）。本物の危険なコマンド（rm、git push --force、wrangler deploy など）を実行して確かめない。判定の確かめは check で行い、exec の確かめは無害な代わりのコマンドで行う。
- これまでの段階と同じ: 1ファイル 800 行まで、推測で埋めない、未対応は未対応と書く、依存ライブラリを足さない、計画1章に無い機能を作らない。
- 印が無い pane への影響（PATH の先頭の空のフォルダ、ZDOTDIR・BASH_ENV の連鎖）で、普通の pane の起動・コマンドの実行が遅くならないこと（SPEC: 呼び出しあたり 1ms 以下）。性能（TestRelative、基準 main 473a2b4）は親が最後に測る。
- go build はメインの Mac で可。テスト・race・ptyrun は mini。

## 3. 進め方と確認

- 区切り: 判定部品と check → 差し込みと印 → record と記録 → enforce と判断待ち → compare と dotfiles の差分案。区切りごとに mini で gofmt・go build・go vet・go test を通して積み上げコミット。最後に race。
- `.artifacts/guard/RESULT.md` に、SPEC の受け入れ条件ごとの表（条件・確かめたテスト名か出力・状態）と、ai_guard と同じ入力で同じ判定になることを確かめた表を書く。確かめていない条件は未達と書く。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
