# 見張りがすべての agent pane で効くことの段階の計画

作成: 2026-10-13（親 Claude）。

## 依頼と経緯

店長（2026-10-13）: 「ai_guard に頼らない仕組みとして君に依頼してます。この状況は期待値以下です。」

経緯: 店長の herdr（0.2.33、見張りは enforce）の上で動く Claude Code の pane（p2301）で、危険なコマンド（`rm`）の承認が herdr の判断待ちダイアログではなく、dotfiles の ai_guard のボタン（Slack・Mac のダイアログ）で行われた。もう1件は300秒応答がなく ai_guard が自動却下した。観察した事実:

- herdr の印のフォルダ `~/.local/state/herdr/guard/agent-panes/` にあるのは、herdr 0.2.33 になってから `herdr agent start` で起動した Codex の pane（p_2443）の印だけ。動いている Claude Code の pane（p2301・p2061・p2293）には印が無い。
- p2301 の環境の PATH に見張りの shim のフォルダが無い（pane が見張りの入る前に起動したため）。
- 印も shim も無いので、dotfiles の `load-ai-guard.sh` が ai_guard を読み込み、ai_guard が承認を出した。

gtx の見張り（main ffbf2be で取り込み、G12）も同じ穴を持つかは確かめていない。G12 の確かめは `gtx guard check` と無害な代わりのコマンドが中心で、本物の Claude Code の Bash 道具から危険なコマンドを出したときに gtx のダイアログになることは確かめていない。

## 0. 作り方の決まり（最優先）

- herdr のソースコードを読まない・移植しない（AGENTS.md）。仕様の正は `~/src/github.com/kazuph/herdr/SPEC.md` の G12 の4節。ai_guard（`~/dotfiles/.ai_guard.zsh`、`~/dotfiles/.config/shell/load-ai-guard.sh`）は読んでよいが、変えない。
- 店長の本物の環境に触らない: 本物の `~/.ai_extreme_confirm.log`・ai_guard の承認ファイル・起動ファイル・dotfiles・店長の gtx と herdr の state・設定・稼働中のサーバーに書き込まない。**確かめの途中で店長の Slack や Mac に ai_guard の承認が飛ぶ状態を作らない**（一時の HOME で、ai_guard の通知先が店長に届かないことを先に確かめる）。

## 1. やること

1. **本物の agent で、gtx の判断待ちになることを確かめる**: mini の gtx の pane の中で本物の Claude Code（mini の `~/.local/bin/claude` 2.1.296）を動かし、その Bash 道具から、見張りが ask にするコマンド（一時フォルダの中だけを消す `rm -rf` など、害の無いもの）を実行させる。gtx の判断待ち（command_guard）が作られ、`gtx decision answer` か画面のダイアログで答えると実行・却下されること、ai_guard が一度も呼ばれないこと（一時 HOME の ai_guard のログが増えないこと）を確かめる。Claude Code はコマンドごとに起動時の環境を写した shell で実行するので、その経路でも shim が効くことを必ず確かめる。Codex・Devin など mini で使える他の agent も同じ形で確かめる（mini に無い agent は未検証と書く）。
2. **印が付かない・shim が効かない場合を全部なくす**: 次のどの順番でも、agent が動いている pane が見張りの対象になること。
   - pane を開いてから、後で手で agent を起動した（`gtx agent start` を使わない）。
   - gtx を起動し直して、保存した session から pane と agent を戻した。
   - live handoff の後。
   - `[guard]` を設定で有効にして再読込した後（`gtx server reload-config`）。
   - agent が終わって、同じ pane でもう一度 agent を起動した。
3. **どうしても対象にできない pane を黙って残さない**: 見張りが入る前から動いている process（PATH に shim が無いなど、後から差し込めないもの）は、`gtx guard status` に「対象にできていない agent pane」として pane ID と理由を出し、画面にも分かる形で知らせる。黙って ai_guard に任せた状態にしない。対象にする方法（その pane で agent を起動し直す、など）も示す。

## 2. 守ること

- 変えてはいけないもの（変えたいときは実装前に親へ報告して止まる）: go.mod、見張りの印 `GTX_GUARD_ENFORCING` と `GTX_GUARD_MARKER`、判定規則と文言（ai_guard と同じ）、`guard` の CLI と出力の形、判断待ちの API、人の pane（agent が動いていない pane）では判定も記録もしないこと、record モードではコマンドを止めないこと。1ファイル 800 行まで。計画1章に無い機能を作らない。
- 印が無い pane の普段のコマンドを遅くしない（SPEC: 1回あたり 1ms 以下）。性能は親が最後に測る。
- 本物の危険なコマンドを実行しない。消してよい一時フォルダの中だけを対象にする。

## 3. 進め方と確認

- 区切り: 今の gtx で本物の Claude Code を動かして穴を再現する（どの順番で印や shim が欠けるかの表）→ 直す → 本物の agent でもう一度確かめる。区切りごとに mini で gofmt・go build・go vet・go test を通して積み上げコミット。最後に race。
- `.artifacts/guard-coverage/RESULT.md` に、1章の各場合ごとの表（場合・確かめ方と保存した出力・印・shim・判断待ちの ID・ai_guard のログの行数の前後・状態）を書く。確かめていないものは未達と書く。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
