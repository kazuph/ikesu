# agent pane の見張りの適用と確認

見張りは agent が動いている pane に印と shim を用意し、終了すると取り除く。
同じ agent のまま印や shim が欠けても再作成する。live handoff では、この適用状態も渡す。

zsh の起動では、利用者の `.zshenv`・`.zprofile`・`.zshrc`・`.zlogin` を元の
ZDOTDIR で読み、各ファイルが PATH を変更した後に shim を先頭へ戻す。
最後には利用者の ZDOTDIR を戻す。bash の対話 shell は起動ファイルの後の
PROMPT_COMMAND で PATH を戻し、その実行を確認するまで enforcing と印を出さない。

設定再読込は、既存 pane の適用状態も再確認する。見張りが無効な間に作った
pane には、後から起動時の環境を差し込めない。その agent pane は未適用として
`ikesu guard status` の stderr と画面の警告に pane・理由・回復方法を出す。
status の stdout JSON は変更しない。回復には guard が有効な状態で新しい pane を開き、
そこで agent を起動し直す。PATH を保証できない shell も印と enforcing を出さず知らせる。

## 確認する経路

次の各経路で、本物の agent の Bash 道具から一時フォルダだけを対象にした削除を
出し、command_guard の判断待ちで承認・却下できることを確かめる。

- pane を開いた後の手動起動
- 保存した pane と実 agent session の復元
- 判断待ち中の live handoff
- 設定有効化と再読込で未適用を通知した後、新しい pane での起動
- agent が終わった後、同じ pane での再起動

利用者の本物の起動ファイルが PATH を変更する条件と、agent が作る起動時環境の
snapshot を使う条件を含める。印・shim・snapshot の PATH・判断待ち ID を保存し、
一時 HOME の ai_guard ログが増えないことを比較する。ai_guard loader が IKESU の印を
認識しない古い環境では ai_guard の関数が優先されるため、ikesu での達成と数えない。

本物の認証は公式の CLAUDE_CONFIG_DIR で参照し、複製しない。Slack・Mac に承認を
送らないことを事前に確かめる。Claude の許可は default と、その回の一時フォルダ内の
申請文付き rm コマンドとの完全一致に絞る。診断は agent の外で行う。

人の pane では判定・記録が増えず、record では記録だけが増えてコマンドを止めない
ことを別に確認する。mini で gofmt・build・vet・test を行い、最後に race を行う。
TestRelative と TestAbsolute は実行しない。確認の出力と未達事項は
`.artifacts/guard-coverage/RESULT.md` に残し、証拠ファイルはコミットしない。
