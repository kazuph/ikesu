# live handoff と kitty 画像の段階の計画

作成: 2026-10-12（親 Claude）。

## 依頼

店長（2026-10-11）: SPEC.md を全部実装する方針のもと、段階を取り込んだら止まらず次へ進めてよい。前の段階（危険コマンドの見張り）が main ffbf2be に入ったので、残項目一覧（`.worktree/feat/endpoint/.artifacts/endpoint/SPEC-REMAINING.md`）の残項目17（live handoff と mouse 報告状態の引継ぎ）と残項目18（kitty 画像と XTVERSION）、判断待ちの段階で後回しにした「handoff 後も判断待ちが消えない」をこの段階で作る。

## 0. 作り方の決まり（最優先）

- herdr のソースコード（`~/src/github.com/kazuph/herdr/src/**`、tests など）を読まない・移植しない（AGENTS.md）。挙動は SPEC・利用者向け文書・API スキーマ・実物の観察で決める。
- 仕様の正: `~/src/github.com/kazuph/herdr/SPEC.md` の「live handoffでのmouse報告状態の引継ぎ」（L1812付近）、「kitty画像対応paneのXTVERSION」（L1822付近）、判断待ちの節の handoff の記述（L1893・L1961付近）、起動時 restore の handoff の除外（L821付近）、pane 番号の handoff 復元（L1111付近）。利用者向け文書 `~/src/github.com/kazuph/herdr/docs/next/website/src/content/docs/session-state.mdx` の「Live handoff」節と `docs/next/CHANGELOG.md` の handoff の行（plugin の保持、名前付き session の socket の保持、遅い停止を待つ、旧サーバーが終わる前に API 応答を出し切る、pane ごとに PTY の親側を1つだけ持つ、端末状態の保持）。`herdr` → `gtx` と読み替える。
- 実物の観察: mini の herdr 0.2.30 配布用ビルド `~/.local/share/herdr-builds/0.2.30/herdr`（`herdr server live-handoff` と `[experimental] kitty_graphics` がある）を、一時の HOME・XDG・state・名前付き session で ptyrun の中で動かして観察する。店長が使っている稼働中の herdr（mini の 0.2.27 を含む）に触らない・止めない。
- SPEC が「〜してはならない」と書く安全側の挙動は SPEC を正とする。実物と SPEC が食い違い、どちらにも決められないときは親へ報告して止まる。

## 1. やること

1. **live handoff の本体**（残項目17）
   - CLI `gtx server live-handoff`、API `server.live_handoff`（params は `docs/ref/herdr-api-schema.json` の `ServerLiveHandoffParams`: `expected_protocol`・`expected_version`・`import_exe`）、`ping` の応答に `live_handoff` の対応有無を載せる。`gtx server --help` を herdr の `server` の help と同じ形で出す（実装済みの stop・restart・ping と live-handoff を載せ、gtx に無い操作は載せない）。
   - 旧サーバーが新しい実行ファイル（`import_exe`、省略時は観察で決める）を起動し、各 pane の PTY の親側（pane ごとに1つ）と端末状態を渡して、新サーバーが同じ socket で受け付けを始めてから旧サーバーが終わる。pane の中のプロセス（shell・agent・dev server など）は止まらず、同じ PID のまま動き続ける。
   - 引き継ぐもの: workspace・tab・pane の構成と pane 番号、画面と scrollback、cursor、端末の mode（mouse 報告の mode と座標形式を必ず含む。alternate screen・bracketed paste・kitty keyboard の flags なども観察で決める）、agent の印と session 情報、plugin の登録（次の plugin 追加で登録表を上書きしない）、名前付き session の socket の場所、判断待ち（下の3）、見張りの agent pane の印。
   - 渡した後に pane のプロセスが終わったとき、引き継ぎ前と同じように pane が閉じる（または終了表示になる）こと。旧サーバーの子でなくなったプロセスの終了を新サーバーが確実に検知すること。
   - 接続中の画面（client）は、handoff の後もつなぎ直しの操作なしで表示と入力が続くこと（実物の観察に合わせる）。
   - 失敗時（版や protocol の不一致、新しい実行ファイルが起動しない・応答しない）は旧サーバーがそのまま動き続け、pane を1つも失わない。エラーの文言と終了 code は実物に合わせる。
   - 起動時 restore の skip 処理は handoff で引き継いだ pane を対象にしない（SPEC L821）。
