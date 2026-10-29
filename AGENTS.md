# AGENTS.md — ikesu で作業する AI への決まり

## 最重要: 仕様から実装する。ほかのプロジェクトの実装を移植しない（2026-10-08 店長の決定）

ikesu は herdr fork（`~/src/github.com/kazuph/herdr`、AGPL-3.0）の仕様に沿って作るが、**herdr の実装を移し替えて作ることは禁止**する。

- **してよいこと（仕様からの実装）**
  - herdr の `SPEC.md`、CLI の help、API スキーマ（`docs/ref/`）、利用者向けの説明を読み、書かれた挙動を ikesu 独自のコードで実装する。
  - herdr の実物（配布用ビルド）を、店長が使っている稼働中の herdr とは別に動かし（例: mini の `~/bench-terrace.noindex/work/herdr-target/release/herdr --no-session` を ptyrun の中で）、入力に対する画面や出力を観察して、その挙動に合わせる。
  - API のメソッド名・項目名・CLI の引数と出力の形を herdr と同じにする（外から見える約束をそろえるのは移植ではない）。
- **してはいけないこと（実装の移植）**
  - herdr のソースコード（`src/**/*.rs` などの実装・テストコード）を読んで、関数・アルゴリズム・データ構造・テストを Go へ書き換えること。言語が違っても、1行ずつでなく関数ごとでも、移し替えは移植にあたる。
  - 実装役（Devin などの子 agent）に herdr のソースコードを読ませること。挙動がわからないときは、上の「実物を動かして観察する」か、親へ質問する。
  - コメントや RESULT.md に「herdr の〇〇から移植」「写経」と書くような作り方をすること。
- 親（計画とレビューの担当）が herdr のソースで挙動を確かめた場合も、実装役へ渡すのは「入力と期待する結果」という挙動の説明だけにし、コードやその書き写しを渡さない。
- 移植した疑いのあるコードを見つけたら、取り込まずに親へ報告する。

経緯: 2026-10-08、レイアウト操作（`internal/layout/ops.go`）を実装役が herdr の `layout.rs` から関数ごとに Go へ移し替えていた。ikesu は MIT で herdr は AGPL-3.0 のため、店長の判断で挙動だけを参考に書き直すことになった。

## 最重要: ビルドとテストは速く軽く保つ。長引くものは止めて、すぐ直す（2026-10-29 店長の決定）

ikesu は開発者体験を良くするための道具なので、ikesu 自身の開発も速く軽くなければならない。高速化と軽量化は、言われるのを待たずにどんどん行う。

- **目安の時間**（mini で測った実時間）
  - 全テスト（通常と race の両方）: 10分以内。
  - 1つのパッケージ: 2分以内。
  - 1つのテスト: 5秒以内。超えるテストには、速くできない理由をコメントで書く。
  - ふだんの作業での確認（変えたパッケージだけ）: 1分以内。
- **長引いたら止める**: 目安の2倍を超えた、または15分を超えたビルド・テストは、終わるのを待たずに `ikesu run cancel <job>` で止める。mini に残ったプロセスは、自分の作業フォルダのパスで確かめた番号を `kill` で止める。止めたら、まずそのビルドやテストの高速化・軽量化を行い、速くなったことを測ってから流し直す。「終わるまで待つ」「長いけど通ったから良し」で済ませない。
- **速くする手段**: テストを減らして速くするのではなく、やり方を変えて速くする。
  - パッケージを同時に流す。
  - 独立したケースを `t.Parallel` で同時に流す。
  - 決まった時間だけ待つ処理は、状態を見て待つ処理に変える。
  - 作業中は変えたパッケージだけを流し、テスト結果のキャッシュを使う。
  - 通常と race は、順番ではなく同時に流す。
  - 確認の項目を減らす・弱めるときは、店長に確認してから行う。
- **時間を記録する**: テストを流したら、パッケージごとの時間（`go test -json` の Elapsed）を残す。目安を超えたものは報告に書き、その場で直すか、直す作業を始める。

経緯: 2026-10-29、全テストが通常17分と race 23分を順番に流して約41分かかっていた。パッケージは1つずつ順番で（`-p 1`）、テストは2コアまでしか使えない設定（`GOMAXPROCS=2`）だった。時間の74%を `tests/e2e` と `internal/server` が使い、5秒を超える48個のテストが全体の69%を使っていた。長いテストを待ち続けることが当たり前になっていた。

## そのほかの決まり

