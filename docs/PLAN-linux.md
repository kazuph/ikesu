# Linux で全テストを通す段階の計画

作成: 2026-10-13（親 Claude）。

## 依頼

店長（2026-10-13）: mini がつながらないので ubuntu-3090 で確かめることにしたところ、取り込み済みの main（42aaeca）で Linux だけ落ちるテストが約25件見つかった。「ちょうどいいじゃん！直してね。」仕様（herdr fork の SPEC.md と ikesu の SPEC.md）では ikesu は macOS と Linux の両方に対応する。

親が見た失敗（main 42aaeca、ubuntu-3090、Linux amd64、git 2.34.1、`/usr/bin/sqlite3` 無し、vim 無しで vi のみ）。一覧は `.artifacts/linux/baseline-failures.txt`。
- internal/worktree・internal/tui・internal/server の worktree 系: git 2.34 で `git worktree` の新しい書き方が通らない様子（usage が出る）。
- internal/notify: `unsupported: sound playback on linux`（Linux での音の再生が未対応）。
- internal/dispatch: テストが `/usr/bin/sqlite3` を前提にしている。
- internal/tui の実 PTY 系（設定の割り当て・既定 shell・scrollback editor）、internal/server の job dot、tests/e2e の TestRawLFToPane など: 原因未調査。

## 0. 作り方の決まり

- herdr のソースコードを読まない・移植しない（AGENTS.md）。Linux での挙動は SPEC と herdr の利用者向け文書で決める。
- 原因ごとに「ikesu 本体が Linux に対応していない」のか「テストが macOS の環境を前提にしている」のかを分け、本体の不足は本体を直す（例: 古い git でも動く書き方、Linux での音の再生）。テストを Linux で飛ばすだけで済ませない。どうしても Linux で成り立たない確かめ（macOS の通知センターなど）は、SPEC に Linux の扱いが書かれていればそれに従い、無ければ親へ報告して止まる。
- ubuntu-3090 の環境（git の版、入っているコマンド）は変えない。ikesu の側を合わせる。
- macOS での挙動を変えない（macOS 側の確かめは mini が戻ったら親が行う。それまでは macOS の分岐を変える変更を最小にし、変えた所を RESULT.md に列挙する）。

## 1. やること

1. 上の全失敗の原因を調べ、表にする（テスト名・原因・本体かテストか・直し方）。
2. 直す。Linux での音の再生は、SPEC と herdr の利用者向け文書（音・通知の節）に書かれた挙動に合わせる（使う再生コマンドの候補と、無いときの扱い）。
3. ubuntu-3090 で `go test ./...` と `go test -race ./...` が全部通ること。tests/e2e も含む。

## 2. 守ること

- 変えてはいけないもの（変えたいときは実装前に親へ報告して止まる）: go.mod（依存を足さない）、既存の API・CLI・設定・保存形式。1ファイル800行まで。計画1章に無い機能を作らない。
- 店長の本物の設定・state・稼働中の ikesu と herdr に触らない。音をスピーカーで鳴らさない（再生コマンドの呼び出しは差し替えで確かめる）。

## 3. 進め方と確認

- 区切り（原因の表 → 本体の直し → テストの直し）ごとに ubuntu-3090 で gofmt・go build・go vet・go test を通して積み上げコミット。最後に race。メインの Mac でも `go build ./...` と `go vet ./...` が通ること（darwin でコンパイルが壊れていないこと）。
- `.artifacts/linux/RESULT.md` に原因の表と、直した後の全テスト結果、macOS の分岐を変えた所の一覧を書く。確かめていないものは未達と書く。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
