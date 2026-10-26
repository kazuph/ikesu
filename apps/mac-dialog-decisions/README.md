# mac-dialog-decisions

Ikesu の判断待ち（`ikesu decision ask`、guard の `ask`、画面プロンプト検出など）を、macOS の最前面に開くネイティブダイアログで回答できるようにするプラグインです。Slack・Sabera グラス・アプリ・ターミナルと同じ `ikesu decision watch` の流れに乗る、もう1つの回答窓口です（後回しではなく、created と同時に並行して出ます）。

CLI は `ikesu` を先に探索し、無ければ `ikesu` を使用します。以下の例は `ikesu` に置き換えても動きます。

## 動作

- 常駐デーモンが `ikesu decision watch --all-machines` を購読します。
- `created` イベントで、ネイティブの AppKit パネル（`dialog.swift`）を今いるデスクトップの最前面に1回だけ出します。**作業中のアプリからキーボードを奪いません**（アクティブにしない・何度も前に出し直さない）。パネルを自分でクリックしたときだけ入力先になります（日本語入力もそこから使えます）。
- パネルの中身（上から）:
  - 出どころ: 🤖 エージェント（保存マシンならマシン名）、**プロジェクト（スペース） › タブ › ペイン**。ウィンドウのタイトルにもプロジェクト名が出ます。
  - 質問の見出し（「質問 1/3」など）と題名を大きく、本文（前提）を全文。
  - コマンド・場所・ブランチ・理由・ペインID。
  - 選択肢ごとのカード: 番号・題名・説明文・プレビュー（読めなかったものは「プレビューあり（エージェントの画面で確認）」）。長い本文やプレビューは切り捨てず、パネル内をスクロールできます。
  - 下に固定: 自由入力欄、期限、ほかに待っている件数、「あとで」と「回答する ⌘↩」。
  - 自由入力欄では ⌘V（貼り付け）・⌘C・⌘X・⌘A・⌘Z / ⇧⌘Z が使えます（パネルにメニューバーが無いので、キーを文字欄へ直接渡しています）。
- 回答は `ikesu decision answer <id> <option_id>... --machine <machine.id> --responder mac-dialog [--text ...]` を実行します。
- 他所で回答・取消・期限切れになった判断待ち（`resolved`）や、マシン切断（`machine_down`）では、開いているパネルを即座に閉じます。デーモンが止まった・パネル用ファイルが消えたときもパネルは自分で閉じるので、答えた後に窓が残りません。
- 判断待ちごとに別の窓を開き、同じAppKitプロセスで保持します。新着は通常の窓の層へ1回だけ出し、Dockアイコンは1つ、バッジは開いている窓の数です。Dockの右クリック一覧から出どころと質問で窓を選べます。窓を閉じても、ほかの窓の選択と下書きは保持します。プレビューは折り返さず横にスクロールでき、文字幅は unicode-width 0.2.2 のスカラー幅にそろえます。

## 答え方

- **1つ選ぶ質問**: 選択肢のカード（丸印）をクリックして選び（⌘1〜⌘9 でも可）、右下の「回答する」か ⌘↩ で送ります。**選んだだけでは送りません**（作業中に突然出たパネルを誤ってクリックしても答えにならないように）。選んだカードをもう一度押すと選択を外せます。自由入力に書いてあれば一緒に送ります（エージェントの画面には「『選択肢』を選択。書いた文」として自由入力欄に届きます。プレビュー付きの質問ではその選択肢のメモになります）。選択を外せば文だけ送ることもできます。
- **複数選ぶ質問**: カード（四角印）をクリックしてチェックを付け外しし（⌘1〜⌘9 でも可）、「回答する」か ⌘↩。自由入力も一緒に送ります。
- **自由入力だけの質問**: 書いて ⌘↩ か「回答する」。
- **あとで**（または Esc・閉じるボタン）: 答えずにいったん閉じ、**10分後**（設定で変更可）にまだ答えていなければもう一度出します。その間も Slack・アプリ・グラス・ターミナルでいつでも答えられます。

一般の送信失敗はパネル内に理由を表示し、同じ質問へ手動で再回答できます。Tailscaleの認証URLを含む失敗は回答内容を保持し、既存の15秒間隔・最大120回の範囲で再試行します。マシン切断中は送らず、再接続後に同じ回答を再開します。成功または既決の応答で、resolved通知が届かなくてもその判断待ちを片付けます。空の ⌘↩ は不足を画面内に表示し、音を鳴らしません。

## インストール

Ikesu リポジトリ内から開発リンク:

```bash
ikesu plugin link <repo>/apps/mac-dialog-decisions
```

リンク後、最初の `decision.created` イベントで `ensure-daemon` が走り、デーモンを `ikesu run --label mac-dialog-decisions` ジョブとして起動します。手動で起動する場合はプラグインアクション `start`（`ikesu plugin action invoke start --plugin kazuph.mac-dialog-decisions`）を使います。

