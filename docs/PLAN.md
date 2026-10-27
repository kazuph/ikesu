# ikesu 実装計画（MoonBit 版 moonterrace と同じ条件で Go で作る）

実装担当（Devin SWE-2 High）への指示書です。上から順に読んで、この順で実装してください。

## 0. 何を作るか・なぜ

店長は、herdr（Rust 製）のビルドが遅いので、別の言語で同等のものを作って比べています。
1つ目は MoonBit で作りました（`~/src/github.com/kazuph/moonterrace`、コマンド `mt`）。今回はそれと**同じ機能・同じ条件**で
**Go** 版を作ります。名前は ikesu、コマンド名は **`ikesu`** です（`gt` は店長の zsh で `git tag` の別名なので使わない）。
作ったあと、親（Claude）がビルド時間・動作速度・CPU・メモリを herdr と moonterrace と比べます。

## 1. 機能の仕様 = MoonBit 版と同じ（変えない）

仕様書は `docs/spec/` にコピーしてあります。**ここに書かれた機能・キー操作・マウス操作・画面の見た目・確認ダイアログ・
検出の意味・状態遷移の数値は、すべてそのまま ikesu の仕様**です。

| ファイル | 内容 |
|---|---|
| `docs/spec/PLAN.md` | 第1段階の本体: マルチペイン、サイドバー、エージェント進捗管理（1章の設計方針、5章の各機能、5.8 のキー、7章の確認方法） |
| `docs/spec/PLAN-mouse.md` | マウスだけで全操作（右クリックメニュー、仕切り線ドラッグ、`[menu]` `[new]` `«` `»`、ホイール、ドラッグでコピー、ステータス行のボタン） |
| `docs/spec/PLAN-fix-1.md` | MoonBit 版で店長が見つけた不具合6件。**ikesu では最初から起きないように作る**（下の 4章） |

仕様書の中の MoonBit 固有の記述（`moon` コマンド、`moon.pkg`、MoonBit の正規表現の変換、`manifests_bundled.mbt` の生成など）は、
この文書の 2章の対応表に置き換えて読んでください。

**moonterrace のソースコード（`src/` 以下）は読まない・写さない**。比較のため、Go 版は仕様と herdr fork（`~/src/github.com/kazuph/herdr`、読むだけ）を
参考に独立して書きます。

## 2. MoonBit 版との対応（使う部品。親が試作で動作確認済み）

| 役割 | MoonBit 版 | ikesu（Go） |
|---|---|---|
| 言語・ツール | moon 0.1.20260920 | Go 1.27（`go version go1.27.0 darwin/arm64`）。**cgo は使わない**（`CGO_ENABLED=0` でビルドできること） |
| 端末エンジン | `tonyfettes/ghostty`（Ghostty の MoonBit 移植） | `github.com/charmbracelet/x/vt`（純 Go の VT エミュレータ）。`vt.NewEmulator(w,h)`、`Write` で PTY 出力を流し込む。エミュレータからの応答（デバイス属性の問い合わせなど）は `Read` で取り出して PTY に書き戻す |
| PTY | `moonbit-community/pty` | `github.com/creack/pty`（`pty.StartWithSize`、`pty.Setsize`） |
| 外側の端末（raw mode・サイズ・入力の解読） | `moonbit-community/tty` | `golang.org/x/term`（raw mode・サイズ）と `github.com/charmbracelet/ultraviolet`（キー・マウス・貼り付けの解読。`EventDecoder` など）。ultraviolet でできないことがあれば理由を書いて自前で解読してよい |
| 正規表現 | `moonbitlang/regexp`（変換層が必要だった） | 標準の `regexp`。herdr の検出ルールの `\x{2800}` `(?i)` `(?m)` はそのまま使える（親が確認済み）。**変換層は作らない** |
| TOML | `moonbit-community/toml` | `github.com/BurntSushi/toml` |
| 検出ルールの同梱 | 生成スクリプトで MoonBit の文字列に埋め込み | `//go:embed manifests/*.toml`（`internal/detect/manifests/` に herdr fork の22ファイルをコピー済み） |
| 前面プロセス | C スタブ（`proc_pidinfo` など） | `golang.org/x/sys/unix`: PTY の master で `unix.IoctlGetInt(fd, unix.TIOCGPGRP)`、名前は macOS `unix.SysctlKinfoProc("kern.proc.pid", pid)` の `P_comm`、argv0 は `kern.procargs2`。Linux は `/proc/<pid>/stat`・`comm`・`cmdline`。OS 差は `internal/platform/` の `_darwin.go` / `_linux.go` だけに置く |
| 終了時の入力の捨て方（不具合6番） | C スタブで tcflush | `unix.IoctlSetInt(fd, unix.TIOCFLUSH, unix.TCIFLUSH)`（macOS）/ `unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIFLUSH)`（Linux） |
| 整形・型チェック・lint・テスト | `moon fmt` / `moon check` / `moon test` | `gofmt -l .`（出力が空）/ `go build ./...` / `go vet ./...` / `go test ./...` |

依存の版は `go get <module>@latest` で取り、`go.mod` と `go.sum` をコミットする。上の表にない依存は追加しない。

## 3. パッケージ構成（MoonBit 版と同じ分け方）

