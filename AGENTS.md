# AGENTS.md — goterrace（gtx）で作業する AI への決まり

## 最重要: 仕様から実装する。ほかのプロジェクトの実装を移植しない（2026-10-08 店長の決定）

gtx は herdr fork（`~/src/github.com/kazuph/herdr`、AGPL-3.0）の仕様に沿って作るが、**herdr の実装を移し替えて作ることは禁止**する。

- **してよいこと（仕様からの実装）**
  - herdr の `SPEC.md`、CLI の help、API スキーマ（`docs/ref/`）、利用者向けの説明を読み、書かれた挙動を gtx 独自のコードで実装する。
  - herdr の実物（配布用ビルド）を、店長が使っている稼働中の herdr とは別に動かし（例: mini の `~/bench-terrace.noindex/work/herdr-target/release/herdr --no-session` を ptyrun の中で）、入力に対する画面や出力を観察して、その挙動に合わせる。
  - API のメソッド名・項目名・CLI の引数と出力の形を herdr と同じにする（外から見える約束をそろえるのは移植ではない）。
- **してはいけないこと（実装の移植）**
  - herdr のソースコード（`src/**/*.rs` などの実装・テストコード）を読んで、関数・アルゴリズム・データ構造・テストを Go へ書き換えること。言語が違っても、1行ずつでなく関数ごとでも、移し替えは移植にあたる。
  - 実装役（Devin などの子 agent）に herdr のソースコードを読ませること。挙動がわからないときは、上の「実物を動かして観察する」か、親へ質問する。
  - コメントや RESULT.md に「herdr の〇〇から移植」「写経」と書くような作り方をすること。
- 親（計画とレビューの担当）が herdr のソースで挙動を確かめた場合も、実装役へ渡すのは「入力と期待する結果」という挙動の説明だけにし、コードやその書き写しを渡さない。
- 移植した疑いのあるコードを見つけたら、取り込まずに親へ報告する。

経緯: 2026-10-08、レイアウト操作（`internal/layout/ops.go`）を実装役が herdr の `layout.rs` から関数ごとに Go へ移し替えていた。gtx は MIT で herdr は AGPL-3.0 のため、店長の判断で挙動だけを参考に書き直すことになった。

## そのほかの決まり

- 仕様の正: herdr fork の `SPEC.md`（読むだけ）と、gtx 固有の要件を書いたこのリポジトリの `SPEC.md`。
- ビルド・計測・race・結合テストは Mac mini（`ssh mini`、`~/bench-terrace.noindex`）で行い、メインの Mac では go build と軽い go test だけにする。
- `.artifacts/` はコミットしない。amend・reset・force push をしない。
- 版（`cmd/gtx/main.go` の `version`）は、main へ取り込むたびに上げる（2026-10-18 店長の決定）。普段は3つ目の数字を1つ上げ（0.2.0→0.2.1）、大きな区切りでは2つ目を上げる。下部バーの「gtx v版」で、サーバーだけ引き継いだ後の古い画面を見分けるため。

## ikesu の Homebrew リリース（2026-10-27 店長承認）

- 合意した変更の取り込みでpatch版を上げる。`cmd/ikesu/main.go`の版を更新して通常の検証・人のマージ判断を済ませ、mainをpushしてから同じ版の`ikesu-vX.Y.Z`タグをpushする。初回予定は0.4.11。実装役は依頼なしに版上げ・push・タグ作成をしない。
- `.github/workflows/ikesu-release.yml`の全job成功を確認する。4環境（darwin arm64/amd64・linux arm64/amd64）のネイティブrunnerで版・アーキテクチャ・隔離serverの起動停止を検査する。Macはパネルと同じDeveloper ID（Team 67M8FDFZ28）で署名・厳密検証し、その後にsha256を作る。公証は行わない。Releaseの4実行ファイルとSHA256SUMS、tapの`Formula/ikesu.rb`の版・URL・sha256を確認する。
- 手元・mini・ubuntu-3090の3台で`./scripts/ikesu-upgrade.sh`を実行する。brewがPATHにないSSH環境では`--brew <実際のbrewの絶対パス>`を指定する（設置先を推測しない）。既定はdefault session、複数なら`--session <名前>`を指定順に繰り返す。`brew --prefix ikesu`から実体を取得し、`~/.local/lib/ikesu/releases/<版>/ikesu`へ保持してからlive-handoffする。`HOMEBREW_NO_INSTALL_CLEANUP=1`を付け、古いreleasesとkegを勝手に消さない。Formulaのpost_installで稼働serverを操作しない。
- 指定sessionは1つずつ更新する。途中失敗ではそこで停止し、残りのsessionと`~/.local/bin/ikesu`は変えない。成功済みsessionは新版のままとし、一括巻戻しは行わない。全session成功後だけ入口を保持用実体へのsymlinkに原子的に置き換える。出力されるbrew版・入口版・各sessionのserver版を別々に確認し、ペインID/シェルPID・入出力・plugin・進行中jobと子起動を確認する。初回installでも不要なserverは起動しない。
- 戻すときは保持済み旧版の実体を確認し、対象sessionへ`<旧実体> --session <名前> server live-handoff --import-exe <旧実体> --expected-version <旧版>`を実行してから入口symlinkを戻す。版やprotocolの不一致で拒否されたら旧serverを止めずに原因を調べる。brewの掃除から独立した保持用実体は削除しない。
- 公開済みReleaseの再アップロード・差替えは禁止。公開前の失敗でdraftが残った場合も自動で上書きしない。親が失敗箇所とdraftの状態を確認する。tapだけ失敗した場合はActionsのworkflow_dispatchで公開済みのタグを入力し、ReleaseのSHA256SUMSからtap更新だけを再開する。tapは最新を取得してFormulaだけ変更し、版の後退・同版の別内容を拒否する。並行したtap更新は通常のfast-forward pushで競合を拒否するため、失敗側を最新tapから再開する。force pushしない。
- `HOMEBREW_TAP_DEPLOY_KEY`は親が登録する専用の書込deploy key。tap更新job以外へ渡さない。権限はtap全体に及ぶため、Formulaの他の項目を変更しない。SSHホスト鍵はGitHubのHTTPS meta APIから取得して厳密照合し、一時秘密鍵・known_hostsをjob終了時に削除する。署名用P12はMac署名stepだけで使い、元のkeychain検索順を復元して一時keychainを削除する。秘密をログ・artifact・cacheへ含めず、鍵jobではRelease内のスクリプトを実行しない。漏洩時は親がhomebrew-tapの該当deploy keyとikesu側secretを削除して失効させる。
- Mac判断待ちパネル（`ikesu-mac-dialog-v*`）・tb・`ikesu mac-dialog install`の配信は変更しない。CLIのIntel/Linux対応はパネル/tbの対応拡張を意味しない。パネル/tbは従来どおりApple Silicon Macのみ。
