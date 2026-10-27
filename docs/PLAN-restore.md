# ikesu 保存と復元の段階の計画（SPEC G3 ＋ 関連節）

作成: 2026-10-08（親 Claude）。店長の方針「SPEC.md のとおりに全部実装する。優先順位は無い」に基づく、AI 連携の次の段階。

仕様の正は `~/src/github.com/kazuph/herdr/SPEC.md`（読むだけ。`herdr` → `ikesu`、`HERDR_*` → `IKESU_*`、「Herdr server restart」→「ikesu サーバーの再起動」と読み替える）。
各節の「UI挙動」「受け入れ条件」「デグレ判定」をそのまま ikesu の受け入れ条件として扱う。「本家」（ogulcancelik/herdr）の機能に乗る前提の記述は、ikesu に同等の仕組みを作って満たす。

## 1. 作る節（全部、節の内容どおり）

| SPEC.md の節 | ikesu での前提・読み替え |
|---|---|
| G3「paneごとのexact agent session復元」 | 下の「セッションの保存」を土台にする。`[agent_start.commands]`・`[agent_restore] restore_delay_ms`・restore template は ikesu の設定ファイル（`~/.config/ikesu/config.toml`）に置く。旧 fork snapshot・v4 以前の移行は ikesu に旧形式が無いので作らない（受け入れ条件のうち移行の3項目は対象外として RESULT.md に明記） |
| G3「session id欠落時のfail-closed再起動警告」 | ikesu に「サーバーの再起動」（API `server.restart`、CLI `ikesu server restart`、メインメニューの項目と確認ダイアログ）を作り、その確認ダイアログを節のとおりにする |
| G3「restorable agent paneの保持」 | 節のとおり |
| G3「inactive shell paneの通常終了cleanup」 | 節のとおり |
| G3「agent restore結果toastの空状態明確化」 | AI 連携段階で作ったトーストを使う |
| G3「Claude/Codex作業表示化石の15秒失効」 | ikesu の検出（`internal/detect`・`internal/status`）に足す。hook authority の部分は、ikesu の `pane report-agent` による報告を hook authority として扱う |
| G5「global pane番号のsession再起動をまたぐ安定性」 | 節のとおり |
| G2「Space cardのjob dot」 | サイドバーのワークスペース行に、節の条件どおりジョブの `●` を出す（`[jobs]` パネルが ikesu に無ければ、ジョブ一覧は `ikesu run list` と同じデータを使い、パネル自体は作らない。RESULT.md に明記） |

## 2. セッションの保存（上の節の土台）

- サーバーは、ワークスペース・タブ・分割・各ペインの作業フォルダ・ペイン番号（`pN`）・ペイン名・agent の記録（agent 種別・session id・title）を
  `$IKESU_STATE_DIR/<session>/session.json` に保存する。状態が変わったら少し間をおいてまとめて書き、サーバー終了時にも書く。
  書き込みは一時ファイルへ書いてから置き換える（途中で壊れたファイルを残さない）。
- サーバー起動時に session.json があれば、同じ配置・同じペイン番号でシェルを作業フォルダに起動し直す。番号の検証は G5 の節どおり。
- ペインと session の対応表（ledger）は SPEC の項目どおりに持つ。保存先は SQLite（`$IKESU_STATE_DIR/ikesu.db`）の新しい表でよい。
- `ikesu server restart` は「保存 → 停止 → 起動 → 復元」を行い、つながっている画面は新しいサーバーへつなぎ直す。

## 3. 守ること

- 土台・AI 連携の方針（状態は純粋なデータ、API と画面は同じコマンドで状態を変える、1ファイル 800 行まで、推測で埋めない）。
- **`--last`・`resume --last`・作業フォルダの最新 session・mtime 最新での復元は、どの経路でも絶対に作らない**（SPEC のデグレ判定）。
- 性能: 区切りごとに ikesu の `SPEC.md`「P1. 性能要件」の相対値の検査（main と交互に各5回）を mini で行い、結果を RESULT.md に書く。
  `tests/perf` がまだ main に無ければ、`tools/bench/bench_screen.sh` と `bench_interrupt.sh` で同じ手順を交互に回す。
- 店長の Claude Code・Codex の本物の session ファイル（`~/.claude/projects`、`~/.codex/sessions`）は読み取り専用。テストは一時フォルダに作った偽の session ファイルで行う。実物確認で claude / codex を起動する場合も、新しい会話を作るだけにし、既存の会話を再開しない。

## 4. 進め方と確認

- 区切り: (1) セッションの保存と復元・ペイン番号の安定・サーバー再起動、(2) exact session 復元と ledger・restore コマンド・トースト、(3) 再起動警告ダイアログ・paneの保持と cleanup、(4) 15秒失効、(5) ジョブの dot、(6) 仕上げ（実物確認・性能）。各区切りで gofmt・go build・go vet・go test（最後は -race も）が mini で通ってから積み上げコミット。
- 実物確認（ptyrun で画面を保存）: 同じ作業フォルダで claude（または codex）を2つ起動し、それぞれ `ikesu pane report-agent --session-id` で別の id を報告 → `ikesu server restart` → 2つのペインが同じ番号で戻り、`ikesu agent restore --dry-run` がそれぞれ別の id のコマンドを返す。session id の無い agent ペインがあるときの再起動確認ダイアログ。
- 計測・結合テスト・race は mini。メインの Mac では go build と軽い go test だけ。
- push・amend・reset・force push しない。`.artifacts/` は git add しない。