配布版は `ikesu mac-dialog install [--version MAJOR.MINOR.PATCH]` で導入し、`ikesu mac-dialog status` で登録版・稼働版・切替待ち・最新版を確認します。署名済み配布ではDeveloper IDを検証したappだけを使い、検証に失敗してもswiftcへ戻りません。新しい版への登録後も未回答・あとで・送信中・再試行中が残る間は旧版を止めません。旧常駐の所有を確認できない時は強制停止せず、稼働版不明・切替待ちを表示します。

開発リンクの前提: macOS のみ（`platforms = ["macos"]`）。Node.js 18 以上と Xcode（`swiftc`）。パネルは初回起動時に `swiftc` でプラグインの状態ディレクトリへビルドし、`dialog.swift` が変わったときだけ作り直します（`DEVELOPER_DIR` が CommandLineTools を指していると SDK と合わずに失敗するため、ビルド時は外します）。

## 設定

プラグイン設定は `<config dir>/config.json`（`ikesu plugin config-dir kazuph.mac-dialog-decisions` で確認可）:

| キー | 既定 | 意味 |
| --- | --- | --- |
| `enabled` | `true` | `false` でプラグイン無効（デーモンを起動しません） |
| `ikesu_bin` | `ikesu` / `IKESU_BIN_PATH` | CLIのパス。旧 `ikesu_bin` / `IKESU_BIN_PATH` も使用可 |
| `helper_command` | なし | パネル表示プロセスの差し替え（テスト用。argv の配列。引数に spec JSON のパスが渡り、結果を stdout に JSON 行で返します） |
| `later_minutes` | `10` | 「あとで」で閉じた判断待ちをもう一度出すまでの分数 |
| `present_delay_ms` | `250` | キューの先頭を画面に出すまでの待ち。同時に届いた複数の判断待ちを束ねて、最初のダイアログの「ほかN件」を正確にします |

環境変数でも上書きできます: `MAC_DIALOG_ENABLED`（`0`/`false` で無効）、`MAC_DIALOG_PRESENT_DELAY_MS`、`MAC_DIALOG_LATER_MS`、`MAC_DIALOG_HELPER_CMD`（JSON の argv）、`MAC_DIALOG_STATE_DIR`、`MAC_DIALOG_CONFIG`（設定ファイルのパス）。

`IKESU_PLUGIN_CONFIG_DIR`、`IKESU_PLUGIN_STATE_DIR`、`IKESU_BIN_PATH`、`IKESU_SOCKET_PATH`、`IKESU_PANE_ID` を読みます。対応する `IKESU_*` も使用でき、両方あるときは `IKESU_*` が優先です。

## オフにする方法

設定ファイルに `{"enabled": false}` を書くか、`MAC_DIALOG_ENABLED=0` を環境に入れます。走っているデーモンは `ikesu run list` で `mac-dialog-decisions` ジョブを見つけて `ikesu run cancel` で止められます。

## 起動方式

`decision.created` のプラグインイベントフックが `ensure-daemon` を呼び、デーモンを `ikesu run --label mac-dialog-decisions` のジョブとして起動します（slack-decisions と同じ仕組み）。pid ファイルとロックで二重起動を防ぐので、イベントごとに呼ばれてもデーモンは1つだけ走ります。ログは `ikesu log <job>` で読めます。

## テスト

```bash
node --test apps/mac-dialog-decisions/test/render.test.mjs
```

表示内容の純粋関数を検証する。イベント・回答・常駐と実パネルは、一時 HOME、名前付き session、`[ui.sound] enabled=false`、画面クライアントなしの実サーバーで確認する。別マシンの保存先も一時環境に限定し、実 SSH を使う。パネルはクリックせず、CLI 回答で閉じることを確認する。

## 出典

作者本人の許可に基づき kazuph/herdr の `apps/mac-dialog-decisions` から導入。コマンド・環境変数・設定名・マニフェストを ikesu に合わせて変更した。herdr 本体の実装は使用していない。

command_guard のパネルでは、選択肢を本文のスクロール領域の外に置き、選択と送信は従来どおり分離する。本文に含まれるコマンド・申請文・場所は Origin 欄に重ねて表示しない。判定理由と対象コマンドは見出し直下に強調表示する。

ネイティブ配置の回帰確認は、長い本文の試験用 spec JSON を渡して実パネルを表示し、全選択肢が画面とクリップ領域の内側にあることを数値で検査する。選択だけでは送信されず、再クリックで解除できることも確認する。自分の試験パネルだけを撮影し、ほかのアプリは操作しない。

```sh
env -u DEVELOPER_DIR swiftc apps/mac-dialog-decisions/{dialog,preview,unicode-width}.swift apps/mac-dialog-decisions/test/layout/main.swift -o /tmp/ikesu-panel-layout
/tmp/ikesu-panel-layout /absolute/path/to/test-spec.json /absolute/path/to/panel.png
```
