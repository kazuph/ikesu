# 書き直しの仕様（クリーンルーム、2026-10-08 親 Claude）

背景: `docs/AUDIT-port-2026-10-08.md`（ブランチ `docs/port-audit`）の監査で、main に herdr（AGPL-3.0）の実装を移し替えた箇所が見つかった。店長の決定で、その箇所を**仕様だけから**書き直す。
このブランチでは、親が移植部分を削除してある（ファイル `internal/detect/identify.go`・`internal/detect/region.go`・`internal/dispatch/store.go` と、下に挙げる関数）。そのため今はビルドが通らない。

**実装役の決まり（リポジトリの `AGENTS.md` と同じ）**
- herdr のソースを読まない。削除前の ikesu の実装も読まない（`git log -p`・`git show`・`git diff` で消えた行を見ない。main や他のブランチ・worktree の同じファイルを開かない）。
- 根拠はこの文書、`docs/ref/`（API スキーマ・CLI help・DB スキーマ）、herdr の `SPEC.md` の文章、残っているテストだけ。決まらないことは推測で埋めずに親へ質問する。
- 下に書く**名前と形**（型・関数のシグネチャ・定数）は、呼び出し側のコードが使っているので、この形で作る。中身の書き方は自由。

---

## A. `internal/detect`: ペインで動いている agent の判定（ファイル新設）

### 形

```go
type JobProcess struct { PID int; Name string; Argv []string; Cmdline string }
type Job struct { ProcessGroupID int; Processes []JobProcess }
// IdentifyJob は agent の正式な id（例 "codex"、無ければ ""）と、表示用の名前を返す。
func IdentifyJob(job Job) (agentID, name string)
```

### 名前から agent を引く表

大文字小文字を区別しない。前後の空白を除き、末尾の `.exe` `.cmd` `.bat` `.ps1` `.js` のどれか1つを外してから引く。左が受け付ける名前、右が正式な id。

| 受け付ける名前 | 正式な id |
|---|---|
| pi | pi |
| claude, claude-code | claude |
| codex | codex |
| gemini | gemini |
| cursor, cursor-agent | cursor |
| devin, devin-cli, "devin cli" | devin |
| agy, antigravity, antigravity-cli | agy |
| cline | cline |
| omp | omp |
| mastracode, mastra-code, "mastra code" | mastracode |
| opencode, open-code | opencode |
| copilot, github-copilot, ghcs | copilot |
| kimi, kimi-code, "kimi code" | kimi |
| kiro, kiro-cli | kiro |
| droid | droid |
| amp, amp-local | amp |
| grok, grok-build | grok |
| hermes, hermes-agent | hermes |
| kilo, kilo-code, "kilo code" | kilo |
| qodercli, qoderclicn, qoder, qodercn | qodercli |
| maki | maki |
| muse, muse-bin, muse-code, muse-cli | muse |
| qwen, qwen-code, "qwen code" | qwen |
| letta, letta-code, "letta code" | letta |

### 判定の手順

1. **プロセスごとの候補名**: そのプロセスの実行名（`Argv[0]` があればそれ、無ければ `Name`）を使う。
   - 実行名（パスなら最後の部分、上の拡張子を外したもの）が **汎用の実行環境** `sh bash zsh fish tmux node bun python python3 cmd powershell pwsh` のどれかなら、下の「包まれた agent の見つけ方」で agent が見つかればその正式 id を候補名にする。
   - 実行名そのものが表で引ければ、実行名をそのまま候補名にする。
   - それ以外は、`Argv[0]` のパス（無ければ `Cmdline` の最初の空白区切りの語）から「パスの語から agent を引く」で見つかれば、その正式 id を候補名にする。
   - どれでもなければ実行名を候補名にする。
2. **グループの先頭を優先**: `PID == ProcessGroupID` のプロセスの候補名が表で引ければ、それで決まり（id と候補名を返す）。
3. **残りから選ぶ**: 候補名が表で引けるプロセスのうち、点数の一番高いものを選ぶ（同点なら先に出てきたもの）。点数は、候補名が `Name` と（大文字小文字を無視して）違う＝包みから見つけた: 3、汎用の実行環境ではない: 2、それ以外: 1。
4. どれも引けなければ id は ""、名前はグループの先頭（無ければ最初のプロセス）の `Name`。プロセスが0個なら両方 ""。

### 包まれた agent の見つけ方（実行環境ごと、`Argv[1:]` を前から見る）

