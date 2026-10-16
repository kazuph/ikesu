# 実物の操作比較

段階1の0.2.55比較は `stage1_observe.py` と `stage1_hosts.py` をminiで使う。
`stage1_cases.py 出力先` がキー形式・popup・Page操作・分割マウス等の手順を生成し、同じstepsを両製品へ流す。
`stage1-host.steps` のtmux比較は `stage1_hosts.py` が専用socketの実tmuxを起動する。
WezTermはホスト識別情報による条件比較であり、WezTerm GUIでの確認を意味しない。
`stage1_host_gate.py 出力先` は起動と終了の全キーボード要求列を検査する。
`stage1_navigation_gate.py 出力先` は履歴の全行ラベル・受信バイトとpopupの枠数・Escの配送を比較する。
`stage1_timing_measure.py 出力先` は送信と実paneへの配送時刻を保存し、単なる待ち時間の足し算を遅延としない。
popup・kittyの終了時には実物がキー解放を合成するため、操作中の配送は保存画面の取得時刻まで、終了時の配送はそれ以降として別々に読む。
`keyboard_lifecycle.py` はpane自身が要求を変更する。kitty全キー要求を出したshellへ通常文字で解除コマンドを打つ手順は使わない。
`sh tests/parity/gate-stage1.sh 証拠ディレクトリ` はminiでgofmt・vet・通常test・raceを全実行し、失敗した検査も保存する。

Mac mini の隔離領域で実行する。herdr の実装・テストは読まず、配布バイナリと gtx に同じ `*.steps` を流す。依存ライブラリを追加しない。

```sh
cd ~/bench-terrace.noindex/src/look
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
go build -o ../../work/gtx-parity ./cmd/gtx
(cd tools/bench/ptyrun && go build -o "$HOME/bench-terrace.noindex/work/ptyrun-parity" .)
python3 tests/parity/observe.py \
  --herdr ../../work/herdr-0.2.24 \
  --gtx ../../work/gtx-parity --ptyrun ../../work/ptyrun-parity \
  --steps tests/parity/menu.steps --out ../../work/parity-menu --clipboard-sandbox
python3 tests/parity/compare.py ../../work/parity-menu
```

0.2.24 の全面流し直しは `rerun_0224.py` を使用する。既存の保存手順と全 `*_cases.py` の生成手順を、既定設定・店長の比較用設定で観測する。socketを使う手順はserver、それ以外はlocal/serverの両方。全起動にclipboard sandboxを適用し、差・未観測をindex.jsonに残す。セルの差を自動で合格へ置き換えない。`--only` は修正後の指定手順の再実行に使う。

```sh
PARITY_CONTROLLER_SESSION_ID=<現在の検証済みsession-id> python3 tests/parity/rerun_0224.py \
  --herdr ../../work/herdr-0.2.24 --gtx ../../work/gtx-parity \
  --ptyrun ../../work/ptyrun-jobs-final \
  --config-file ../../work/look-0224/compare-config.toml \
  --out ../../work/look-0224/all-parity
```

このrunnerは性能を実行しない。look-0224 の親の範囲指示により、性能は親が測る。既存gateのTestRelative部分は本作業で実行しない。observe.pyは各結果へ配布版・binary hash・設定をreference.jsonに記録する。

`sidebar-card-states`・`sidebar-card-menu`・`sidebar_theme_cases/` の単独起動は、比較用設定の密度が slim なら `[slim]` → `[full]` の順で切り替えて開始時の密度へ戻す。実行した手順を `configured-density.steps` と index.json に保存し、同じ手順を両製品へ送る。endpoint に密度ボタンが無いことによる失敗は未達のまま残す。

`sidebar-width` の折畳み往復は両製品にある prefix+b を使う。旧固定座標に endpoint の折畳みボタンは無いため、この比較をマウスでの折畳みの証明にはしない。`jobs-panel`・`jobs-success`・`popup-editor` は実 API 応答を各製品の証拠 JSON へ保存し、応答の固有 ID・項目順を描画の差へ混ぜない。ジョブのリサイズ前には実 clear を送る。ログ popup 内の固有値や実測時刻は既存のログ・時刻検査で判定し、全セルの差を自動で合格へ変えない。