- 仕様の正: herdr fork の `SPEC.md`（読むだけ）と、ikesu 固有の要件を書いたこのリポジトリの `SPEC.md`。
- ビルド・計測・race・結合テストは Mac mini（`ssh mini`、`~/bench-terrace.noindex`）で行い、メインの Mac では go build と軽い go test だけにする。
- `.artifacts/` はコミットしない。amend・reset・force push をしない。
- **main へ直接コミット・マージ・push しない（2026-10-28 店長の決定）**。変更はすべて `origin/main` から `git wt <branch>` で worktree を作り、そのブランチを push して GitHub の Pull Request で main へ取り込む。文書だけの変更・版上げ・パネルの版上げも同じ。PR のマージは人の判断（そのタスクで「マージして」と明示された時だけ親がマージしてよい）。ただし、次の3つを全部満たす PR は、親が聞かずにマージし、リリース（タグ・GitHub の Release・Homebrew の tap）と3台の入れ替えまで進めてから報告する（2026-10-29 店長の決定。依頼してすぐ反映できるようにするため）: (1) 全テスト（通常と race）が通っている。(2) 親と Astra のレビューが終わり、重大な指摘が残っていない（2回目の後に直した重大な指摘は、正規の試験で確かめたことを PR に書く）。(3) 店長が依頼した作業の範囲の中。仕様を変えるもの、確認を減らす・弱めるもの、消す・払う・外へ出すものは、今までどおり店長に聞く。マージ後は手元の main で `git pull --ff-only` する。リリースのタグは、版上げの PR が main にマージされた後の main のコミットに付ける。
- 版（`cmd/ikesu/main.go` の `version`）は、main へ取り込むたびに上げる（2026-10-18 店長の決定）。普段は3つ目の数字を1つ上げ（0.2.0→0.2.1）、大きな区切りでは2つ目を上げる。下部バーの「ikesu v版」で、サーバーだけ引き継いだ後の古い画面を見分けるため。

## ikesu の Homebrew リリース（2026-10-27 店長承認）

- 合意した変更の取り込みでpatch版を上げる。`cmd/ikesu/main.go`の版を更新するPRを作り、通常の検証・人のマージ判断を経てmainへマージされた後、そのmainのコミットに同じ版の`ikesu-vX.Y.Z`タグを付けてpushする。初回予定は0.4.11。実装役は依頼なしに版上げ・push・タグ作成をしない。
- `.github/workflows/ikesu-release.yml`の全job成功を確認する。4環境（darwin arm64/amd64・linux arm64/amd64）のネイティブrunnerで版・アーキテクチャ・隔離serverの起動停止を検査する。Macはパネルと同じDeveloper ID（Team 67M8FDFZ28）で署名・厳密検証し、その後にsha256を作る。公証は行わない。Releaseの4実行ファイルとSHA256SUMS、tapの`Formula/ikesu.rb`の版・URL・sha256を確認する。
- 手元・mini・ubuntu-3090の3台で`./scripts/ikesu-upgrade.sh`を実行する。brewがPATHにないSSH環境では`--brew <実際のbrewの絶対パス>`を指定する（設置先を推測しない）。既定はdefault session、複数なら`--session <名前>`を指定順に繰り返す。`brew --prefix ikesu`から実体を取得し、`~/.local/lib/ikesu/releases/<版>/ikesu`へ保持してからlive-handoffする。`HOMEBREW_NO_INSTALL_CLEANUP=1`を付け、古いreleasesとkegを勝手に消さない。Formulaのpost_installで稼働serverを操作しない。
- 指定sessionは1つずつ更新する。途中失敗ではそこで停止し、残りのsessionと`~/.local/bin/ikesu`は変えない。成功済みsessionは新版のままとし、一括巻戻しは行わない。全session成功後だけ入口を保持用実体へのsymlinkに原子的に置き換える。出力されるbrew版・入口版・各sessionのserver版を別々に確認し、ペインID/シェルPID・入出力・plugin・進行中jobと子起動を確認する。初回installでも不要なserverは起動しない。
- 戻すときは保持済み旧版の実体を確認し、対象sessionへ`<旧実体> --session <名前> server live-handoff --import-exe <旧実体> --expected-version <旧版>`を実行してから入口symlinkを戻す。版やprotocolの不一致で拒否されたら旧serverを止めずに原因を調べる。brewの掃除から独立した保持用実体は削除しない。
- 公開済みReleaseの再アップロード・差替えは禁止。公開前の失敗でdraftが残った場合も自動で上書きしない。親が失敗箇所とdraftの状態を確認する。tapだけ失敗した場合はActionsのworkflow_dispatchで公開済みのタグを入力し、ReleaseのSHA256SUMSからtap更新だけを再開する。tapは最新を取得してFormulaだけ変更し、版の後退・同版の別内容を拒否する。並行したtap更新は通常のfast-forward pushで競合を拒否するため、失敗側を最新tapから再開する。force pushしない。
- `HOMEBREW_TAP_DEPLOY_KEY`は親が登録する専用の書込deploy key。tap更新job以外へ渡さない。権限はtap全体に及ぶため、Formulaの他の項目を変更しない。SSHホスト鍵はGitHubのHTTPS meta APIから取得して厳密照合し、一時秘密鍵・known_hostsをjob終了時に削除する。署名用P12はMac署名stepだけで使い、元のkeychain検索順を復元して一時keychainを削除する。秘密をログ・artifact・cacheへ含めず、鍵jobではRelease内のスクリプトを実行しない。漏洩時は親がhomebrew-tapの該当deploy keyとikesu側secretを削除して失効させる。
- Mac判断待ちパネル（`ikesu-mac-dialog-v*`）・tb・`ikesu mac-dialog install`の配信は変更しない。CLIのIntel/Linux対応はパネル/tbの対応拡張を意味しない。パネル/tbは従来どおりApple Silicon Macのみ。