- **node・bun**: 評価用の旗 `-e --eval -p --print`（`-eX` のように値が続く形と `--eval=…` の形も含む）が出たら「見つからない」。
- **python・python3**: 評価用 `-c`、モジュール用 `-m`（同じく続く形も含む）が出たら「見つからない」。
- **sh・bash・zsh・fish**: `-c`（続く形を含む）が出たら「見つからない」。
- 上の3種類の共通: `--` の次の語を「パスの語から agent を引く」で調べる。`-` で始まる語は旗として飛ばし、そのうち値を取る旗（`-r --require --loader --import --experimental-loader --inspect-port -W -X -S -L -o`）は次の語も飛ばす。最初の旗でない語を「パスの語から agent を引く」で調べる。
- **cmd**: 引数を、前後の `"` を外して小文字にして見る。`/c` か `/k` の次の語を「コマンド文から agent を引く」で調べる。`/d /s /q /a /u /e:on /e:off /f:on /f:off /v:on /v:off` は飛ばす。
- **powershell・pwsh**: 同じく小文字で見る。`-file -f /file` の次の語は「パスの語から」、`-command -c /command /c` の次の語は「コマンド文から」。`-encodedcommand -enc /encodedcommand /enc` が出たら「見つからない」。`-configurationname -executionpolicy -outputformat -psconsolefile -version -windowstyle -workingdirectory` は次の語も飛ばす。そのほかの `-`・`/` で始まる語は飛ばし、それ以外の語は「パスの語から」で調べる。
- **tmux とそのほか**: 見つからない。

**コマンド文から agent を引く**: 文を前から語に分ける（`"` か `'` で始まる語はその引用符の対まで、それ以外は空白まで）。`&`・`.`・`call`（大文字小文字は無視）は飛ばし、最初の残りの語を「パスの語から」で調べる。

