# 設定再読込の観察（2026-10-13）

仕様・利用者向け文書・API スキーマと、mini の配布バイナリ herdr 0.2.30 の独立した名前付き session を参照した。実装・テストのソースは参照していない。

- API は `server.reload_config`、引数は空オブジェクト。
- CLI の正常応答は `{"id":"cli:server:reload-config","result":{"diagnostics":[],"status":"applied","type":"config_reload"}}`、終了コード0。
- TOML 構文エラーも終了コード0。結果は `status: failed`、diagnostics に parse error と `keeping current config`。pane の terminal_id は正常・異常再読込の前後で一致した。
- `show_tab_bar=false` と `pane_borders=false` は接続中の画面へ反映した。`sidebar_start_collapsed=true` は現在の開閉状態を上書きしない。
- `theme.name` と `sidebar_width` を変更しただけでは、この観察では配色と sidebar 幅が変わらなかった。反映済みと推測せず、追加観察で分類する。
- ikesu の既存メニュー再読込は接続元の設定読み取りを経由する。保存マシンの表示設定と endpoint generation の契約を保ち、再読込操作の共通経路を作る。

生の画面・セル・CLI出力は未追跡の `.artifacts/reload-config/herdr-observation/` に保存する。

追加観察では `prefix+shift+r` によりテーマの色（panel_bg=#123456）、幅38、ctrl+a prefix が反映した。CLI と画面の反映範囲には実物でも差がある。

親の決定（2026-10-13）: 店長の live reload の目的と全画面・共通範囲を優先し、ikesu の CLI/API ではメニューと同じ範囲を反映する。herdr CLI の一部しか反映しない点は合わせず、差と理由を RESULT.md に記す。接続元所有の設定は既存 config_edit/config_result 経由で再読込し、保存マシン契約を維持する。

値の一部が誤っている場合は `status: partial`、終了コード0。ui節の型不一致では旧ui設定を維持し、不正キー割当は無効化する（`keeping current ui settings` / `disabling binding`）。
