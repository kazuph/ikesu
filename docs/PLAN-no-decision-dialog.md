# 判断待ちダイアログを画面から外す段階の計画

作成: 2026-10-13（親 Claude）。

## 依頼

店長（2026-10-13）: herdr fork（s277 の作業）で「herdr の画面の判断待ちダイアログをなくし、答えるのは Slack・スマホアプリ・グラスだけにする」方針に切り替えた。判断待ちの仕組み自体、画面の質問の判断待ち化、危険コマンドの判断待ちは残す。古い画面と新しいサーバーが混ざっても、何も出なくなるだけで壊れないようにする。gtx も「今すぐダイアログだけ消す」（店長の決定）。Slack などの受け口が入るまで、gtx の判断待ちは `gtx decision answer`（と `gtx decision watch` を読む外部の道具）で答える。

## 0. 作り方の決まり

- herdr のソースコードを読まない・移植しない（AGENTS.md）。
- gtx の判断待ちの段階（docs/PLAN-decision.md の工程2「decision dialog」）で作った、画面側のダイアログ・アクションバーの `! N` の目印・ダイアログを開くキーやクリックを取り除く。

## 1. やること

1. 画面（client）から判断待ちダイアログ、`! N` の目印、それを開く・閉じる・答える操作をすべて取り除く。判断待ちが pending の間も、画面には何も出ず、キー入力は普段どおり pane へ届く。
2. 残すもの: サーバーの判断待ち（`decision.create/get/list/wait/answer/cancel`、期限切れ、first-answer-wins、event と plugin の `[[events]]`）、CLI `gtx decision ask|answer|list|get|cancel|watch`、`--machine` での回答、見張りの ask の判断待ち（command_guard）。
3. 新旧の混在で壊れないこと: サーバーが画面へ判断待ちの一覧（projection）を送っている場合、新しい画面はそれを読み捨てる（または送らなくする。送らなくする場合も、古い画面が新しいサーバーにつながって壊れないこと）。endpoint generation の決まり（追加は min 1 のまま）を守り、世代を下げたり上げたりしない。保存マシンの接続も含めて確かめる。
4. 使われなくなったコード・テスト・設定の警告を整理する（ダイアログ専用のものだけ。判断待ちの仕組みのテストは残す）。

## 2. 守ること

- 変えてはいけないもの（変えたいときは実装前に親へ報告して止まる）: go.mod、判断待ちの API・CLI・event・保存形式、見張りの挙動、endpoint generation の値。1ファイル800行まで。計画1章に無い機能を作らない。
- 店長の本物の設定・state・稼働中の gtx と herdr に触らない。確かめは一時の HOME と名前付き session で行う。
- 普段の動きの性能を悪くしない（親が最後に測る）。go build はメインの Mac で可。テスト・race・ptyrun は mini。

## 3. 進め方と確認

- 区切りごとに mini で gofmt・go build・go vet・go test。最後に race。積み上げコミット。
- `.artifacts/no-decision-dialog/RESULT.md` に、各項目の表（条件・確かめたテスト名か保存した画面・状態）。特に、見張りの ask の判断待ちが pending の間に画面に何も出ないこと（実画面の保存）、`gtx decision answer` で答えるとコマンドが実行・却下されること、古い画面と新しいサーバーの組み合わせ（とその逆）で壊れないことを実際に動かして示す。確かめていないものは未達と書く。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
