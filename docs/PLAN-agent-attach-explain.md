# agent attach と agent explain の段階の計画

作成: 2026-10-13（親 Claude）。

## 依頼

店長（2026-10-13）: これまで親が確認なしに範囲から外していた残項目30（`agent attach` / `agent explain`）を「作る」と決定。herdr の CLI にある利用者向けの機能で、gtx に無い。

## 0. 作り方の決まり

- herdr のソースコードを読まない・移植しない（AGENTS.md）。挙動は herdr fork の SPEC.md（G5 の help 補足など）、`docs/ref/herdr-cli-ai.txt`（`agent attach <target> [--takeover]`、`agent explain <target> [--json]`、`agent explain --file PATH --agent LABEL [--json]`）、`docs/ref/herdr-api-schema.json`（`agent.explain` の params と結果）、herdr の利用者向け文書（`~/src/github.com/kazuph/herdr/docs/next/website/src/content/docs/` の socket-api.mdx・cli-reference.mdx・agents.mdx など）、herdr の実物の観察で決める。
- 実物の観察は ubuntu-3090（`ssh ubuntu`）か店長のマシンで、店長の稼働中の herdr とは別に、一時の HOME・名前付き session で行う（ubuntu-3090 には herdr が入っているかを確かめる。無ければ herdr の配布物を一時フォルダに置いて使う。店長の稼働中の herdr には触らない）。

## 1. やること

1. `gtx agent attach <target> [--takeover]`: herdr と同じ意味・出力・終了 code で、指定した agent の pane に接続する（`--takeover` の意味も herdr に合わせる）。
2. `gtx agent explain <target> [--json]` と `gtx agent explain --file PATH --agent LABEL [--json]`、API `agent.explain`: agent の状態（working・idle・blocked など）をどう判定したかを説明する。説明の項目（最終状態、検出ルールの出どころと版、一致したルール、評価したルールの根拠、状態の判定を飛ばした理由、idle に落ちた理由、画面の判定を飛ばした理由など。socket-api.mdx に書かれた項目）を herdr と同じ名前・形で出す。gtx の検出の仕組み（同梱のルール）で得られる情報を正しく出し、gtx に無い情報（remote の manifest の版など）は herdr の扱いに合わせて空または未対応として出す（似た値で埋めない）。
3. `gtx agent` の help と `gtx help` に追加。

## 2. 守ること

- 変えてはいけないもの（変えたいときは実装前に親へ報告して止まる）: go.mod、既存の API・CLI・検出の挙動・保存形式・endpoint generation。1ファイル800行まで。計画1章に無い機能を作らない。
- 店長の本物の設定・state・稼働中の gtx と herdr に触らない。
- 普段の動きの性能を悪くしない（親が最後に測る）。

## 3. 進め方と確認

- 区切り（実物の観察メモ → explain → attach）ごとに ubuntu-3090（`~/bench-terrace/env.sh`、作業は `~/bench-terrace/work/<名前>`）で gofmt・go build・go vet・go test。最後に race。メインの Mac で go build と go vet。積み上げコミット。
- `.artifacts/agent-attach-explain/RESULT.md` に、各項目の表（条件・確かめたテスト名か保存した出力・状態）と、herdr と gtx の出力の比較を書く。確かめていないものは未達と書く。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