`--steps` を他の手順に替えて繰り返す。herdr バイナリが無ければ `NOT_RUN` として飛ばす。出力先には両方の実行手順・画面テキスト・実行ログが残る。compare.py は画面の全文差と観測文字列の個数を出し、異なる項目の意味を自動で同一扱いにしない。ワークスペース名・ペインID等の動的差とメニュー項目の差は、結果表で区別する。

HOME、XDG、状態ディレクトリ、ZDOTDIR、作業ディレクトリは実行ごとに一時領域へ隔離する。0.2.24の初回案内を閉じるEnter1回だけherdrの前処理とし、操作本体は同一。`--no-session` で起動し、ptyrunが起動したアプリを終了させる。実行者の設定・通常のherdrソケットを利用しない。端末出力の捕捉だけではnative clipboard書込みを隔離しない。yankやマウス選択releaseを含む比較には、下記の親承認済みsandboxを必ず付ける。

コピーの比較は `observe.py --clipboard-sandbox` で行う。UIと隔離serverをともに `sandbox-exec -p '(version 1)(allow default)(deny mach-lookup (global-name "com.apple.pasteboard.1"))'` で起動する。実行前には同じsandboxの子shから実pbcopyがrc=1で拒否されることを確かめる。実pbpasteの出力は内容を表示・保存せずSHA256だけを取り、開始前と各製品の終了後に比較する。変化後のハッシュがその比較で捕捉したコピー内容と一致したら全体を止めて親へ報告する。一致しない外部の変化は記録だけにして続行し、自分で復元しない。sandbox-probe で pbcopy の拒否を確かめられなければ全体を止めて親へ報告する。`clipboard-guard/` に起動プロファイル・拒否結果・ハッシュ、各製品に終了とserver.stop応答を保存する。OSC52はptyrunのrawへ捕捉し、利用者の端末へ転送しない。pbcopy拒否が画面に及ぼす影響も差として記録する。旧 `--clipboard-guard` のPATH捕捉は実herdrのpbcopy呼出を捕捉できず停止した経緯があり、sandbox比較の代わりには使わない。

`resize COLS ROWS` はPTYと画面エミュレーターを同時にリサイズする。画面キャプチャの寸法も変更後の値になる。`mouse` は端末が交渉したモードで送り、固定セルの `click`/`rclick` は `-pixel-mouse=false` で使う。

性能比較は `perf.sh` をminiで実行する。事前に `git archive b0a18b0` のtarを `work/look-baseline-b0a18b0.tar` へ置く。同じGo、同じ `go build` 引数で基準と対象をビルドし、SPEC P1の `TestRelative` を全項目実行する。

`--server` は同じ一時領域に専用サーバーを起動し、実APIの `workspace.create` で比較用workspaceを作る。所有領域の `herdr.sock` / `gtx.sock` だけに接続し、終了時に `server.stop`、PIDの終了と応答を `cleanup.json` へ保存する。通常の稼働中サーバーには接続しない。`socket_action.py` はその領域から継承したソケットとペインIDだけを使う。

`clicktext "表示ラベル"` は保存画面内で一意に見つかった行をクリックする。見つからない、または複数行なら失敗し、別項目で代用しない。`prefix_cases.py` / `probe-prefix.sh` はprefix後のキーを個別起動で調べる。実行していない手順は比較済みとしない。

`copy-detail.steps` は移動・検索・選択解除・終了だけを調べる。選択範囲のyankとマウス選択のreleaseは含めない。コピーの実行は別のsandbox付き手順と、実行ごとの証拠で確認する。

`measure.py 出力先` は画面の行数・セル幅・枠の座標と寸法・ペインタイトル・下部行・helpの行・確認用入力文字列を数値と文字列で記録する。各フィールドの一致だけを判定し、画面全体や操作全体の合格へ拡張しない。動的なペイン名等は全文差とあわせて結果表で確認する。

