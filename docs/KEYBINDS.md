# paneのコピーと分割のキー

prefixは `Ctrl+b`。copy modeのdefault bindingは `prefix+[` / `prefix+]` の2つで、同じcopy modeへ入る。
画面の `[menu]` → `Keybinds` でも、この2つのbindingを確認できる。
`[keys] copy_mode` を明示設定するとdefaultを置き換える。空配列はbindingを解除する。

vertical splitは `prefix+%` / `prefix+v`、horizontal splitは `prefix+"` / `prefix+minus`。
`[keys] split_vertical` / `split_horizontal` の明示設定もdefaultを置き換える。
