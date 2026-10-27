# live handoff の観察と実装契約

対象: mini の配布用 herdr 0.2.30。実装・テストソースは参照しない。
隔離した `/tmp/ikesu-handoff-observe-*` の HOME、XDG_CONFIG_HOME、XDG_STATE_HOME、XDG_CACHE_HOME と名前付き `handoff-observation` session を使い、独立した ptyrun で起動した。各回の最後にその socket の server.stop を呼ぶ。

## 観察した外部契約

- ping の `capabilities.live_handoff` は true。protocol は 19。
- `server.live_handoff` の成功応答は `{"type":"ok"}`。`import_exe` 省略時も成功し、同じ版を使う。名前付き session の API socket パスは変わらない。
- expected_protocol 不一致、expected_version 不一致は `handoff_failed` / `handoff stream closed while reading line`。不存在の import_exe は `handoff_failed` / `failed to spawn handoff import server at <path>: No such file or directory (os error 2)`。いずれも旧サーバーは ping に応答し、pane の構成は残る。
- 応答しない実行ファイルを移行先にすると、30.026秒後に `handoff_failed` / `timed out waiting for handoff import connection` を返し、旧サーバーの ping と pane p1 は残った。短い隔離 HOME `/tmp/gho-u5wk6fk_` と名前付き session `hto` で確認し、そのサーバーと起動した移行先だけを停止した。
- kitty 有効時の XTVERSION は `ESC P >|ghostty (herdr 0.2.30) ESC \\`。画像問い合わせ `a=q,i=31,s=1,v=1,f=24;AAAA` は `ESC _ G i=31;OK ESC \\`。
- mouse を normal tracking / SGR にした実プロセスは handoff 前後とも `ESC [ <0;23;8M` と `ESC [ <0;23;8m` を受信した。
- handoff は display client の接続を切るが client が自動で再接続する。利用者の再接続操作は不要。
- 追加観察では probe の PID 28191 が2回の handoff 後にも `ps` に現れ、同じ probe.jsonl へ入力を記録した。ptyrun の画面プロセス PID 28151 も終了していない。
- kitty 無効時の XTVERSION は `ESC P >|libghostty ESC \\`。実物は無効時にも画像の query / transmit へ OK を返したが、画像対応を名乗らないという SPEC の条件を優先して ikesu の無効時に画像成功は返さない。
- kitty の直接 RGB 転送は、pane 原点 (27,2) に対して pane 内 (6,4) の画像を外側 (33,6) に c=3,r=2 で置いた（いずれも0始まり）。6回の placement の座標を生出力から数値で確認した。
- 追加の Unicode placeholder 観察では `a=T,U=1,q=2,i=33554474,p=7,c=4,r=2` と前景色・結合文字を送ると、外側に元の4×2 RGB画像と、各行の通常配置（4列×1行、source y=0/1）が出た。handoff 前後の probe PID は72550で同じ。ikesu は通常配置への変換ではなく、仕様で認められた仮想配置と正規化した文字を外側へ送る。隔離 session の生出力は unicode-observation.bin、詳細は RESULT.md。
- 終了用 pane のコマンドが戻った後は shell が残り、その shell に `exit` を送ると pane p2 は一覧から消え、p1 は同じ PID 38267 のまま残った。独立したコマンドの終了と pane の shell の終了を分けて確認した。

## ikesu の実装上の不変条件

1. 新実行ファイルの起動と版・protocol の検証が成功するまで旧サーバーと PTY の所有権を維持する。
2. 端末の画面、scrollback、cursor、両画面の mode、mouse の形式を直接引き継ぐ。保存履歴の再生や新しい shell の起動で代用しない。
3. 新サーバーが同じ socket で受付可能になるまで旧サーバーを終えない。失敗時は旧サーバーの処理を再開する。
4. import では startup restore / skip / agent resume を行わない。判断待ちの永続 record と plugin registry は同じ隔離保存先を使う。
5. pane の終了は PTY の EOF でも検知する。旧サーバーの waitpid に依存しない。
6. 画像を描けない設定では ghostty を名乗らない。未対応の画像操作は明示して成功扱いしない。

観察の生出力と検証結果は `.artifacts/handoff/RESULT.md` に対応付ける（Git へ追加しない）。