`cells 保存先.json` は端末エミュレーターのセル内容・幅・色・属性を保存し、文字だけで確認できない選択の位置を測定する。`terminal.raw` は端末出力を保存し、OSC 52などを通常の端末へ転送しない。no-sessionでの起動cwdは両方とも隔離HOMEへ揃える（herdrが初期shellをHOMEで開くため）。永続サーバーの比較workspaceは明示cwdで作る。

`cell_styles.py 出力先 screen label` は一意な共通ラベルの先頭セルの色・属性を比較し、不一致または未観測でexit 1を返す。座標も出力するが、項目数が違うメニューの座標は一致扱いにしない。`navigator_cases.py` / `probe-navigator.sh` は操作ごとに二つの実shell paneを新しく起動し、前の検索やモードが次の操作へ混入するのを防ぐ。

click/rclick/mouseの座標は1始まり、screen/cells/measure.pyの保存座標は0始まり。上端タブバーはクリック手順ではy1であり、y0を指定しない。

`navigator_compare.py 出力先` は navigator の枠を実物画面から検出し、枠全体の文字・セル幅・色・属性を比較する。Settings は `--title ' settings' --result settings-comparison.json` を付ける。`PASS` は観測した枠の全セル一致、`DIFFERENT_CELLS` は差あり、`NOT_OBSERVED` はその枠を観測できなかった画面を示す。枠が閉じた画面や設定保存の正しさを、この判定で合格扱いにしない。

`region_compare.py 出力先 --rect x,y,width,height --screens 保存名...` は指定した実測領域の全セルを比較し、省略せず差を保存する。折り畳みは `0,0,4,45`、60列のmobileヘッダは `0,0,60,2` を既取得画面で測った領域として使用する。これは領域比較であり、領域外や未実行操作の合格を意味しない。

要素ごとの通常検証・観測・相対性能は `gate-settings.sh`、`gate-navigator.sh`、`gate-sidebar.sh`、`gate-menu-detail.sh`、`gate-help-detail.sh` で繰り返す。比較結果の `DIFFERENT_CELLS` や `NOT_OBSERVED` はジョブのexit 0でも未達として確認する。結果の表と元の画面を見て判断し、テスト終了だけで完全一致としない。

通常テストは `go test -count=1 ./...` としてキャッシュを使わず実行する。E2E は TestMain 内で比較対象の外部バイナリをビルドするため、Go のテストキャッシュだけでは新しい実装の操作を確認できない。過去のキャッシュ利用結果を新しいバイナリの回帰確認として扱わない。

`gate-copy-safe.sh` はクリップボードへ書き込まない12手順と、nested vt の検査・通常検査・相対性能を実行する。`copy_cells_gate.py 出力先 手順ファイル` は手順にある全保存名が双方に存在することを必須にし、タブバーと下部ステータス以外のペイン全域の文字・幅・スタイルを厳格に比較する。高さ変更、全画面コピーからの復帰、日本語の単語移動、選択の開始と解除も含む。コピーの実行自体と選択releaseは検証範囲に含めない。

`gate-overlays.sh` は確認ダイアログと名前入力の通常検査・比較・相対性能を実行する。確認の detail は gtx SPEC の製品名読み替えを保持し、各製品名を含む正確な全文とスタイルを別々に検査する。それ以外のセルは一致を必須にする。Main メニューの比較結果は観察として保存し、メニュー項目の差をこのゲートの合格へ含めない。`confirm-navigation.steps` は矢印・Tab・内側クリック・右クリック・取消ボタン・再表示を追加観察する手順であり、実行と結果の検査が済むまでは未検証とする。

agent-summary.steps は隔離サーバーへ実API `pane.report_agent` を送り、報告された状態の表示を確認する。`PARITY_CONTROLLER_SESSION_ID` に実行している制御セッションの実IDを明示する。IDが無ければ未実行として止まり、架空IDや別セッションIDで代用しない。この手順はagentの自動検出やAI起動を検証していない。終了時には専用server.stop応答とPIDの終了を保存する。

`settings_size_compare.py 出力先` は settings-size.steps / settings-height-sections.steps で実測した余白と寸法から枠を指定し、4角の存在と枠内の全セルを比較する。低い画面でタイトルが無い枠も対象とする。`MISSING_EXPECTED_BOX` を合格と扱わない。toasts の製品名 herdr / gtx は差としてそのまま保存する。

