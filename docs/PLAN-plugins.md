# ikesu プラグインと新しいエージェントの段階の計画（SPEC G10 の UP-AUTOMATION・UP-AGENTS と G9 の local overlay）

作成: 2026-10-09（親 Claude）。店長の方針「herdr の SPEC.md のとおりに全部実装する。優先順位は無い」と、リポジトリの `AGENTS.md`「仕様から実装する。移植しない」に基づく。同じ時間帯に、別の実装役が保存マシンの段階（`feat/machines`）を進めている。

## 0. 作り方の決まり（最優先）

- herdr のソースコード（`~/src/github.com/kazuph/herdr` の `src`・`tests`・`workers` などの実装とテスト）を読まない。
- 仕様の正は次の3つ。`herdr` → `ikesu`、`HERDR_*` → `IKESU_*`、`herdr-plugin.toml` → `ikesu-plugin.toml` と読み替える。
  - `~/src/github.com/kazuph/herdr/SPEC.md` の下記の節の文章
  - herdr の利用者向け説明 `~/src/github.com/kazuph/herdr/docs/next/website/src/content/docs/` の `plugins.mdx`・`cli-reference.mdx`・`socket-api.mdx`・`configuration.mdx`・`agents.mdx`（英語版を正とする）
  - このリポジトリの `docs/ref/`（API スキーマと CLI help）
- 文章で決まらない細部は、mini で herdr の配布用ビルドを別に動かして観察する（`~/bench-terrace.noindex/work/herdr-target/release/herdr`。`plugin` コマンドを持っている）。観察には隔離した状態フォルダと設定フォルダを使い、店長が使っている稼働中の herdr と、その設定・プラグインには触らない。それでも決まらなければ親へ質問する。
- 各節の「受け入れ条件」と「デグレ判定」を、そのまま ikesu の受け入れ条件として扱う。仕様の文どうしが食い違うときは実物を正とし、仕様と実物が食い違うときは仕様を正として RESULT.md の発見事項に書く。

## 1. 作る節

| SPEC の節 | 作る範囲 |
|---|---|
| G10「UP-AUTOMATION 取込packet」P1〜P4 | プラグインの土台一式: `ikesu plugin install/uninstall/link [--disabled]/unlink/list [--plugin] [--json]/config-dir/enable/disable/action list/action invoke/log list/pane open/pane focus/pane close` と API `plugin.*`（`docs/ref/herdr-api-schema.json` の名前と項目）。マニフェスト `ikesu-plugin.toml` の検証（必須項目・id の文字・`min_ikesu_version`・platforms）、`[[build]]`・`[[actions]]`・`[[events]]`・`[[panes]]`（配置 overlay/popup/split/tab/zoomed、width/height）・`[[link_handlers]]`、`[[keys.command]] type = "plugin_action"`、実行時の環境（`IKESU_BIN_PATH`・`IKESU_PLUGIN_CONTEXT_JSON` など説明書に書かれたもの）、プラグインごとの設定・状態フォルダ、ログ。サーバー停止中の `plugin link`（P1）、プラグインの状態を session をまたいで共有し mailbox・job の session 分離は壊さない（P2）、`plugin list --json` と保存形式（P3）、agent views の表示差し替え（P4）。P4 のうち startup hooks・integration installer・外部設定の書き換えは作らない（SPEC の店長確定政策 a） |
| G10「UP-AUTOMATION」P5 | 自動化 CLI（wait の条件・event・skill の説明）のうち、ikesu の今の CLI に無い「足すだけ」の差分。既存のコマンド名・help の文言は変えない（政策 b） |
| G9「local overlay pane command」と G1 から移した overlay の条件 | overlay のペインは通常ペインと別の状態で描画・フォーカスし、通常のレイアウト操作と session の保存に混ぜない。開閉で通常ペインの順序・分割比率・保存内容が変わらない。開いている間だけ移動・入力が overlay に向く。G1 の条件「active tab 内に overlay と対象の両方のペインがあるときだけ、対象を同じ矩形で置き換え、ほかのペインの矩形・順序・分割比率を変えない」もここで確かめる |
| G10「UP-AGENTS」 | 全部（muse・qwen の検出、qwen の承認待ちを Blocked 通知、grok・agy・qwen・letta の再開コマンド、`default:` 付きの letta の会話は復元しない、起動形状からの session id の読み取り、メニューの6項目の順） |
| G10「比較基準と差分範囲」「差分台帳」「段階的に取り込む実装契約」 | herdr の版・配布・Cargo・Windows の話は ikesu の機能ではないので、各受け入れ条件に「対象外（理由）」か「ikesu での確かめ方」を RESULT.md に1行ずつ書く。ikesu に当てはまるもの（ID の形 `pN`/`sN`/`sN:tN` が変わらない、自動更新が復活しない、mailbox・job の分離、外部設定を書き換えない）はテストで確かめる |

### この段階で作らないもの

- G10「UP-AUTOMATION」P5b（マーケットプレイスの索引を集める側の仕組み）: herdr.dev のサーバー側の仕組みで、ikesu の機能ではない。`plugin install owner/repo` は作る。
- G10「UP-MULTICLIENT」と G11: 保存マシンの段階と、その後の複数画面の段階で作る。
- Windows 対応（政策 c）。

## 2. 守ること

- これまでの段階と同じ: 状態は純粋なデータ、API と画面は同じコマンドで状態を変える、1ファイル 800 行まで、推測で埋めない、未対応は未対応としてエラーを返し未達に書く（似た値で代用しない）、計画1章に無い機能は作らない、依存ライブラリを足さない（TOML は今ある BurntSushi/toml を使う）。
- プラグインの確かめには、一時フォルダに作った自作のプラグインを使う。`plugin install` の確かめは、PATH の先頭に置いた偽の `git` と一時フォルダの git リポジトリで行い、ほかの人が書いたプラグインのコードを実行しない。
- 店長の本物のリポジトリ・シェル設定・`~/.claude`・`~/.codex`・クリップボード・稼働中の herdr に触らない。
- 性能: 区切りごとに mini で `go test -tags perf ./tests/perf -run TestRelative`（基準は main 9347b07 のビルド）を全項目合格にする。プラグインが無いときに起動・描画・メモリが増えないこと。

## 3. 進め方と確認

- 区切りは自分で決めてよい（目安: マニフェストと保存・link/list → action・events・ログ → pane の配置と overlay → install・keybind・link handler → agent views と自動化 CLI の差分 → 新しいエージェント → 仕上げ）。区切りごとに mini で gofmt・go build・go vet・go test（最後は -race も）を通してから積み上げコミット。
- mini の同期先は `src/plugins`。メインの Mac では go build と軽い go test だけ。
- `.artifacts/plugins/RESULT.md` に、SPEC の節ごと・受け入れ条件ごとに「確かめたテスト名、または保存した画面・出力」の表を書く。確かめていない条件は未達と書く。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
