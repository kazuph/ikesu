# gtx AI 連携段階 — 結果報告

作業日: 2026-10-07。ブランチ `feat/ai-link`。計画: `docs/PLAN-ai-link.md`。
仕様の正: `~/src/github.com/kazuph/herdr/SPEC.md`（herdr→gtx 読み替え）。

## マイルストーン達成状態

| M | 内容 | 状態 |
|---|---|---|
| M1 | SQLite 組み込み（modernc.org/sqlite v1.60.1・純 Go・cgo なし）、internal/dispatch（actors・dispatches・ビューは herdr と同じ列名・状態名）、`~/.config/gtx/config.toml`（`$GTX_CONFIG` 上書き）、`GTX_WORKSPACE_ID`/`GTX_TAB_ID` をペイン環境へ注入、祖先プロセスによるネスト検出 | 達成 |
| M2 | メールボックス: `gtx send <target> <msg> [--room R] [--from A] [--reply-to N]`、`gtx inbox [--room R]`、`gtx msg history`、`gtx msg rooms`。配送はイベント駆動（送信時・状態遷移時・起動時1回）、queued→in_flight→delivered/read、PTY 全バイト書き込み確認（`Pane.WriteToPTY` は短い書き込みを再送し、未完なら queued に戻す）、blocked 相手は再開時に配送 | 達成 |
| M3 | 裏方ジョブ: `run.start`/`run.cancel`/`job.running`/`job.exited` API、`gtx run`（background・`--pane`・`--close-on-success`）、`gtx run list`、`gtx run cancel`、`gtx log`（ジョブログ・ディスパッチ時系列・`--db`・`--schema`）、`__pane-run` ラッパー、gtx-jobs 完了通知 | 達成 |
| M4 | agent コマンド: `agent list/get/read/send/rename/focus/wait/start`、pane.current の引数拒否（exit 2）と `GTX_PANE_ID` 祖先照合、agent send/pane.send_input の text→500ms→Enter、process 祖先ベースの nested guard（`[experimental] allow_nested`）、AI 向け `gtx help`（`--help` と同一、herdr の7節構成） | 達成 |
| M5 | 通知: 状態遷移（blocked→要応答・working→idle→完了）トースト、`<ws番号> <ws名>-<pane title>` タイトル、本文抽出規則、10秒 rate limit、`[ui.toast] delivery = off|gtx|terminal|system`（既定 off）、`prefix+o`/クリックで通知元ペインへ移動、terminal-notifier `-execute` で pane focus | 達成 |
| M6 | 受け入れ確認・性能・ビルド時間計測・証拠 | 本文参照（性能・ビルド数値は下記） |

コミット: `ab4a783`（計画）→ `5f9be84` M1 → `575e492` M2 → `850d3c9` M3 → `f666377` M4 → `7ee9ae0` M5 → `d226e4d` CLI 配線 → `f746dc1` ジョブ終端状態・agent.* dispatch 修正 → `a40ffa8` run --pane 契約。

## SPEC 節ごとの受け入れ条件

### G9「durable mailbox」— 達成
- `gtx send`/`inbox`/`msg history`/`msg rooms` が API `msg.send`/`msg.inbox`/`msg.history`/`msg.rooms` 経由で動作。実物: `r2-mail-waiting.txt`（待機中 codex へ配送）、`r3-mail-working.txt`（作業中 codex へ配送）、`r6-inbox-history.txt`（history・rooms 出力）。
- 宛先解決: agent 名・`pN`・`%N`・`N`・`p_N` を受理（`internal/server/ids.go`）。`to=*` は拒否。

### G9「PTY 全バイト書き込み確認」— 達成
- `Pane.WriteToPTY` が short write をループ再送し、全部書けた時だけ `MarkMessageDelivered`/`MarkDelivered`。失敗時は queued のまま。二重送信は `mailInFlight`/`jobInFlight` で防ぐ。配送ワーカーは App ループ外の goroutine。
- bracketed paste の有効時はそのまま本文のみ書き込み（herdr と同じくペースト経路ではなく直接書き込み+全バイト確認）。