`probe-help-detail.sh`、`probe-settings-size.sh`、`probe-name-buttons.sh` は `GTX_PARITY_BIN` を指定して同じ操作を繰り返す。これらの観察だけでは通常検査や性能の合格を意味しない。名前欄のclear後は空欄になるため、タイトル検出が未観測を返した場合も、保存画面と実測領域を region_compare.py で確認する。

`gate-local-restore.sh` は単独起動の復元確認、ワークスペース終了確認、名前、NAVからのprefixと直接c/v、worktree拒否の比較と通常・相対性能検査を実行する。`full_frame_gate.py 出力先 手順` は全保存名について文字・幅・スタイルの全セル一致を必須とする。復元の検査は正確なsession IDを使い、欠けたIDや`--last`を使って推測しない。

`gate-copy-success.sh` はsandbox付きの通常マウス選択とyank、複数行のy/Enter、逆向き、日本語、PHONE COPYからの全画面コピー、選択なしEnterを比較し、通常・相対性能も検査する。`osc52_gate.py 出力先` は子pbcopy拒否と本物のclipboardハッシュ不変に加え、raw出力から復号した全OSC52をバイト単位で比較する。選択なし終了には `--expect-empty` を指定し、両方ともOSC52が0件であることを必須にする。

`copy-feedback-boundary.steps` と `copy_deadline_gate.py 出力先` は成功表示の期限を確認する。ptyrunの`LastSendUnixMs`と`CapturedUnixMs`で実際の送信から採取までの時間を計算する。両製品で表示中の全セルと消去後の全セルが一致し、各観測の「最後に表示された時刻」から「最初に消えた時刻」の区間にSPECの2000msが含まれることを必須にする。sleepの合計や、同じ保存名の画面だけから期限を推測しない。

`server-chrome.steps` と `server-chrome-detail.steps` は隔離serverにAPIで作ったworkspaceを表示し、サーバー接続時の通常・NAV・help・Settings・Main・タブ追加・クリック・リサイズを観察する。`--server --server-default-workspace` はAPIでworkspaceを作る前の起動状態を観察する。作成の前処理とID割当の差もそのまま保存し、似たIDへの置換をしない。これらの観察は一致検査の合格を意味しない。

サーバー接続時の見た目は単独起動と別に比較する。`gate-server-chrome.sh` はサイドバー、名前入力、タブの多数表示・ホイール・リサイズ、Main の開閉とシェル入力、コピー中の下部表示を同じ手順で検査する。`--server-reuse-initial` は隔離サーバーに既存の workspace があればその実在IDを使用し、なければ作る。準備に使ったAPI応答は `fixture-preparation.json` にそのまま保存する。初期workspace数が異なる事実はこの準備で消えないため、初期APIの同一性を達成したとは扱わない。サーバー接続には初回案内が出ないので Enter 2回は送らない。

Main の項目追加・削除・並び替えはこの段階の対象外。`main_menu_frame_gate.py` は実際の枠から項目領域を求めて双方の項目を記録し、フッター直上の位置、項目領域の外の全セル、閉じた後の全セル、実シェルへの入力を検査する。項目領域だけの相違を全画面一致と報告しない。

新規タブの初期番号は Enter でそのまま採用でき、最初の文字入力では置き換わる。既存名前の rename は追記する。この差は `server-tab-name.steps` をサーバー接続と単独起動の両方で検査する。

`gate-jobs.sh` は実APIで起動したジョブのrunning/成功/失敗/取消、ログを開いてq・通常入力・Ctrl-C、popupの幅・ホイール・終了・リサイズを検査する。`--cell-clock` はPTY出力を受けた時刻をセルの文字が変わった時刻として保存する。`jobs_age_gate.py` は実ジョブログの開始・終了時刻と、そのセルが実際に描かれた時刻から経過秒を独立に検査する。`jobs_frame_gate.py` は証明済みの経過時間の文字以外の全セルを比較する。

