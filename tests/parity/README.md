# 実物の操作比較

Mac mini の隔離領域で実行する。herdr の実装・テストは読まず、配布バイナリと gtx に同じ `*.steps` を流す。依存ライブラリを追加しない。

```sh
cd ~/bench-terrace.noindex/src/parity
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
go build -o ../../work/gtx-parity ./cmd/gtx
(cd tools/bench/ptyrun && go build -o "$HOME/bench-terrace.noindex/work/ptyrun-parity" .)
python3 tests/parity/observe.py \
  --herdr ../../work/herdr-target/release/herdr \
  --gtx ../../work/gtx-parity --ptyrun ../../work/ptyrun-parity \
  --steps tests/parity/menu.steps --out ../../work/parity-menu
python3 tests/parity/compare.py ../../work/parity-menu
```

`--steps` を他の手順に替えて繰り返す。herdr バイナリが無ければ `NOT_RUN` として飛ばす。出力先には両方の実行手順・画面テキスト・実行ログが残る。compare.py は画面の全文差と観測文字列の個数を出し、異なる項目の意味を自動で同一扱いにしない。ワークスペース名・ペインID等の動的差とメニュー項目の差は、結果表で区別する。

HOME、XDG、状態ディレクトリ、ZDOTDIR、作業ディレクトリは実行ごとに一時領域へ隔離する。初回案内を閉じるEnter2回だけherdrの前処理とし、操作本体は同一。`--no-session` で起動し、ptyrunが起動したアプリを終了させる。実行者の設定・クリップボード・通常のherdrソケットを利用しない。

`resize COLS ROWS` はPTYと画面エミュレーターを同時にリサイズする。画面キャプチャの寸法も変更後の値になる。`mouse` は端末が交渉したモードで送り、固定セルの `click`/`rclick` は `-pixel-mouse=false` で使う。

性能比較は `perf.sh` をminiで実行する。事前に `git archive 9347b07` のtarを `work/parity-baseline-9347b07.tar` へ置く。同じGo、同じ `go build` 引数で基準と対象をビルドし、SPEC P1の `TestRelative` を全項目実行する。

`--server` は同じ一時領域に専用サーバーを起動し、実APIの `workspace.create` で比較用workspaceを作る。所有領域の `herdr.sock` / `gtx.sock` だけに接続し、終了時に `server.stop`、PIDの終了と応答を `cleanup.json` へ保存する。通常の稼働中サーバーには接続しない。`socket_action.py` はその領域から継承したソケットとペインIDだけを使う。

`clicktext "表示ラベル"` は保存画面内で一意に見つかった行をクリックする。見つからない、または複数行なら失敗し、別項目で代用しない。`prefix_cases.py` / `probe-prefix.sh` はprefix後のキーを個別起動で調べる。実行していない手順は比較済みとしない。

`copy-detail.steps` は移動・検索・選択解除・終了だけを調べる。選択範囲のyankとマウス選択のreleaseはクリップボードを書き換えるため含めない。コピーの実行自体は未達として別途記録する。

`measure.py 出力先` は画面の行数・セル幅・枠の座標と寸法・ペインタイトル・下部行・helpの行・確認用入力文字列を数値と文字列で記録する。各フィールドの一致だけを判定し、画面全体や操作全体の合格へ拡張しない。動的なペイン名等は全文差とあわせて結果表で確認する。

`cells 保存先.json` は端末エミュレーターのセル内容・幅・色・属性を保存し、文字だけで確認できない選択の位置を測定する。`terminal.raw` は端末出力を保存し、OSC 52などを通常の端末へ転送しない。no-sessionでの起動cwdは両方とも隔離HOMEへ揃える（herdrが初期shellをHOMEで開くため）。永続サーバーの比較workspaceは明示cwdで作る。

`cell_styles.py 出力先 screen label` は一意な共通ラベルの先頭セルの色・属性を比較し、不一致または未観測でexit 1を返す。座標も出力するが、項目数が違うメニューの座標は一致扱いにしない。

click/rclick/mouseの座標は1始まり、screen/cells/measure.pyの保存座標は0始まり。上端タブバーはクリック手順ではy1であり、y0を指定しない。

`navigator_compare.py 出力先` は navigator の枠を実物画面から検出し、枠全体の文字・セル幅・色・属性を比較する。Settings は `--title ' settings' --result settings-comparison.json` を付ける。`PASS` は観測した枠の全セル一致、`DIFFERENT_CELLS` は差あり、`NOT_OBSERVED` はその枠を観測できなかった画面を示す。枠が閉じた画面や設定保存の正しさを、この判定で合格扱いにしない。