```
ikesu/
  go.mod
  cmd/ikesu/            main（引数解析と起動だけ。`ikesu`、`ikesu --version` → `ikesu 0.1.0`、`ikesu detect <file>`）
  internal/model/     純粋な状態（AppState, Workspace, Command, Effect）
  internal/layout/    純粋な BSP レイアウト（分割・閉じる・方向移動・仕切り線の当たり判定とドラッグ）
  internal/detect/    純粋な検出（エージェント識別・ルール読み込み・領域・ゲート判定）＋ manifests/
  internal/status/    純粋な状態機械（作業中→待機の確認待ち・未確認・注目度・集計）
  internal/platform/  前面プロセス・入力の捨て方（OS 依存はここだけ）
  internal/runtime/   PTY ＋ vt.Emulator ＋ 検出ループ（ペインの実体）
  internal/render/    純粋な描画（状態＋各ペインの画面 → 外側の端末へ出すバイト列。差分描画と同期出力）
  internal/tui/       外側の端末・入力・マウス・メニュー・確認ダイアログ・描画ループ
  tests/e2e/          実 PTY で ikesu を起動して画面を vt.Emulator で読む結合テスト
```

依存の向きは spec/PLAN.md 4章の図と同じ（逆向きの import は禁止）。
`model`・`layout`・`detect`・`status`・`render` は PTY・ファイル・OS の API を使わない（`detect` が `embed` を使うのはよい）。

Go の書き方:
- ペインごとの読み取りは goroutine でよいが、**状態（AppState・各ペインの状態）を書き換えるのは1つの goroutine（イベントループ）だけ**にする。
  他の goroutine はチャネルでイベントを送る（MoonBit 版の「状態は1か所で変更」と同じ考え方。データ競合を作らない）。
- `go test -race ./...` も通ること。
- 1ファイル 800 行を超えそうなら分ける。

## 4. MoonBit 版の不具合6件（最初から起きないように作る）

`docs/spec/PLAN-fix-1.md` の6件。Go 版でも同じ原因が起こりうるので、最初から次のように作る:

1. **マウス信号の途中切れ**: 入力の解読ライブラリが途中で切れたマウス信号（`ESC [ < 35 ; 60 ; 8` と `M`）や、ESC だけ先に届いた信号を
   文字として返しても、それをペインへ送らない。つなげるならつなぎ直してクリックとして処理する。解読できなかった入力をそのままペインへ送らない。
2. **ゴミが飛ぶ**: 描画は `ESC[?2026h`（同期出力開始）→ カーソルを隠す → 前フレームとの差分だけ書く → カーソル位置と表示を設定 → `ESC[?2026l`。
3. **IME が右下**: フォーカス中ペインのカーソルが非表示でも、そのカーソル位置へ外側のカーソルを置いてから隠す。同期出力の外でももう一度置く。
4. **クリックしたペインにフォーカスが移らない**: 1番と同じ原因。1番を守ったうえで、spec/PLAN-fix-1.md の 4番の確認（claude と codex を並べて交互にクリック10回以上）を行う。
5. **最後のペインを閉じられない**: 最後の1枚を閉じる操作は「ワークスペースを閉じる」確認、最後のワークスペースなら「終了」確認を出す。
6. **終了直後のゴミ**: 終了時に、マウスの受け取りを止める → 入力に残ったバイトを捨てる（2章の表の方法）→ raw mode と代替画面を戻す。

それぞれにテストを書く（spec/PLAN-fix-1.md に書かれたテストと同じ内容）。

## 5. マイルストーン（この順。各マイルストーンの最後に 2章の4つのコマンドが全部通ってからコミット）

| 順 | 内容 |
|---|---|
| 1 | 骨組み、`ikesu --version` |
| 2 | layout（spec/PLAN.md 5.1 のテスト＋仕切り線の当たり判定・ドラッグ） |
| 3 | model（spec/PLAN.md 5.2） |
| 4 | platform ＋ runtime（1ペインで zsh・vim・色が普通に使える） |
| 5 | render ＋ tui（分割・サイドバー・ワークスペース・キー操作・同期差分描画・IME アンカー） |
| 6 | detect（22ルール・全領域・判定。フィクスチャは実物から採取。個人情報は `user`・`~/project` に置き換え） |
| 7 | status ＋ サイドバーのエージェント表示（実物の claude・codex・devin で確認） |
| 8 | マウス操作（spec/PLAN-mouse.md の全部） |
| 9 | 不具合6件の作り込みとテスト（4章） |
| 10 | e2e テスト（spec/PLAN.md 5章の e2e ＋ spec/PLAN-mouse.md 4章の e2e ＋ マウス信号を2回に分けても文字が出ないこと） |

コミットは小さく積み上げる（amend・reset・push しない）。メッセージは英語の小文字 conventional commits、末尾に
`Co-Authored-By: Devin <noreply@cognition.ai>`。

## 6. 動作確認（本人が必ず行う）

spec/PLAN.md 7章、spec/PLAN-mouse.md 5章、spec/PLAN-fix-1.md の各「実物確認」を、`ikesu` で行う。
ビルドは `go build -o ikesu ./cmd/ikesu`（リポジトリ直下に `ikesu` ができる。`.gitignore` 済み）。
証跡は `.artifacts/<マイルストーン名>/` に保存（コミットしない）。
herdr の検証用ペインは確認後に閉じる。claude・codex・devin への入力は確認用の短いものだけ。

## 7. やってはいけないこと

- spec にない機能を作らない（見つけた問題は `.artifacts/RESULT.md` の発見事項に書くだけ）。
- moonterrace の `src/` を読まない・写さない。herdr fork・稼働中の herdr サーバー・ほかの人のペインを変更しない。
- cgo を使わない。2章の表にない依存を足さない。
- テストを消したり緩めたりして通さない。
- `sleep` の繰り返しで待つループを書かない（タイマー・チャネルを使う）。
- 店長のクリップボードを読まない。