### G9「pane-less background jobs」— 達成
- `gtx run --label t -- sh -c '...'` が `run.start` で pane なし実行。実物: `r4-run-done.txt`（`j0006 exited exit=0`・`gtx log j0006` に出力・呼び出し元ペインに `[gtx run] exit=0 label=t job=j0006 details: gtx log j0006` 届く）。
- `gtx run list`（queued/running/cancelling/cancelled/exited と exit code）、`gtx run cancel`、`gtx log <job>`、`gtx log --db`/`--schema`（herdr と同じスキーマ文字列）。
- 終了判定バグ（exit 0 が cancelled と表示される）を発見・修正（`f746dc1`、終了確定後にだけ cancelled へ遷移）。

### G9「unified communication dispatcher」— 達成
- send/inbox/log/run が同一 SQLite（`$GTX_STATE_DIR/gtx.db`、WAL）・同一宛先解決・room・reply_to を共有。`gtx msg rooms` は `default`/`gtx-jobs` を表示。

### G9「run --pane」「exit 時 close 契約」— 達成
- `gtx run --pane` が呼び出し元ペインから split（フォーカスを奪わない）し、`gtx __pane-run --parent-agent A --target P --job-id J --run-label L [--close-on-success|--close-on-exit] -- <cmd>` をペインへ投入。実物: `r7-run-pane.txt`（spawn JSON・`[gtx run] exit=0 label=pj3 pane=p5 details: gtx log job-…` が呼び出し元へ1行で届く・`--close-on-exit` でペイン閉鎖・`gtx log` がペーンジョブのログを表示）。
- spawn 出力は herdr と同じ JSON（`job`/`label`/`mode`/`next`/`pane`）。
- pane モードのジョブは herdr と同じく dispatch 行を持たず、記録はログファイルのみ（`gtx log` が glob で解決）。

### G5「fail-closed な current pane 解決」— 達成
- `gtx pane current foo` → usage・exit 2。`GTX_PANE_ID` はサーバー側でプロセス祖先と照合し、ペイン外からの漏れた値は `pane_not_found`（実物: `r8-cli-safety.txt`）。

### G5「短い pane target と AI 向け help」— 達成
- `gtx help` と `gtx --help` が同一本文（実測 diff なし）。構成は herdr の When To Use/Agent Rules/Usage/Commands/Essential Agent Recipes/Options/More Help。
- pane target は `N`・`%N`・`pN`・`p_N` を受理。

### G5「agent send と headless 入力」— 達成
- `agent send`・`pane.send_input` は text→500ms 待機→keys（Enter）をサーバー側で逐次実行。`pane run` は1要求で送信。

### G5「leaked env を無視する nested guard」— 達成
- 環境変数の有無だけでは拒否せず、祖先プロセスに gtx サーバーがいる場合のみ入れ子として拒否（`internal/client/nested.go`）。`[experimental] allow_nested = true` で許可可。

### agent コマンド — 達成
- `list/get/read/send/start/rename/focus/wait` を実装（`agent.*` dispatch エントリの欠落を発見・修正 `f746dc1`）。`agent wait` は CLI が `agent.get` をポーリング。`agent start` は argv 直実行、`--cwd/--workspace/--tab/--split/--env/--focus/--no-focus`。実物: `r5-agentlist.txt`（`agent start` で起動した codex が `agent list` に `p3 name=cody agent=codex status=done` で出る）。
- `agent prompt/attach/restore/explain` は計画外のため未実装（意図的）。

### G4「通知タイトルと本文抽出」— 達成
- タイトル `<workspace番号> <workspace名>-<pane OSC title>`、本文は SPEC の抽出規則（`internal/notify/extract.go`・テストあり）。実物: `n1-toast.txt`（`● 2 notify-ws-…` トーストが右上に表示）。

