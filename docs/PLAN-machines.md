# gtx 保存マシンの段階の計画（SPEC G11 の P1・P2・P5 と G9「herdr run --machine」）

作成: 2026-10-09（親 Claude）。店長の方針「herdr の SPEC.md のとおりに全部実装する。優先順位は無い」と、リポジトリの `AGENTS.md`「仕様から実装する。移植しない」に基づく。同じ時間帯に、別の実装役がプラグインの段階（`feat/plugins`）を進めている。

G11 のうち、サイドバーのマシン一覧（P3）・再接続（P4）・endpoint client の節は、複数画面（G10「UP-MULTICLIENT」）の土台の上に作るため、この段階の次に「複数画面とマシン横断表示」の段階としてまとめて作る。

## 0. 作り方の決まり（最優先）

- herdr のソースコード（`~/src/github.com/kazuph/herdr` の `src`・`tests` などの実装とテスト）を読まない。
- 仕様の正は、`~/src/github.com/kazuph/herdr/SPEC.md` の下記の節の文章、herdr の利用者向け説明 `~/src/github.com/kazuph/herdr/docs/next/website/src/content/docs/`（`persistence-remote.mdx`・`cli-reference.mdx`、英語版）、このリポジトリの `docs/ref/`。`herdr` → `gtx`、`HERDR_*` → `GTX_*` と読み替える。
- 文章で決まらない細部は、mini で herdr の配布用ビルドを別に動かして観察する（`~/bench-terrace.noindex/work/herdr-target/release/herdr machine --help` などで出力の形を見る）。観察には隔離した状態フォルダを使い、店長が使っている稼働中の herdr とその保存マシン一覧には触らない。それでも決まらなければ親へ質問する。
- 各節の「受け入れ条件」と「デグレ判定」を、そのまま gtx の受け入れ条件として扱う。

## 1. 作る節

| SPEC の節 | 作る範囲 |
|---|---|
| G11「machine保存CRUD（P1）」 | 全部。`gtx machine list [--json]/add <ssh-target> --label <label> [--remote-session <name>]/rename <label-or-id> --label <label>/remove/enable/disable`、SSH の到達確認（`ssh -o BatchMode=yes -o ConnectTimeout=15 -- <target> true`）が成功したときだけ `machines.json` へ原子的に保存、文言・終了コード、壊れた一覧は空扱い |
| G11「--machine CLI転送（P2）」 | 全部。`gtx --machine <label-or-id> <agent\|pane\|workspace\|worktree> ...` を規定の ssh の引数で転送し、remote の終了コードをそのまま返す。失敗しても手元で実行しない。`GTX_MACHINE_REMOTE_BINARY`（既定 `gtx`）、拒否する組み合わせ、`--help` は手元 |
| G11「machine管理メニュー（P5）」 | 下部の `[menu]` に `add machine...` と `remove machine...` を出す部分と、add の入力（SSH target → 表示名 → remote session、入力を止めずに到達確認してから保存）と remove の確認（`remove machine?`、手元の一覧だけ消す、古い一覧からの削除を拒否）。マシン見出しの右クリックは、見出しを作る次の段階で作る |
| G9「herdr run --machine」 | 全部（ログインシェルの中での実行、2段の引用、作業フォルダの写し方、`--cwd`、`ssh -n`／`ssh -t`、`--sync` の mkdir → rsync → コマンドの順と ignore されたものを送らず消さない、cancel、全体の `--machine` 転送に奪われない） |

### この段階で作らないもの

- G11 P3・P4・「endpoint clientでのfork操作と入力の保持」、G10「UP-MULTICLIENT」、G9 の複数画面の条件: 次の段階。
- Windows の remote、session 削除の正確名（SPEC で G11 の範囲外）。machine 宛の `inbox`（SPEC で対象外）。

## 2. 守ること

- これまでの段階と同じ: 状態は純粋なデータ、API と画面は同じコマンドで状態を変える、1ファイル 800 行まで、推測で埋めない、未対応は未対応としてエラーを返し未達に書く（似た値で代用しない）、計画1章に無い機能は作らない、依存ライブラリを足さない。
- ssh の引数を組み立てる処理は純粋な関数にして、引数の形をテストで固定する。
- 本物の SSH の確かめは mini の中で行う: mini の sshd へ `localhost` で接続する。mini の `~/.ssh/known_hosts` や `~/.ssh/config` は書き換えず、PATH の先頭に置いた `ssh` の包み（一時フォルダの known_hosts を使う `-o UserKnownHostsFile=<一時> -o StrictHostKeyChecking=accept-new` を足して本物の ssh を呼ぶだけのもの）で行う。remote 側の gtx は mini のビルドを `GTX_MACHINE_REMOTE_BINARY` で指す。到達できない target の確かめは、存在しないホスト名か閉じたポートで行う。
- 店長の本物のリポジトリ・シェル設定・SSH 設定・クリップボード・稼働中の herdr に触らない。
- 性能: 区切りごとに mini で `go test -tags perf ./tests/perf -run TestRelative`（基準は main 9347b07 のビルド）を全項目合格にする。

## 3. 進め方と確認

- 区切りは自分で決めてよい（目安: 保存と一覧 → 到達確認と rename/remove/enable/disable → `--machine` 転送 → `run --machine` と `--sync` → メニュー → 仕上げ）。区切りごとに mini で gofmt・go build・go vet・go test（最後は -race も）を通してから積み上げコミット。
- mini の同期先は `src/machines`。メインの Mac では go build と軽い go test だけ。
- `.artifacts/machines/RESULT.md` に、SPEC の節ごと・受け入れ条件ごとに「確かめたテスト名、または保存した画面・出力」の表を書く。確かめていない条件は未達と書く。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
