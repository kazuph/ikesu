# プラグインと自動化

`ikesu plugin help` はプラグインの登録・実行・ログ・ペイン操作を説明します。
ローカルの `ikesu-plugin.toml` は `ikesu plugin link <path> [--disabled]` で
サーバー停止中にも登録でき、登録と設定・状態フォルダは session 間で共有します。
mailbox と job は各 session に属します。

`ikesu wait help` は追加の待機コマンドを説明します。
通常のコマンドには `ikesu wait output <pane_id> --match <text> --source visible`
または `--source recent`、エージェントには
`ikesu wait agent-status <pane_id> --status blocked` を使います。
`--regex`、`--lines N`、`--timeout MS` も指定できます。
既存の `ikesu agent wait`、`ikesu send`、`ikesu msg`、`ikesu run` の導線は保持します。
長い job は `ikesu run` で起動し、完了通知が来たあとに `ikesu log <job_id>` を読みます。

ソケットの `events.subscribe` は状態変化の購読、`events.wait` は条件待機です。
マニフェストの `[[events]]` はイベントを別プロセスのコマンドへ渡します。
`agent.view.set` / `agent.view.clear` は agents 一覧の表示を差し替え、
エージェントの状態・レイアウト・保存内容は変更しません。

`wait output` の既定 `recent_unwrapped` と `--source recent-unwrapped` は、
出力処理で記録した自動折り返しだけを結合し、明示改行を残します。
`--raw` は端末セルが保持する色・装飾・リンクを ANSI として返します。
同じ読み取りは `pane read`・`agent read` の API でも使えます。
外部エージェント設定への hook/plugin の自動インストールは行いません。
