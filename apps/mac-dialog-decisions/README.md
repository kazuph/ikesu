# mac-dialog-decisions

Ikesu の判断待ち（`gtx decision ask`、guard の `ask`、画面プロンプト検出など）を、macOS の最前面に開くネイティブダイアログで回答できるようにするプラグインです。Slack・Sabera グラス・アプリ・ターミナルと同じ `gtx decision watch` の流れに乗る、もう1つの回答窓口です（後回しではなく、created と同時に並行して出ます）。

## 動作

- 常駐デーモンが `gtx decision watch --all-machines` を購読します。
- `created` イベントで、ネイティブの AppKit パネル（`dialog.swift`）を今いるデスクトップの最前面に1回だけ出します。**作業中のアプリからキーボードを奪いません**（アクティブにしない・何度も前に出し直さない）。パネルを自分でクリックしたときだけ入力先になります（日本語入力もそこから使えます）。
- パネルの中身（上から）:
  - 出どころ: 🤖 エージェント（保存マシンならマシン名）、**プロジェクト（スペース） › タブ › ペイン**。ウィンドウのタイトルにもプロジェクト名が出ます。
  - 質問の見出し（「質問 1/3」など）と題名を大きく、本文（前提）を全文。
  - コマンド・場所・ブランチ・理由・ペインID。
  - 選択肢ごとのカード: 番号・題名・説明文・プレビュー（読めなかったものは「プレビューあり（エージェントの画面で確認）」）。長い本文やプレビューは切り捨てず、パネル内をスクロールできます。
  - 下に固定: 自由入力欄、期限、ほかに待っている件数、「あとで」と「回答する ⌘↩」。
  - 自由入力欄では ⌘V（貼り付け）・⌘C・⌘X・⌘A・⌘Z / ⇧⌘Z が使えます（パネルにメニューバーが無いので、キーを文字欄へ直接渡しています）。
- 回答は `gtx decision answer <id> <option_id>... --machine <machine.id> --responder mac-dialog [--text ...]` を実行します。
- 他所で回答・取消・期限切れになった判断待ち（`resolved`）や、マシン切断（`machine_down`）では、開いているパネルを即座に閉じます。デーモンが止まった・パネル用ファイルが消えたときもパネルは自分で閉じるので、答えた後に窓が残りません。
- 判断待ちが複数あるときは1枚だけ出し、残りは順番に出します（「ほかに N 件待っています」）。

## 答え方

- **1つ選ぶ質問**: 選択肢のカード（丸印）をクリックして選び（⌘1〜⌘9 でも可）、右下の「回答する」か ⌘↩ で送ります。**選んだだけでは送りません**（作業中に突然出たパネルを誤ってクリックしても答えにならないように）。選んだカードをもう一度押すと選択を外せます。自由入力に書いてあれば一緒に送ります（エージェントの画面には「『選択肢』を選択。書いた文」として自由入力欄に届きます。プレビュー付きの質問ではその選択肢のメモになります）。選択を外せば文だけ送ることもできます。
- **複数選ぶ質問**: カード（四角印）をクリックしてチェックを付け外しし（⌘1〜⌘9 でも可）、「回答する」か ⌘↩。自由入力も一緒に送ります。
- **自由入力だけの質問**: 書いて ⌘↩ か「回答する」。
- **あとで**（または Esc・閉じるボタン）: 答えずにいったん閉じ、**10分後**（設定で変更可）にまだ答えていなければもう一度出します。その間も Slack・アプリ・グラス・ターミナルでいつでも答えられます。

## インストール

Ikesu リポジトリ内から開発リンク:

```bash
gtx plugin link <repo>/apps/mac-dialog-decisions
```

リンク後、最初の `decision.created` イベントで `ensure-daemon` が走り、デーモンを `gtx run --label mac-dialog-decisions` ジョブとして起動します。手動で起動する場合はプラグインアクション `start`（`gtx plugin action invoke start --plugin kazuph.mac-dialog-decisions`）を使います。

前提: macOS のみ（`platforms = ["macos"]`）。Node.js 18 以上と Xcode（`swiftc`）。パネルは初回起動時に `swiftc` でプラグインの状態ディレクトリへビルドし、`dialog.swift` が変わったときだけ作り直します（`DEVELOPER_DIR` が CommandLineTools を指していると SDK と合わずに失敗するため、ビルド時は外します）。

## 設定

プラグイン設定は `<config dir>/config.json`（`gtx plugin config-dir kazuph.mac-dialog-decisions` で確認可）:

| キー | 既定 | 意味 |
| --- | --- | --- |
| `enabled` | `true` | `false` でプラグイン無効（デーモンを起動しません） |
| `gtx_bin` | `gtx` / `GTX_BIN_PATH` | gtx 実行ファイルのパス |
| `helper_command` | なし | パネル表示プロセスの差し替え（テスト用。argv の配列。引数に spec JSON のパスが渡り、結果を stdout に JSON 行で返します） |
| `later_minutes` | `10` | 「あとで」で閉じた判断待ちをもう一度出すまでの分数 |
| `present_delay_ms` | `250` | キューの先頭を画面に出すまでの待ち。同時に届いた複数の判断待ちを束ねて、最初のダイアログの「ほかN件」を正確にします |

環境変数でも上書きできます: `MAC_DIALOG_ENABLED`（`0`/`false` で無効）、`MAC_DIALOG_PRESENT_DELAY_MS`、`MAC_DIALOG_LATER_MS`、`MAC_DIALOG_HELPER_CMD`（JSON の argv）、`MAC_DIALOG_STATE_DIR`、`MAC_DIALOG_CONFIG`（設定ファイルのパス）。

## オフにする方法

設定ファイルに `{"enabled": false}` を書くか、`MAC_DIALOG_ENABLED=0` を環境に入れます。走っているデーモンは `gtx run list` で `mac-dialog-decisions` ジョブを見つけて `gtx run cancel` で止められます。

## 起動方式

`decision.created` のプラグインイベントフックが `ensure-daemon` を呼び、デーモンを `gtx run --label mac-dialog-decisions` のジョブとして起動します（slack-decisions と同じ仕組み）。pid ファイルとロックで二重起動を防ぐので、イベントごとに呼ばれてもデーモンは1つだけ走ります。ログは `gtx log <job>` で読めます。

## テスト

```bash
node --test apps/mac-dialog-decisions/test/render.test.mjs
```

表示内容の純粋関数を検証する。イベント・回答・常駐と実パネルは、一時 HOME、名前付き session、`[ui.sound] enabled=false`、画面クライアントなしの実サーバーで確認する。別マシンの保存先も一時環境に限定し、実 SSH を使う。パネルはクリックせず、CLI 回答で閉じることを確認する。

## 出典

作者本人の許可に基づき kazuph/herdr の `apps/mac-dialog-decisions` から導入。コマンド・環境変数・設定名・マニフェストを gtx に合わせて変更した。herdr 本体の実装は使用していない。