2. **mouse 報告状態の引継ぎ**（SPEC L1812 の受け入れ条件）: handoff 前に有効だった mouse 報告が、handoff 後も同じ形式で pane へ届く。
3. **判断待ちの持続**: handoff 後も pending の判断待ちは消えず、期限切れの判定も続く。同じ質問を二重に作らない（SPEC L1893・L1961）。
4. **kitty 画像と XTVERSION**（残項目18）: `[experimental] kitty_graphics = true` のとき、
   - その pane は XTVERSION（`CSI > q`）に `ghostty (gtx <版>)` と答え、kitty 画像の問い合わせに `OK` と答える。無効な pane は今と同じ既定の答え（実物の無効時の答え `libghostty` と同じ形にするかは観察と既存の挙動を見て決め、RESULT.md に理由を書く）で、画像対応を名乗らない。
   - pane の中の program が kitty 画像の手順で送った画像を、接続中の画面が外側の端末（kitty 画像対応の端末）へ、pane 内の正しいセル位置・大きさで描く。scroll・pane の移動や大きさ変更・tab や workspace の切替・pane の close で、ずれずに動くか消える。描き方の細部（どの手順に対応するか、外側の端末へ送る列）は実物の観察で決める。
   - 設定の警告（`internal/config/compat.go` の kitty_graphics の未対応警告）を外す。

## 2. 守ること

- **店長の本物の環境に触らない（最重要）**: 店長の state・設定・クリップボード・稼働中の herdr と gtx（メインの Mac と mini の両方）に触らない。handoff の確かめは一時の HOME・state・名前付き session で自分が起動したサーバーだけで行い、確かめのたびに止める。
- 変えてはいけないもの（変えたいときは実装前に親へ報告して止まる）: 依存ライブラリを足さない（`go.mod` を変えない）。CLI 名 `gtx server live-handoff`、API 名 `server.live_handoff` と params の名前、ping の項目名 `live_handoff`。既存の API・event・CLI・保存マシン接続（endpoint generation の決まり）・判断待ち・見張りの挙動を壊さない。1ファイル 800 行まで。計画1章に無い機能を作らない。
- 近似で埋めない: 実装できない引き継ぎ項目や kitty 画像の手順は、未対応と明示し（必要ならエラーや警告を出し）、RESULT.md の未達に書く。似た値で代用しない。
- 普段の動き（handoff を使っていない時）の画面・CPU・メモリを悪くしない。性能（TestRelative、基準 main ffbf2be）は親が最後に測る。
- go build はメインの Mac で可。テスト・race・ptyrun は mini。

## 3. 進め方と確認

- 区切り: 実物の観察メモ → handoff の本体（PTY と構成の受け渡し、失敗時に旧サーバーが残る）→ 端末状態と mouse 報告の引継ぎ → 判断待ち・plugin・見張りの印の持続と接続中の画面の継続 → XTVERSION と kitty 画像。区切りごとに mini で gofmt・go build・go vet・go test を通して積み上げコミット。最後に race。
- `.artifacts/handoff/RESULT.md` に、SPEC の受け入れ条件と上の1章の各項目ごとの表（条件・確かめたテスト名か保存した画面・出力・状態）を書く。handoff の前後で同じ PID が動き続けていること、mouse 報告が同じ形式で届くこと、kitty 画像が正しい位置に描かれることは、実際に動かした証拠（出力や画面の保存）で示す。確かめていない条件は未達と書く。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
