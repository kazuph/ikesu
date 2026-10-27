# 画面の質問と許可確認を判断待ちとして受け取る

`[decisions] screen_prompts = true` が既定値。blocked な Claude Code・Codex・Devin の現在の検出画面を読み、質問を `agent_prompt`、許可確認を `tool_permission` として保存する。スクロールバックと利用者の表示位置は読まない。agent の状態判定ルールを変更する機能ではない。

```toml
[decisions]
screen_prompts = true
```

`ikesu server reload-config` で false に切り替えると、画面監視を止め、画面由来の pending 判断待ちを cancelled にする。監視中または blocked な対応 agent がいる時だけ、画面監視の期限を設定する。

## 選択肢の説明と複数回答

```sh
ikesu decision ask --title '項目を選ぶ' --multi-select \
  --option 'summary:概要:other' --option 'evidence:証拠:other' \
  --option-desc 'summary:短い概要を含める' \
  --option-desc 'evidence:実行したコマンドと出力を含める'
ikesu decision answer <decision_id> summary evidence
```

`--option-desc id:text` は繰り返し指定でき、未知の id は作成前にエラーになる。既存の `--option id:label:role` と `--timeout` のミリ秒指定は変更しない。

- 選択肢の `description` と `preview` は任意の文字列。無い時は JSON に出さない。
- `preview_unavailable: true` は未訪問のプレビューを示す。訪問してプレビューが無かった選択肢には付けない。
- `multi_select` は複数選択、`question_label` は複数質問の名前と進行を表す。
- 複数回答は重複を除く。先頭を従来の `option_id`、2件以上を `option_ids` に保存する。
- 古い JSON の追加項目が無くても読める。SQLite の表と既存の record 保存形式を使う。

`decision watch` の created/resolved 行はこれらを含む判断待ち全体を配信する。`ikesu status --json` の `server.capabilities.screen_prompt_decisions` と ping 応答で、この提供機能の有無を読める。

## 回答の安全確認

質問文・選択肢・説明文・自由入力の有無・複数選択の有無を質問署名に使う。タブのチェック状態、選択中のカーソル、プレビューは署名を変えない。

画面が2回連続で一致しないと取消し、1回だけの読み取り異常では取り消さない。同じ pending は再利用し、他の経路で回答・取消・期限切れになった質問は再提示しない。

回答直前にも画面を確認する。別の質問、消えた質問、無い pane/監視は、キーを送る前に cancelled にして `decision_stale` を返す。番号行が残った壊れた読み取りは、直前の走査が一致していた時だけ受け付ける。

画面にあるショートカットを優先し、無ければ番号を送る。自由入力は自由入力の番号、文字列、Enter の順。複数選択は現在のチェック状態から必要な番号を個別に送り、再読後に Next/Submit へ移って確定する。

複数選択の自由入力では、矢印で自由入力の行へ移ってから文字列とEnterを送る。文字列の確定だけではチェックが付かない画面では、その行でEnterを送り、チェックを確認してからNext/Submitへ進む。入力中にラベルが文字列へ変わっても、元から観察した自由入力の行IDを使う。1問だけの複数選択のタブも `質問 1/1 <名前>` として記録する。

プレビュー走査は各選択肢へ矢印だけで移り、元の位置へ復帰してから判断待ちを作る。質問が変わる、カーソルが進まない、箱を最後まで読めない時は中断し、未訪問の選択肢を明示する。

対応する進行行や自由入力行が無いなど、送信方法を確定できない回答は `screen_prompt_unsupported` で拒否する。送信中に pane/質問が変わって入力を完了できなかった時は `screen_prompt_input_failed`。その時は回答記録の採用と画面への入力完了を区別する。

実 agent の確認範囲と残件は、作業の `RESULT.md` に記録する。Devin の実地起動は行わない。