`jobs_log_frame_gate.py` は各製品の実ジョブログにあるID・PID・コマンド・cwd・開始時刻・stdoutを、その製品のpopup内の文字と照合する。この手順のASCIIログは折返し無しであり、別の長文・日本語ログを検証済みとは扱わない。自分のログに一致した文字だけを内容比較から外し、幅・色・属性と残りの全セルを比較する。作業中の記号は動くため、idle状態の厳格な全セル比較と、working状態のキー配送検査を分けて残す。workingの表示一致は別の実測を必要とする。

`jobs_cancel_gate.py` は共通CLI `run cancel` の実応答と実ログに記録された正確なrunner PIDを使い、カーネルへのprocess group照会で消滅を確認する。取消中は実物のdone赤・経過時間-・dotなしを比較し、消滅後は経過時間も含めて全セルをそのまま比較する。SPECの所有・SIGTERM/SIGKILL・消滅前に取消完了としない安全規則を保持する。`waitfile` は実ファイルの準備が期限内に完了しなければ、ptyrunが起動したプロセスを終了させてexit 1にする。

`agent-working-clock.steps` は実 API で working・blocked・idle を報告し、working のセル変更時刻を保存する。`agent_clock_gate.py` は実 API の成功、点字の全周期と順番、両製品の実測区間に収まる相手の中央値を検査する。アニメーションの位相だけを独立に証明した後、位置・幅・スタイル・その他の全セルを比較する。`agent_states_gate.py 状態の出力先 周期の出力先` はこの周期証拠を必須として、desktop/mobile の idle・blocked・unknown・working を比較する。

`gate-agent-cells.sh` は隔離 server の agents 一覧のあふれ・ホイール・クリック・リサイズ、複数 agent、popup 外の workspace カードを二回クリックして切り替える操作を全セルで比較する。`agent-server-overflow.steps` の最後列のドラッグは、scrollbar が無い実物での行クリックも検証する。通常検査・性能の合格はこのスクリプトだけでは証明されない。

`agent-done-focus.steps` は初期の CSI O、working→idle 後の CSI I、入力・クリック・workspace 往復を送る。raw の CSI ?1004h と採取画面を保存し、focus 通知が有効な状態でも done が残るかを確認する。親の判断により、実物の done 残留は模倣せず SPEC の「表示中かつ focus 中の viewer が見ている tab を既読にする」を保持する。この手順の差を全画面一致と扱わない。

`jobs-overflow.steps` は隔離 server に二十件の実ジョブを起動し、jobs 一覧のホイールとリサイズを観察する。実 API 応答・実ジョブ ID は保存し、観察結果の検査前は未検証とする。

`popup-copy.steps` と `copy-workspaces.steps` は `--clipboard-sandbox` を必須として比較する。popup 内、複数 workspace の選択表示・mouse release・コピー後の入力・元の workspace への復帰を保存し、`osc52_gate.py` でコピーの全バイト、sandbox の拒否、コピー内容と一致する clipboard ハッシュ変化が無いことを検査する。`full_frame_gate.py` で各画面の全セルも比較する。sandbox の拒否失敗、または変化後の実ハッシュが捕捉したコピー bytes／その末尾改行付き bytes の SHA と一致したら、コピー比較を止め、元に戻そうとせず親へ報告する。一致しない外部の変化は各比較の proof.json／sandbox-probe.json に記録し、やり直しも全体の停止もせず続行する。sandbox-probe は毎回実行する。拒否に失敗したとき、または変化後のハッシュがその比較のコピー候補のいずれかと一致したときは全体を止める。popup を開く命令と画面消去は別々に送り、画面に出すと実行順と固有IDが比較へ混ざる API 応答は runtime-json.json に保存する。

`jobs_overflow_gate.py` は各製品の実 API 応答から label の順序と正確な job ID を確認し、一覧内の座標・一行ホイール・リサイズ後の位置保持を検査する。この検査は age の全セル比較を証明しない。age・ログ・取消の共有描画回帰は `gate-agents.sh` の実ジョブ時刻・ログ照合・process group 消滅検査で別に確認する。

