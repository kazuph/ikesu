# 判断待ち（decision hub・dialog・watch）の段階の計画

作成: 2026-10-11（親 Claude）。

## 依頼

店長（2026-10-11）: SPEC.md を全部実装する方針のもと、段階を取り込んだら止まらず次へ進めてよい。前の段階（画面側で描く作り・複数画面・保存マシン）が main a346644 に入ったので、残項目一覧（`.worktree/feat/endpoint/.artifacts/endpoint/SPEC-REMAINING.md` の残項目19〜21）のうち、判断待ちの3工程をこの段階で作る。

## 0. 作り方の決まり（最優先）

- herdr のソースコードを読まない・移植しない（AGENTS.md）。
- 仕様の正: `~/src/github.com/kazuph/herdr/SPEC.md` の G9「判断待ち（decision hub、工程1）」「判断待ちのマシン横断UI（decision dialog、工程2）」「判断待ちの配信とマシン横断回答（decision watch、工程3A）」と、関連する `docs/ref/`・利用者向け説明。`herdr` → `gtx` と読み替える。
- 見本の herdr 0.2.24 には `decision` コマンドが無い（`unknown command: decision`）。実物の観察はできないので、SPEC の文と既存の gtx の見た目（dialog・アクションバー・トースト）の作法に合わせる。SPEC に無い細部を自分で決めたときは RESULT.md に「SPEC に記載なし・gtx の既存の作法に合わせた」と書く。

## 1. やること

1. **工程1（decision hub）**: 判断待ちの record（`dec-` の ID、kind、title、body、options、allow_text、origin、期限、status、answer）を既存の dispatch DB に `decisions` 表として永続化する。ソケット API `decision.create/get/list/wait/answer/cancel`、first-answer-wins（2件目は `decision_already_resolved`）、期限切れの遷移（再起動後も継続）、`decision.created`/`decision.resolved` を event hub と plugin の `[[events]]` フックへ発行。CLI `gtx decision ask|answer|list|get|cancel`（ask の終了 code: answered=0、expired=3、cancelled=4、pane 内なら origin.pane_id、接続が切れても同じ ID で待ち直す）。
2. **工程2（decision dialog）**: server が接続中の各画面へ pending 全件の projection（boot_id・revision 付き）を送り、画面側は全マシン（Local と保存マシン）の projection を束ねて古い順に1件ずつ dialog を出す。表示項目（title・body・マシン名・origin・選択肢・`N/M`・残り時間、長い本文は折返しか scroll）。dialog 内をクリックするまでキーは pane へ届き、Enter 一発で誤回答しない。クリック後は上下・Enter・text 入力。Esc/close で回答せず閉じ、アクションバーに `! N` の目印、クリックで再表示。回答は所有マシンへ `decision.answer`（responder=`client`）。外部で決着したものは消え、切断したマシンの分は外れて再接続で戻る。
3. **工程3A（decision watch）**: `gtx decision watch [--all-machines]` の JSON 行配信（created/resolved/machine_up/machine_down、flush、パイプ切断で静かに終わる、再接続で pending を出し直す）、保存マシンは ssh で `gtx --session <session> decision watch`、1台の失敗を隔離、Tailscale の認証行ではブラウザを開かず machine_down にして10分後にリトライ。`gtx decision answer --machine <label-or-id>`。

### 作らないもの
- 判断待ちを使う側の機能（危険 command の判定＝G12 の guard、agent の tool 許可の自動作成など）。別の段階で作る。
- live handoff そのもの（「handoff 後も pending が消えない」は、gtx に handoff が入った段階で確かめる。今回は再起動後の持続を確かめる）。

## 2. 守ること

- これまでの段階と同じ: 1ファイル 800 行まで、推測で埋めない、未対応は未対応と書く、依存ライブラリを足さない（DB は今の SQLite 部品のまま）、計画1章に無い機能を作らない。
- 既存の API・event・CLI・画面側の保存マシン接続（世代の決め方を含む）を壊さない。新しい画面向けの通知（projection）が必要なら、前の段階で決めた endpoint generation の決まり（追加は min 1 のまま、使う action の必要世代を上げる）に従う。
- 店長の本物の設定・state・クリップボード・稼働中の herdr と gtx に触らない。保存マシンの確かめはテスト専用の session と一時の state で行い、自分で起動したサーバーと ssh は確かめのたびに止める。
- go build はメインの Mac で可。テスト・race・ptyrun は mini。性能（TestRelative、基準 main a346644）は親が最後に測る。大量出力中の処理を増やさない（前の段階で余裕が小さいため）。

## 3. 進め方と確認

- 区切り: 工程1 → 工程2 → 工程3A。区切りごとに mini で gofmt・go build・go vet・go test を通して積み上げコミット。最後に race。
- `.artifacts/decision/RESULT.md` に、SPEC の受け入れ条件ごとの表（条件・確かめたテスト名か画面・状態）を書く。確かめていない条件は未達と書く。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
