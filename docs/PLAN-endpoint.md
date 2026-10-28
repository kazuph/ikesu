# 画面側で描く作り（endpoint client）と複数画面・保存マシンの段階の計画

作成: 2026-10-11（親 Claude）。

## 依頼

店長（2026-10-11）: 次の段階へ進めてよい。作り方は「herdr fork と同じく、画面側（つないだ側）がサイドバー・タブ・枠を描き、サーバーからは状態と pane の中身を受け取る作り」に変える（親の推奨を店長が選択）。

経緯: 今の ikesu はサーバーが画面全体を描いて1つの画面に流す。2つ目の画面がつなぐと1つ目は切られる（`internal/server/server.go` の attach）。SPEC.md の複数画面と保存マシンの節は、画面側が描く作りを前提に書かれている。

## 0. 作り方の決まり（最優先）

- herdr のソースコード（`~/src/github.com/kazuph/herdr` の `src`・`tests` などの実装とテスト）を読まない。移植しない（AGENTS.md）。
- 仕様の正は次のとおり。`herdr` → `ikesu` と読み替える。
  - `~/src/github.com/kazuph/herdr/SPEC.md` の次の節
    - G10「取込packet UP-MULTICLIENT」（接続の互換 C1〜C3、画面ごとの表示 A1〜A8、endpoint generation の決定事項）
    - G11「sidebar machine groupと横断表示（P3）」「再接続・backoff・切断時の画面分離（P4）」「endpoint clientでのfork操作と入力の保持」
    - G2「Space cardのjob dot」と、G2 の「接続中のclientへ画面情報を配る時」の決まり（session 全体の読み取りは1回の配信につき1回）
    - G9「問い合わせとjob記録によるserver負荷の抑制」の複数 client の条件
  - herdr の利用者向け説明 `docs/next/website/src/content/docs/`、`docs/ref/`
- 細部は mini の herdr 0.2.24（`~/bench-terrace.noindex/work/herdr-0.2.24`）を一時の設定・state・session 名で動かして観察する。2つの画面からの同時接続も、保存マシン（mini 自身への ssh など、店長の本物の herdr・ikesu と別の session 名）も実物で観察する。
- 仕様と実物が食い違い、SPEC が「〜してはならない」と書く挙動は、親へ報告して止まる（親が店長へ確認する）。

## 1. やること

### 1-1. 接続の土台
1. 1つのサーバーへ複数の画面が同時につながる。2つ目がつないでも1つ目を切らない。
2. 画面ごとに「見ている space・tab」と「画面の大きさ」を持つ。片方の切替がもう片方を変えない（A1）。tab の配置はその画面の大きさに合わせる（A2）。同じ tab を2つの画面が見ているときは、最後に操作した画面が大きさを決める（A3）。
3. endpoint generation の交渉（`generation` と `min_generation` の範囲で判定、範囲外は `endpoint_generation_unsupported` / `endpoint_generation_newer` で明示拒否、相手に無い機能の操作だけを無効化、pane 解決・session 復元・mailbox 配送は拒否のまま）（C1〜C3）。既存の client と server の約束（`PROTOCOL_VERSION` 相当）とは別の軸にする。
4. 1回の配信で session 全体の読み取りは1回だけにし、全画面で共有する（G2・G9 の条件）。

