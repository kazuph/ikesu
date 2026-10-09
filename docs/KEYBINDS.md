# paneのコピーと分割のキー

prefixは `Ctrl+b`。隔離したherdr配布ビルドとの比較に合わせ、copy modeの既定は `prefix+[`、vertical splitは `prefix+v`、horizontal splitは `prefix+minus`。
画面の `[menu]` → `keybinds` でも設定済みのbindingを確認できる。

`[keys] copy_mode` / `split_vertical` / `split_horizontal` の明示設定は既定を置き換え、空配列で解除する。`prefix+]`、`prefix+percent`、`prefix+double_quote` の明示設定は引き続き利用できる。

名前入力は末尾に追加し、Backspaceで末尾を消し、Ctrl-Cで全体を空にする。Enterで保存、Escで取り消す。
