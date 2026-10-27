# herdr のセッションを ikesu に取り込む段階の計画

作成: 2026-10-14（親 Claude）。

## 依頼

店長（2026-10-14）: 「今このツールを使うための最大の障壁が、ikesu を起動しても herdr のセッションが復元されないこと。これだといつまで経っても移行できないので、ikesu でも herdr のリストアが動くようにしてほしい。実際に使って確認したい。」
店長の決定: **コマンドで取り込む**。取り込む前に何が戻るかを一覧で見せる。herdr が動いている間は agent の会話の再開はせず、画面の形と作業フォルダだけを戻す（同じ Claude の会話が2か所で動いて壊れるのを防ぐ）。

## 0. 作り方の決まり

- herdr のソースコードを読まない・移植しない（AGENTS.md）。herdr が保存したファイル（`~/.config/herdr/session.json` 形式の版 5、`agent-session-snapshot.json`、`agent-session-ledger.json` など）の**中身の形**を観察して読み取るのはよい（データの形を合わせるのは移植ではない）。利用者向けの説明 `~/src/github.com/kazuph/herdr/docs/next/website/src/content/docs/session-state.mdx` も読んでよい。
- 店長の本物の herdr のファイルは**読むだけ**。書き換え・移動・削除をしない。確かめは、それらを一時フォルダに写したものと、一時の HOME・名前付き session の ikesu で行う。店長の本物の ikesu の state にも書かない。
- ikesu にすでにある「保存したセッションの復元」と「agent の会話の再開（native restore）」の仕組みを使い、二重の実装を作らない。

## 1. やること

1. コマンド `ikesu session import-herdr`（名前と引数は既存の `ikesu session` の流儀に合わせて決め、help に載せる）。既定では `~/.config/herdr/` を読む。読み込み元を指定する引数も付ける（確かめや別マシンのため）。
2. 取り込む前に一覧を出す: スペース・タブ・pane の数と名前、各 pane の作業フォルダ、agent の会話を再開する pane（agent の種類と会話 ID）、戻せないもの（その理由）。`--dry-run` で一覧だけ出して何もしない。実際に取り込むときは確認を求める（`--yes` で省略）。
3. 戻すもの: スペース（名前・section）・タブ（名前）・画面の分割の形・pane の作業フォルダ・フォーカス・sidebar の幅。agent の pane は、記録された正確な会話 ID で再開する（`--last`・最新の会話を推測して再開することは絶対にしない。ID が分からない pane は shell として戻し、一覧に「会話 ID 不明」と出す）。
4. herdr が動いているとき（herdr の server に接続できる、またはロックが取られている）は、agent の会話を再開しない（shell として戻す）。一覧にその旨を出す。
5. ikesu の server がすでに動いていて pane があるときの扱い: 既存の ikesu のセッションを消さずに、取り込んだスペースを追加する（ID は ikesu の採番で振り直し、ぶつからない）。
6. 読めない形式（版 5 以外、壊れた JSON、未対応の項目）は、何も変えずにエラーで止まる（推測で埋めない）。

## 2. 守ること

- 変えてはいけないもの: go.mod、既存の保存形式・API・CLI、見張り、判断待ち。1ファイル800行まで。計画1章に無い機能を作らない。
- 性能の普段の動きを悪くしない。ikesu のビルドとテストはこの Mac でよい（mini 不通の間）。Rust の herdr はこの Mac でビルドしない。
- スクリプトの先頭で PATH を整えてから rm を使う。

## 3. 進め方と確認

- 店長の本物の herdr の session.json と agent の記録を一時フォルダに写し、そこから `--dry-run` の一覧と、一時 HOME の ikesu への取り込みを実際に行い、スペース・タブ・分割・作業フォルダが同じになることを画面の保存で示す。agent の会話の再開は、偽の claude（引数を記録するだけの実行ファイル）で、記録された会話 ID がそのまま渡ることを示す（本物の Claude Code は起動しない）。herdr が動いている場合に再開しないことも示す。
- 区切りごとに gofmt・go build・go vet・go test（この Mac）。最後に race。ubuntu-3090 でも gofmt/build/vet/test/race。積み上げコミット。
- `.artifacts/import-herdr/RESULT.md` に項目ごとの表。確かめていないものは未達。push・amend・reset しない。`.artifacts/` は add しない。