### 1-2. 画面側で描く
1. `--session` で起動した画面（client プロセス）が、サイドバー・tab bar・pane の枠と title・下の操作行・メニュー・ダイアログ・トーストを描く。サーバーは状態（space・tab・layout・pane の情報・agents・jobs・既読・設定）と pane の中身を送る。`--no-session` は今の一体の作りのまま残す。
2. 今まで herdr 0.2.24 と合わせてきた見た目と操作（tests/parity と e2e、これまでの段階の比較手順）を、画面側で描く作りでも全部保つ。SPEC「endpoint clientでのfork操作と入力の保持」の各挙動（title click zoom、サイドバー幅と区切りの drag と保存、tab chip と `+` と `<` `>`、card 透過、space card の drag による並べ替えと section 移動、幅ボタン、job 行の保持、viewer へ戻ったときの PTY 寸法、表示中 viewer の focus tab だけ既読、focus 復帰時の最長1秒のキー預かり、描き直し中の入力の受付）。
3. focus の伝播と画面ごとの title（A4〜A8）。
4. OSC 52 のコピーは、表示中で最後に使われた画面の機械のクリップボードへ届ける。保存マシンの pane を表示中の ⌘V は手元のクリップボードを読んで貼る（店長のクリップボードを確かめで使わない。後述）。

### 1-3. 保存マシンをサイドバーに並べる
1. 画面側が、有効な保存マシンごとにそのマシンの ikesu サーバーへ ssh で直接つなぐ（保存済み接続は remote のサーバーを起動しない）。Local の space の下に、マシンごとの group（見出し＋`[new]`）とそのマシンの space を並べ、click でそのマシンの pane を表示する（P3）。Agents タブに `Local: online` のような接続状態を出す。
2. 再接続と画面の分離（P4）: 0.5秒から倍々・最大120秒（Local は最大30秒）、60秒以上安定で回数を戻す、要対応の失敗は30秒後に再試行してその時だけ通知、切替は5秒で取り消し、切断中の選択マシンは pane 領域に状態と理由、通知の出し場所と10秒で消えること、resize の扱い、Tailscale SSH の `To authenticate, visit:` の承認待ち（最大30分、URL を手元のブラウザで1回だけ開く、「承認待ち。クリックで再試行」）。
3. ID の決め方（SPEC G11「親判断事項」を親が決定）: 保存マシンの pane・space はそのマシンの ID のまま表示し、複合 ID を作らない。手元の CLI・mailbox・`ikesu run` は `--machine` 指定なしに保存マシンの pane を解決しない（fail closed）。

### 作らないもの（別の段階）
- 判断待ち（decision hub、G9 の工程1〜3）。この段階が入ったあとに別の段階で作る。
- サーバー引き継ぎ（live handoff）時のマウス設定、kitty 画像。
- Windows 向けのもの。

## 2. 守ること

- これまでの段階と同じ: 1ファイル 800 行まで、推測で埋めない、未対応は未対応と書く（似た値で代用しない）、依存ライブラリを足さない、計画1章に無い機能は作らない。
- 店長の本物の設定・state・稼働中の herdr と ikesu（店長の Mac と mini の両方。mini の店長の herdr は止めない）・クリップボードに触らない。mini でクリップボードが関わる確かめは `sandbox-exec -p '(version 1)(allow default)(deny mach-lookup (global-name "com.apple.pasteboard.1"))'` で遮断し、テスト用の偽のクリップボードで確かめる。
- 保存マシンの確かめは、テスト専用の session 名と一時の state で行い、確かめが終わるたびに自分で起動したサーバーと ssh を止める。
- go build はメインの Mac でしてよい。テスト・race・ptyrun の観察は mini。
- 性能: 最後に親が TestRelative（基準は main 85b370a）で測る。合格の基準は SPEC P1 の既存の相対条件（画面に出るまで・CPU 時間は 1.05 倍、メモリは 1.10 倍、Ctrl-C の反応）。画面側で描く作りで基準を満たせない見込みが出たら、実装を進める前に親へ報告して止まる。

## 3. 進め方と確認

- 区切り: 接続の土台（1-1）→ 画面側で描く（1-2）→ 保存マシン（1-3）→ herdr 0.2.24 との通し比較。区切りごとに mini で gofmt・go build・go vet・go test を通して積み上げコミット。最後に race。
- `.artifacts/endpoint/RESULT.md` に、SPEC の受け入れ条件ごとの表（条件・確かめたテスト名か実物と比べた画面・状態）を書く。確かめていない条件は未達と書く。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
