# プラグインと自動化

`gtx plugin help` はプラグインの登録・実行・ログ・ペイン操作を説明します。
ローカルの `gtx-plugin.toml` は `gtx plugin link <path> [--disabled]` で
サーバー停止中にも登録でき、登録と設定・状態フォルダは session 間で共有します。
mailbox と job は各 session に属します。

`gtx wait help` は追加の待機コマンドを説明します。
通常のコマンドには `gtx wait output <pane_id> --match <text> --source visible`
または `--source recent`、エージェントには
`gtx wait agent-status <pane_id> --status blocked` を使います。
`--regex`、`--lines N`、`--timeout MS` も指定できます。
既存の `gtx agent wait`、`gtx send`、`gtx msg`、`gtx run` の導線は保持します。
長い job は `gtx run` で起動し、完了通知が来たあとに `gtx log <job_id>` を読みます。

ソケットの `events.subscribe` は状態変化の購読、`events.wait` は条件待機です。
マニフェストの `[[events]]` はイベントを別プロセスのコマンドへ渡します。
`agent.view.set` / `agent.view.clear` は agents 一覧の表示を差し替え、
エージェントの状態・レイアウト・保存内容は変更しません。

現段階の端末バッファには折り返し情報と元の ANSI バイト列がありません。
`wait output` の既定 `recent_unwrapped`、`--source recent-unwrapped`、`--raw` は
未対応エラーを返します。物理行を論理行や元のバイト列として代用しません。
外部エージェント設定への hook/plugin の自動インストールは行いません。