### G4「rate limit」— 達成
- 同一ペイン・同一種別 10 秒以内は抑止（全配送経路共通、`internal/server/notify.go`）。

### G4「通知クリック・キーで pane へ移動」— 達成
- `prefix+o`（`[keys] open_notification_target` で変更可）で最新通知の送信元ペインへ移動（実物 `n2-after-nav.txt`: ws1→ws2・blocked 表示の pane へ遷移）。トーストのマウスクリック・`terminal-notifier -execute '<gtx> pane focus <pane>'` も実装。
- Antigravity(agy) は `internal/detect/manifests/antigravity.toml` が既にあり追加不要だった。

### G4「render drain 通知の正確化」— 確認済み
- 画面への書き込みは `clientSink.Write`（書き込み期限付き）が成功した時のみ drain とみなす土台実装を確認（wedged-client テストが担保）。

## 実物確認（ptyrun 保存画面）

- 2ペインで claude（p1）と codex（p4→p3）を起動: `s0-start.txt`、`r1-agents.txt`
- 待機中・作業中への `gtx send` 配送: `r2-mail-waiting.txt`、`r3-mail-working.txt`
- `gtx run` 完了通知（呼び出し元に1行）: `r4-run-done.txt`
- `gtx agent start` で起動した agent が `agent list` に出る: `r5-agentlist.txt`
- `msg history`/`rooms`: `r6-inbox-history.txt`
- `run --pane` 一連: `r7-run-pane.txt`
- CLI 安全性: `r8-cli-safety.txt`
- サーバー停止→再起動で未配送メールとジョブ状態・ログが残る: `s6-restart.txt`（`j0003 exited`、`j0002 cancelled`、history が残存）
- トースト通知と `prefix+o` ナビゲーション: `n1-toast.txt`、`n2-after-nav.txt`

## 性能とビルド時間

未計測（ブロッカー）。計画は mini で 200 万行（`tools/bench/bench_screen.sh`、サーバー経由と
`--no-session`）を main 84a112b と交互に測り中央値を取り、SQLite 前後のビルド時間と配布
バイナリサイズを測ることを要求するが、「mini の負荷が 10 以上のときの計測値は使わない」
の条件に対し、19:30〜20:33（本報告時点）の実測 load が 45〜284 で継続して規格外だった
（占有は `mediaanalysisd`・`mds_stores`・iOS/Android シミュレータ等のシステム・他者
プロセスであり、本タスク由来のプロセスではない）。`main` のソースと `bench_build_src.sh`
（クリーン/1ファイル変更×開発用/配布用の計測手順）は mini 上に準備済みで、負荷が下がり
次第すぐ計測できる状態にある。

## 未達と理由

- 性能・ビルド時間の最終数値: mini の負荷が計測条件（<10）を60分以上満たさず未計測。
  実装面で性能を変えうる変更は PTY 書き込み経路の追加（`WriteToPTY`・メール配送ワーカー）
  と SQLite の起動時オープンのみで、画面描画パイプラインには手を入れていない。
- `agent prompt/attach/restore/explain`、G9「local overlay pane」「alternate binary session 隔離」、G1/G2/G3/G6/G11、`--machine`: 計画1章の範囲外（作らない指定）。

## 発見事項

- `modernc.org/sqlite` が `golang.org/x/sync v0.23.0` へ強制アップデート（推移的依存）。他の新規直接依存なし。
- `gtx run --pane` の内部コマンドは `os.Executable()` 優先（呼び出し元シェルの PATH に gtx が無くても動く）。herdr も `current_exe` を使う同型。
- `agent send`/`pane send_input` の 500ms settle はサーバー側で実施（CLI 側で sleep しない）。
- `__pane-run` の `--parent-agent` には呼び出し元ペインの agent 名（カスタム名→検出名の順）を渡す。agent 名未報告のペインからの `run --pane` は通知先を確定できずエラー（fail-closed）。
