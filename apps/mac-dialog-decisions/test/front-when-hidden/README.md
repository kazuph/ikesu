# 判断待ちパネルの実物確認（親が店長の許可を得て実施）

> 依頼「対象スペースを表示中なら前に出さず、離れたら1回だけ前に出し直す」。
> 実物 a/b/c は未実施。Sol は `self-check`（窓を作らない起動・停止・観測）だけ実行済み。

この試験は指定worktreeのビルドを使う。default session・インストール済みパネル・plugin登録・署名済みappを変更しない。試験用Swift mainは製品と同じDialogHost/DialogControllerを載せ、観測だけを追加する。`record` は撮影を行うため、親が店長の許可を得てから実行する。Computer Use等の権限ダイアログを自動で押さない。

## 準備

worktreeで次を実行する（長いコマンドは `ikesu run --label ... --` を使う）。

```sh
go build -o .artifacts/cross-machine-nav/ikesu-task3 ./cmd/ikesu
swiftc -O apps/mac-dialog-decisions/dialog.swift apps/mac-dialog-decisions/host.swift apps/mac-dialog-decisions/preview.swift apps/mac-dialog-decisions/unicode-width.swift apps/mac-dialog-decisions/test/front-when-hidden/main.swift -o .artifacts/cross-machine-nav/dialog-front-test
node apps/mac-dialog-decisions/test/front-when-hidden/run.mjs self-check
```

`self-check` は別serverと窓のない別hostを起動し、観測を読み、両方を正常停止する。窓数0・最前面PID・入力先・app_activeをJSONで出力し、`directory`に生記録を残す。スクリーンショットは撮らない。

以下では `DRIVER` をこのworktreeの `apps/mac-dialog-decisions/test/front-when-hidden/run.mjs` の絶対パスにする。`ROOT=/tmp/ikesu-task3-review` は未使用の短い絶対パスを選ぶ。macOSのUnix socket上限のためworktreeの深いパスは使わない。

```sh
DRIVER="$PWD/apps/mac-dialog-decisions/test/front-when-hidden/run.mjs"
ikesu run --label task3-manual -- node "$DRIVER" start /tmp/ikesu-task3-review
```

親が店長の許可を得て、**新しい一時ターミナルの窓**で次を実行する。常用のikesuの中でattachせず、一時窓を使う。

```sh
node /絶対パス/apps/mac-dialog-decisions/test/front-when-hidden/run.mjs attach /tmp/ikesu-task3-review
```

以降の命令は親の実行環境から送る。一時ターミナルの前面・キーボード入力先を変えずに送る。attachした窓では、店長に許可を得てウィンドウを選択し、端末フォーカス通知を発生させる。キー入力だけではfocused=trueにはならない。

```sh
node "$DRIVER" prepare /tmp/ikesu-task3-review
```

出力の `target`（対象スペース）、`away`（別スペース）、対象スペースの `pane_id` を控える。以降の例の `s1` / `s2` / `p1` は、必ずこの実出力のIDへ置き換える。

## 操作と観測

1. **対象を表示中→背面（a）**：一時ターミナルでtargetを表示し、その窓にキーボードフォーカスを置く。親から `create ROOT p1 a` → `record ROOT a` を実行する。
2. **別スペース表示中→前面（b）**：`clear ROOT`、`focus ROOT s2` でawayへ移動し、`create ROOT p1 b` → `record ROOT b` を実行する。判断待ちの発生元はtargetのペインp1のまま。
3. **背面の後に離れる→1回（c）**：`clear ROOT`、`focus ROOT s1` でtargetへ戻す。`create ROOT p1 c` → `record ROOT c-before`。続けて `focus ROOT s2` → `record ROOT c-after`。次に店長の許可のもとで一時ターミナルの窓を選び直し、`focus ROOT s1` → `focus ROOT s2` → `record ROOT c-again`。同じ判断待ちを回答・閉鎖せずに使う。

上記の各命令は次の形（例）：

```sh
node "$DRIVER" create /tmp/ikesu-task3-review p1 a
node "$DRIVER" record /tmp/ikesu-task3-review a
```

`record` が示す `panels[].screen_index` はCGWindowListCopyWindowInfoの通常レベル窓の前後順（0が最前面）。対象窓が見つからない `-1` は不合格ではなく未検証として原因を確認する。`input_index` は最前面アプリの最初の通常レベル窓。撮影と突き合わせ、別のターミナル窓を見ていないことを確かめる。

|記録|数値で確かめること|
|---|---|
|a / c-before|panel.visible=true、panel.key_window=false、app_active=false、panel.screen_index > input_index、front_count=0|
|b|panel.screen_index < input_index、panel.key_window=false、app_active=false、frontmost_pidは一時ターミナルのPIDのまま|
|c-after|bと同じ前後関係、front_count=1|
|c-again|ターミナルを再選択してからのpanel.screen_index > input_index、front_count=1のまま|
|全記録|input_targetのアプリ・窓・AX roleが期待する入力先、window_count=1、表示一覧のfocused=true・machine_id=local・session=dialog-front-task3とスペース所属が期待どおり|

`input_target.available=false` はAccessibility権限不足のため未検証。勝手に許可リストを変えず、親が店長に判断を求める。撮影も失敗時はエラーとなり、成功した扱いにしない。

背面窓は別途Dock・右クリック一覧・⌘Tab・Mission Controlから実際に選び、キー入力と回答ができることも親が確認する（先に既存のa/b/c記録を終える）。

## 記録・停止

`ROOT/native.jsonl` にisKeyWindow・NSApp.isActive・frontmost PID・窓数・windowNumber・前後順・入力先・再前面化命令数、`ROOT/views.jsonl` に表示接続の状態、`ROOT/decisions.jsonl` に判断待ちID、`ROOT/<label>.png` に撮影を保存する。`ROOT/runtime.json` は自分が起動したserver/host/supervisorのPID。記録は追跡対象外の `.artifacts/cross-machine-nav/` へ親が必要分をコピーする。

```sh
node "$DRIVER" stop /tmp/ikesu-task3-review
```

stopは隔離daemon→host→serverの順に正常停止し、プロセス終了を待つ。default sessionを操作せず、ファイルも削除しない。一時ターミナルのattachはserver終了で戻る。異常時にOSプロセスの停止が要るなら、`pgrep -u "$(id -u)" -f '<このworktreeの絶対パス>'` の結果と `runtime.json` を照合し、自分の実体と確認できた番号だけを `kill <PID>` する。pkill/killallは禁止。
