# ローカル・mini を v0.4.0 に切り替える

実行前提: **main が stage3 を含む**。親が stage3 を main へ早送りで取り込んだ後に実行する。元チェックアウトは main のままにし、実行するブランチの SHA が main の祖先であることと、main 直下の `apps/mac-dialog-decisions/ikesu-plugin.toml` をスクリプトで検査する。製品版は 0.4.0。

実装役は本番 `--dry-run` だけ。実行は親担当。全体・race・プラグイン実測が合格し、両機に v0.4.0 の実物がビルド済みであること。`--binary` と `--mini-binary` はその実物を指定する。既定値は段階3の保存済みビルド。mini は `ssh mini`、既定移行元は手元 `~/.local/state/gtx-2e01183` と実体 `~/.config/gtx`、現用APIソケットは引継ぎ元の `~/.local/state/gtx/default/gtx.sock`（`--source-socket` で明示変更可）。DB/stateが同じ場所だと仮定しない。mini `~/.local/state/gtx` と未作成でもよい `~/.config/gtx`。

```sh
scripts/cutover-local.sh --dry-run
```

dry-run は両機のジョブ・版・保存先・main/プラグイン配置を読み、変換内容と順番を標準出力に表示する。稼働中のロックを解除せず、本移行・ログファイル・journal は作らない。前提不成立なら拒否する。稼働中サーバーの queued/running/cancelling ジョブがあれば停止しない。

本実行は全ペインを閉じるため、親が既存の gtx/ikesu の外へ切り離して起動する。以下は **親だけが実行する例**。Python の `start_new_session=True` と `nohup` で端末終了から切り離し、作業ディレクトリを移動されない HOME にする。スクリプトは開始時に必要なコードとバイナリを読み込む。

```python
import os, subprocess
home = os.path.expanduser('~')
script = home + '/src/github.com/kazuph/goterrace/.worktree/feat/rename-ikesu-stage3/scripts/cutover-local.sh'
with open(home + '/ikesu-cutover-launch.log', 'ab') as log:
    subprocess.Popen(['nohup', script], cwd=home, stdin=subprocess.DEVNULL,
                     stdout=log, stderr=log, start_new_session=True)
```

順番は前提確認→mini 停止・停止確認・移行・配置・起動検証→手元停止・停止確認→移行→repo 移動と全 worktree repair→Claude projects の元を残したコピー→新版配置→手元起動検証。両機の版・スペース数・ペイン数・DB パスが一致してから、両機の旧リンクと `~/.local/lib/gtx` を完全削除する。削除を最後へまとめるのは、片機だけ旧版へ戻せない中間状態を減らすため。画面接続は行わない。

ログは手元 `~/ikesu-cutover-<日時>.log`。mini の出力も同じログに収める。途中操作は各機 `~/.ikesu-cutover-journal.json` に記録する。削除開始前の失敗は完了済みの操作を逆順で戻し、元設定/state を復元して旧サーバーを再起動する。新サーバー停止が確認できなければ live state を移動せず明確に止める。旧実行ファイル削除開始後は旧版への自動巻き戻しをせず、新版を維持して `ROLLFORWARD REQUIRED` と失敗箇所を残す。旧版を戻す場合はソースから再ビルドする（店長の決定）。

元設定/state は `.pre-ikesu` として保持する。過去の `gtx*` state や `.config/gtx-2e01183.pre-v030`、`.race-0851` などは一覧表示だけで削除しない。Claude の記録も元を残す。正常終了後、店長が `ikesu` で画面へ接続する。

試験の `--fail-after` は `preflight/stop/migrate/repo/claude/install/verify/delete`、mini 側は `mini:` prefix で、指定工程の直後に例外を発生させる。**本番では指定しない**。一時 HOME の検証は本物の旧新版バイナリ・SQLite・Git worktree を使い、SSH 接続だけ別 HOME に向ける。