`gate-modifier-section-cells.sh` は prefix に続く ESC と文字の連続入力、Navigator の修飾文字を単独起動・server 接続の両方で比較する。複数 section の card スクロール・見出し保持・折畳み・下端・resize と、既存の単一 section の操作も全セル検査する。`server-section-small-end.steps` は小画面のスクロール上限と上向きの戻りを追加検査する。画面一致だけで通常テストや性能を合格扱いにしない。

`prefix-notification.steps` は通知 target が無い状態の prefix+o と後続シェル入力を比較する。実通知がある状態で target へ移動する操作の証拠にはしない。focus history は既定の prefix 割当が無いため、47種類の既定操作 matrix へ架空の割当を足さない。

`menu_cases.py 出力先` は5種類のメニューの開閉・キー・wheel・resize・共通選択を独立した隔離PTYで採取する手順を生成する。`menu_matrix_compare.py 比較出力先 手順の出力先` は全差と枠内の項目一覧を保存する collector であり、exit 0 は合格を意味しない。項目領域内だけの差も、禁止されている項目内容・順序の差なのか、対象となる選択・位置・描画の差なのかを別に判定する。閉じた後と選択後の画面は項目領域を除外しない。

`prefix_full_gate.py 比較出力先 手順の出力先` は collector の各画面の判定を必須にし、新worktree拒否だけはSPECの製品名読み替えを正確な全文と全banner属性で検査する。`gate-modifiers.sh` は修飾入力/section/メニュー修正の通常検査と相対性能を実行する。`gate-final.sh` は最終ソースでgofmt/build/vet/race-count1、狭い/広いNavigatorの背景と閉じた後の入力、新規workspace閉じる確定、共有入力・スクロールの回帰、変更されたprefix作成/終了経路と相対性能を検査する。途中で失敗したら後続は未実行として扱う。

`prefix-notification-{target,blocked}-settled.steps` は隔離起動前に `--in-app-toast` を指定し、背景paneを実APIで working→idle/blocked にしてprefix+oと後続入力を比較する。`notification_target_gate.py` はAPIから得た正確なworkspace/pane ID、入力文字列、所有serverの終了を検証する。環境変数のID表記を推測で正規化しない。製品名の異なる設定値は `herdr` / `gtx` とそれぞれ正しく指定し、gtxは別名binaryでも同じ隔離configを `GTX_CONFIG` で読む。

`--cell-clock` と `prefix-notification-expiry.steps` は通知点・状態点の実PTY描画時刻を記録する。実物では開始遅延1秒（1001/1002ms）、表示期間8秒（開始999ms、終了9005ms）を観測した。時間は元のcell-clock JSONを証拠とし、検査していない時間や状態を代用しない。表示中のペインを既読にするSPEC規則は維持し、実物のdone残留は差として記録する。

`original-menu-cleared.steps` と `original-menu-routing-cleared.steps` は最初の再現操作の前に双方へ同じ端末クリアを送る。初回案内を閉じるEnterのシェル表示を揃え、外クリック/Escape後のメニュー消失、実echo出力、別pane/タブ追加へのクリックが消費されることを比較する。元の準備無しの結果も保存し、準備を入れた文字比較を全スタイル一致とは扱わない。

段階2（K8〜K14、0.2.57）は `stage2_matrix.py --out 出力先` をminiの
`src/detail` から実行する。`work/detail/gtx-stage2` を事前にビルドし、
`work/detail/user-config.toml` を使う。`stage2_cases.py` が同じ操作と設定の組を
出力先の `procedures/` に残す。`--only 手順名...` で修正対象だけを再実行できる。
`stage2_gate.py 出力先` は選択名・モード継続・子への文字配送・pane追加数・
設定診断・1〜9の全選択先を検査する。画面全体の一致を主張するゲートではない。
台帳より実物を優先し、名前変更と閉鎖は表示中workspace、Tabと方向キーは
navigate維持という0.2.57の観察結果を固定している。
`runtime-logs.json` は観察専用一時ディレクトリのserverログだけを保存する。
相対性能は `python3 ~/bench-terrace.noindex/work/perf-lock.py sh tests/parity/stage2_perf.sh`
で実行する。基準ソースは同じGo引数でビルドする `src/stage2-baseline-a68c878`。
絶対性能はこの工程の判定対象に含めない。