**パスの語から agent を引く**: 前後の `"` `'` を外す。空か `-` で始まれば見つからない。次の順で調べる: (1) パスの最後の部分を表で引く、(2) パスの区切り（`/` と `\`）で分けた並びに `node_modules/@earendil-works/pi-coding-agent/dist/cli`（各部分は表と同じ正規化で比べる）が続いて現れれば pi、(3) 区切りを含むパスならシンボリックリンクを実体までたどり、その最後の部分を表で引く。

### 確かめる例（残っている `identify_test.go` の表に加えて、次も満たすこと）

| プロセス | 結果 |
|---|---|
| `node /usr/lib/node_modules/@earendil-works/pi-coding-agent/dist/cli.js` | pi |
| `node /x/node_modules/@earendil-works/pi-coding-agent/dist/other.js` | 見つからない（実行名 node） |
| `cmd /c "C:\tools\codex.cmd" --help` | codex |
| `pwsh -NoProfile -File C:\bin\claude.ps1` | claude |
| `pwsh -EncodedCommand AAAA` | 見つからない |
| `python3 codex.py` | codex |
| `python3 -c "import codex"` | 見つからない |
| `bash -c codex` | 見つからない |
| `bun x/omp.js` | omp |

---

## B. `internal/detect`: 検出ルールが見る範囲（ファイル新設）

検出ルール（`internal/detect/manifests/*.toml`、herdr と同じデータ。そのまま使う）の各ルールは `region` で画面のどこを見るかを指定する。`manifest.go` は次の2つを呼ぶので、この形で作る。

```go
func knownRegion(name string) bool          // 下の表の名前（前後の空白は無視）なら true
func regionText(in Input, name string) string // 範囲の文字列
```

画面 `in.Screen` を改行で行に分けて考える（最後の改行の後ろは行に数えない）。範囲の結果は「ある行から最後まで」「最初からある行の手前まで」のように、元の文字列の連続した一部を返す（行の間の改行もそのまま含む）。

**用語**
- **横罫線の行**: 前後の空白を除いて空でなく、先頭から `─`（U+2500）が1個以上続き、その後ろ（空白を除く）が空であるか、`─` が3個以上続いている行。
- **入力欄の上端**: 下から数えて2本目の横罫線の行（横罫線が2本未満なら「無い」）。
- **Codex の入力行**: 行がちょうど `›`、または `› ` で始まる行（空白を除かずに比べる）。
- **Codex の区切り行**: `•` `■` `✗` `✓` のどれかで始まる行。
- **現在の Codex 入力行**: 最後の Codex の入力行。ただし、その後ろに Codex の区切り行があるなら「無い」。

| 範囲の名前 | 中身 |
|---|---|
| `osc_title` / `osc_progress` | 画面ではなく `in.OSCTitle` / `in.OSCProgress` |
| `whole_recent` | 画面全体 |
| `after_last_prompt_marker` | 最後の Codex の入力行の**次の行から**最後まで。入力行が無ければ画面全体 |
| `before_current_prompt_marker` | 最初から、現在の Codex 入力行の**手前まで**。無ければ画面全体 |
| `whole_recent_without_current_prompt_marker` | 現在の Codex 入力行があれば空、無ければ画面全体 |
| `current_prompt_block_marker` | 現在の Codex 入力行より上で一番近い Codex の区切り行（その1行だけ）。無ければ空 |
| `after_current_prompt_block_marker` | 上の区切り行**から**最後まで。無ければ空 |
| `prompt_box_body` | 入力欄の上端の次の行から、その下で最初の横罫線の行の手前まで（下に横罫線が無ければ最後まで）。上端が無ければ空 |
| `above_prompt_box` | 最初から入力欄の上端の手前まで。上端が無ければ画面全体 |
| `last_non_empty_above_prompt_box` | `above_prompt_box` の中で、空白だけでない最後の行（1行）。無ければ空 |
| `after_last_horizontal_rule` | 最後の横罫線の行の次の行から最後まで。横罫線が無ければ画面全体 |
| `bottom_lines(N)` | 最後の N 行（N は 0 以上の整数。行数より多ければ全体） |
| `bottom_non_empty_lines(N)` | 下から数えて N 個目の「空白だけでない行」から最後まで（途中の空行も含む）。空白だけでない行が1つも無ければ空 |
| それ以外 | 空（`knownRegion` は false） |

残っている `detect_test.go` の範囲のテスト（`TestRegions`・`TestHorizontalRuleRegions`）は入力と期待値をそのまま使う。呼び出す関数名が変わる場合だけ、呼び出し部分を直してよい。

---

## C. `internal/detect`: 作業中の表示（作業の証拠）

SPEC G3「Claude/Codex作業表示化石の15秒失効」と、Codex の待機判定の補正に使う。`detect.go` と `status` が次を呼ぶ。

```go
func ActivityFingerprint(agent, content string) string // 作業の証拠の行を改行でつないだもの。無ければ ""
func codexActivityFingerprint(content string) string
```

- **Claude**（agent が `claude`）: `above_prompt_box` の範囲の行のうち、次のどれかに当たる行を上から順に集める。
  - **Claude のスピナー行**: 前後の空白を除いた先頭の1文字が `·✱✲✳✴✵✶✷✸✹✺✻✼✽✾✿❀❁❂❃❇❈❉❊❋✢✣✤✥✦✧✨⊛⊕⊙◉◎◍⁂⁕※⍟☼★☆` のどれかで、その直後が空白、残りに `…` と英数字（Unicode の文字・数字）を含む行。
  - 小文字にして `esc to interrupt` か `ctrl+c to interrupt` を含む行。
- **Codex**（agent が `codex`）: 下の「Codex の生きている範囲」の行のうち、先頭の空白を除いて `•` で始まり `Working (` を含む行と、小文字で `esc to interrupt` を含む行を集める。
  - **Codex の生きている範囲**: 最後の「先頭の空白を除いて `›` で始まる行」を探す。無ければ画面全体。あれば、その行から上へ、空行と「上の2種類の作業の行」が続く限りさかのぼる。さかのぼった中に作業の行が1つでもあれば、さかのぼった一番上の行から最後まで。1つも無ければ `›` の行から最後まで。
- そのほかの agent: ""。
- Codex の判定が「待機」でも、`codexActivityFingerprint` が空でなければ「作業中」に直す（`detect.go` の既存の呼び出しのまま）。

残っている `TestActivityFingerprint` の入力と期待値をそのまま満たすこと。

---

## D. `internal/dispatch`: メール・ジョブ・ペインの会話記録の保存（ファイル新設）

SQLite（`github.com/ncruces/go-sqlite3`、既存の依存）で、表の形は `docs/ref/herdr-db-schema.sql` と同じ列名・同じ状態名にする（`ikesu log --db` で中を見る人のため）。加えて `pane_sessions`（下の PaneSession）用の表を持つ。SQL は自分で書く。残っている `store_test.go` を全部満たすこと。

### 定数・型（この形で作る）

```go
const DefaultRoom = "default"
const JobsRoom = "ikesu-jobs"
const RunShellActor = "ikesu-run"
const DBPathEnvVar = "IKESU_DB_PATH"

type Store struct{ /* 自由 */ }
type Message struct {
	ID int64 `json:"id"`; Room string `json:"room"`; Project string `json:"project"`
	FromAgent string `json:"from_agent"`; ToAgent string `json:"to_agent"`; Body string `json:"body"`
	CreatedAt string `json:"created_at"`; DeliveredAt *string `json:"delivered_at"`; ReadAt *string `json:"read_at"`
}
type Job struct {
	ID string `json:"id"`; Label string `json:"label"`; Command string `json:"command"`; Cwd string `json:"cwd"`
	CallerPane string `json:"caller_pane"`; CallerAgent string `json:"caller_agent"`; Completion string `json:"completion"`
	Status string `json:"status"`; RunnerPID *int64 `json:"runner_pid"`; ExitCode *int64 `json:"exit_code"`
	StartedMs *int64 `json:"started_unix_ms"`; FinishedMs *int64 `json:"finished_unix_ms"`; LogPath string `json:"log_path"`
}
type PendingNudge struct { Room string; ToAgent string; Count int; LatestFrom string }
type PaneSession struct {
	Session string; PaneID int; TerminalID string; WorkspaceID int; TabID int; Cwd string
	Agent string; SessionKind string; SessionID string; Title string; Source string; ObservedAt string
}
```

### 関数とメソッドの約束

| シグネチャ | 約束 |
|---|---|
| `DBPath() string` | `$IKESU_DB_PATH` があればそれ、無ければ `$IKESU_STATE_DIR/ikesu.db`（`IKESU_STATE_DIR` の既定は `internal/api` の StateDir と同じ） |
| `DBExists() bool` | `DBPath()` のファイルがあるか |
| `JobLogDir() string` | `$IKESU_STATE_DIR/job-logs` |
| `JobLogPath(jobID, label string) string` | `JobLogDir()/<jobID>-<label を安全な文字にしたもの>.log`（label が空なら `<jobID>.log`）。`ikesu log <job>` がこの名前でログを探す |
| `Schema() string` | 開くときに流す DDL の文字列（`ikesu log --schema` が表示する） |
| `Open(path)` / `OpenReadOnly(path)` | 読み書きで開く（無ければ作る）／読み取り専用で開く（無ければ失敗）。WAL、待ち時間 5000ms、synchronous=NORMAL。古い DB に `read_at` 列が無ければ足す（一度だけ） |
| `Close()` | 閉じる |
| `UpsertActor(kind, name, source, model, sessionID, paneID *string) (int64, error)` | (kind, name) で同じ行を使い、なければ作る。source などは新しい値で上書き。行の id を返す |
| `InsertMessage(room, project, from, to, body string, replyTo *int64, expectsReply bool) (Message, error)` | 送り手と宛先の actor を（kind=agent で）用意し、状態 `queued` のメッセージを作る。replyTo があれば、その行に `replied_at` を入れる（その行がジョブでも状態は変えない） |
| `MessagesForInboxRecipients(room string, recipients []string, inFlight map[int64]bool) ([]Message, error)` | 宛先がどれかの名前の `queued` を古い順に取り、同じ処理の中で `delivered`（`delivered_at` と、未設定なら `read_at`）にする。inFlight の id は取らない・変えない。room が空なら全部の room |
| `PendingMessagesForAgent(room, to string)` | その room・宛先の `queued` を古い順 |
| `PendingForAgentCreationOrder(to string)` | 宛先の `queued` を、`JobsRoom` 以外の全 room から、作られた順（同時なら id 順） |
| `PendingNudgesForAgent(to string) ([]PendingNudge, error)` | 宛先の `queued` を room ごとにまとめ、件数と一番新しい送り手を入れる。room の並びはその room の最初の未配送 id 順 |
| `PendingNudgeFor(room, to string) (*PendingNudge, error)` | 上の1 room 分。無ければ nil |
| `History(room string, project *string, limit int)` | その room（project 指定時はそれも一致）の新しい方から limit 件を、古い順に並べて返す |
| `Rooms()` | メッセージのある room を、最近の活動順で重複なく |
| `MarkMessageDelivered(id) (bool, error)` | その行が `queued` のときだけ `delivered` にして true。そうでなければ false（何度呼んでも同じ） |
| `MarkDelivered(room, to) (int, error)` | その room・宛先の `queued` を全部 `delivered` にして件数 |
| `MarkDeliveredByIDs(ids []int64) (int, error)` | 指定 id のうち `queued` のものだけ `delivered` にして件数（指定外の新着は `queued` のまま） |
| `InsertCommand(id, label, command, cwd, callerPane, callerAgent, completion, logPath string) error` | ジョブ（kind=command、状態 `queued`、external_id=id）を作る。送り手は RunShellActor、宛先は callerAgent |
| `MaxJobSeq() (int64, error)` | `jNNNN` 形の external_id の数字の最大（無ければ 0） |
| `CommandRows(limit int)` | ジョブを新しい順（limit>0 なら件数制限） |
| `CommandRow(id string) (*Job, error)` | external_id が id、またはログファイル名が `…id.log` で終わるジョブ。無ければ nil |
| `CommandDispatchID(id string) (*int64, error)` | そのジョブの行 id（reply_to 用） |
| `MarkCommandRunning(id, pid)` | `queued`→`running`、runner_pid と開始時刻を入れる |
| `MarkCommandFinished(id, exitCode *int64) (bool, error)` | `running` のときだけ `exited`（終了時刻と exit code）にして true |
| `MarkCommandCancelling(id) (bool, error)` | `running` のときだけ `cancelling` にして true |
| `MarkCommandCancelled(id) (bool, error)` | `queued`・`running`・`cancelling` のときだけ `cancelled`（終了時刻）にして true |
| `PutPaneSession(PaneSession) error` / `PaneSessions(session string) ([]PaneSession, error)` / `DeletePaneSession(session string, paneID int) error` | ペインと会話 id の対応表（SPEC G3 の ledger）。(session, paneID) で1行。Put は置き換え、一覧はその session の全行 |

時刻の文字列は UTC の `2006-01-02T15:04:05.000Z` 形（ミリ秒まで）。

---

## E. `internal/cli`: 削除した補助関数

呼び出し側はそのまま残してあるので、同じ名前・同じ引数で作る。

| 関数 | 約束 |
|---|---|
| `parseEnvAssignment(s string) (key, value string, err error)` | `KEY=VALUE`。`=` が無い、または KEY が空ならエラー |
| `parseReadSource(v string) (string, error)` | `visible`→visible、`recent`→recent、`recent-unwrapped`・`recent_unwrapped`→recent_unwrapped、`detection`→detection、それ以外はエラー `invalid read source: <v>` |
| `parseSplitDirection(v string) (string, error)` | `right`・`down`（と呼び出し側が使っている値）を受け付け、それ以外はエラー。受け付ける値は呼び出し側と `docs/ref/herdr-cli-pane.txt` に合わせる |
| `parseU32Flag` / `parseU64Flag` | 呼び出し側の使い方どおり、符号なし整数として読み、範囲外・数字でなければエラー |
| `(r *runner) resolveAgentName(target string) (string, error)` | API `agent.get` で target を引き、表示名（name）→ agent の種類 → global pane id の順に、空でない最初のものを返す。どれも無ければエラー `target has no reported agent identity` |
| `(r *runner) resolveCurrentPaneID() (string, error)` | `IKESU_PANE_ID` があればそれを呼び出し元として、既存の `r.currentPaneID` で決める |
| `printMsgMessage(w, m api.MsgMessage)` | 1行 `#<id> [<room>] <created_at> <from> -> <to>: <body>` |
| `printSendHelp` / `printMsgHelp` / `printLogHelp` / `printRunHelp` | 各コマンドの使い方の説明。内容は `docs/ref/herdr-cli-ai.txt` の該当コマンドと同じ引数・同じ決まり（例 run の「待つ間 sleep やポーリングをしない」「完了通知の形」）を、`herdr` を `ikesu` にして ikesu の言葉で書く。文章を写さない |
| `defaultRunLabel(argv []string) string` | argv[0] の最後の部分。無ければ `run` |
| `oneLineField(s string) string` | 前後の空白を除き、改行を空白にする |
| `runSpawnNextHint(job, label string, pane *string, completion string) string` | `ikesu run` の受付応答の `next` 欄。pane 実行なら「呼び出し元に `[ikesu run] exit=<code> label=<label> pane=<pane>` が届く、sleep やポーリングをせず待ち、`ikesu log <job>` を読む」。completion が none なら「終了通知は来ない、`ikesu run list` か `ikesu log <job>` で自分で確かめる」。それ以外は「`[ikesu run] exit=<code> label=<label> job=<job>` が届く、ターンを終えて待ち、`ikesu log <job>` を読む」 |
| `shellCommandFromArgs(argv []string) string` / `shellQuote(s string) string` | argv の各要素を POSIX シェル用に引用して空白でつなぐ（`sh -c` に渡して元の argv に戻ること）。空文字は `''`。安全な文字だけの要素は引用しない |
