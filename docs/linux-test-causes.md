# Linux テスト失敗の原因と修正方針

対象: PLAN-linux.md 1章。Git 2.34.1、Go 1.27.0、ubuntu-3090。herdr 実装・テストコードは参照していない。

| テスト名 | 原因 | 分類 | 修正・検証 |
|---|---|---|---|
| `TestG9CleanupWaitsForOtherProcessWriter` | /usr/bin/sqlite3 の存在を前提 | テスト | 同じテスト実行ファイルの別プロセスで既存 SQLite ライブラリの実トランザクションを保持 |
| `TestBundledSoundsNumericWAVAndPlaybackPaths` | 本体の Linux 音再生未対応、OS 通知なし。テストは afplay と terminal-notifier を前提 | 本体・テスト | 文書の Linux 再生候補・notify-send を使用。OS 別期待値と差し替えで検証 |
| `TestDefaultSoundCacheFailureKeepsNotificationAndWarnsOnce` | 本体の Linux 音再生未対応、OS 通知なし。テストは afplay と terminal-notifier を前提 | 本体・テスト | 文書の Linux 再生候補・notify-send を使用。OS 別期待値と差し替えで検証 |
| `TestDesktopNotificationClickAndSoundCommands` | 本体の Linux 音再生未対応、OS 通知なし。テストは afplay と terminal-notifier を前提 | 本体・テスト | 文書の Linux 再生候補・notify-send を使用。OS 別期待値と差し替えで検証 |
| `TestWorktreeSocketCLIAndEvents` | Git 2.34.1 は worktree list --porcelain -z 未対応 | 本体 | 行区切り porcelain・登録済み完全パス・新 Git の引用パスを使い、改行入りパスも保持 |
| `TestWorktreeCLIAllOperationsAndBareSocket` | Git 2.34.1 は worktree list --porcelain -z 未対応 | 本体 | 行区切り porcelain・登録済み完全パス・新 Git の引用パスを使い、改行入りパスも保持 |
| `TestWorkspaceJobDot` | Linux では kill(-pgid,0) が zombie だけの群にも成功 | 本体 | Linux の群に実行可能なメンバーが残るか /proc で確認。macOS の判定は維持 |
| `TestConfigEveryActionRemapThroughRealInput` | Git 2.34.1 は worktree list --porcelain -z 未対応 | 本体 | 行区切り porcelain・登録済み完全パス・新 Git の引用パスを使い、改行入りパスも保持 |
| `TestConfigDefaultShellAndModeReachRealPTY` | auto は macOS のみ login という文書契約に対し全 OS で -l を期待 | テスト | auto の期待を OS ごとに分け、login/non_login は全 OS で実 PTY 検証 |
| `TestConfigWorktreeDirectoryCreatesCheckoutInConfiguredRoot` | Git 2.34.1 は worktree list --porcelain -z 未対応 | 本体 | 行区切り porcelain・登録済み完全パス・新 Git の引用パスを使い、改行入りパスも保持 |
| `TestParityScrollbackEditorUsesRealViPTYAndReturnsToShell` | Linux の実 vi が長い一時パスのメッセージで Press ENTER 待ち | テスト | 実 vi の継続プロンプトに Enter を送信し、本文表示と終了を検証 |
| `TestParityPopupScrollbackEditorKeepsPopupUntilEscape` | Linux の実 vi が長い一時パスのメッセージで Press ENTER 待ち | テスト | 実 vi の継続プロンプトに Enter を送信し、本文表示と終了を検証 |
| `TestG2GitNumstatBinaryAndLinkedName` | Git 2.34.1 は worktree list --porcelain -z 未対応 | 本体 | 行区切り porcelain・登録済み完全パス・新 Git の引用パスを使い、改行入りパスも保持 |
| `TestG2GitLeftRightMenuOrderAndSectionAction` | Git 2.34.1 は worktree list --porcelain -z 未対応 | 本体 | 行区切り porcelain・登録済み完全パス・新 Git の引用パスを使い、改行入りパスも保持 |
| `TestSidebarWorktreeNewExistingOpenDelete` | Git 2.34.1 は worktree list --porcelain -z 未対応 | 本体 | 行区切り porcelain・登録済み完全パス・新 Git の引用パスを使い、改行入りパスも保持 |
| `TestWorktreeDirtyFailureKeepsFocusAndPersistedGroup` | Git 2.34.1 は worktree list --porcelain -z 未対応 | 本体 | 行区切り porcelain・登録済み完全パス・新 Git の引用パスを使い、改行入りパスも保持 |
| `TestWorktreeMoveBackToGroupAndFailureWithoutMutation` | Git 2.34.1 は worktree list --porcelain -z 未対応 | 本体 | 行区切り porcelain・登録済み完全パス・新 Git の引用パスを使い、改行入りパスも保持 |
| `TestWorktreeCreateExistingAndSafeRemoval` | Git 2.34.1 は worktree list --porcelain -z 未対応 | 本体 | 行区切り porcelain・登録済み完全パス・新 Git の引用パスを使い、改行入りパスも保持 |
| `TestBareRepositoryWorktree` | Git 2.34.1 は worktree list --porcelain -z 未対応 | 本体 | 行区切り porcelain・登録済み完全パス・新 Git の引用パスを使い、改行入りパスも保持 |
| `TestRawLFToPane` | BSD od の空白幅を固定し、od 起動前の shell に入力が届く競合もある（raw LF は shell の処理で 0d、Shift+Enter は出力なし） | テスト | 実 od の準備完了を待ち、tty の byte 変換を止めて空白を正規化し byte 列を検証 |
| `TestShiftEnterToPane` | BSD od の空白幅を固定し、od 起動前の shell に入力が届く競合もある（raw LF は shell の処理で 0d、Shift+Enter は出力なし） | テスト | 実 od の準備完了を待ち、tty の byte 変換を止めて空白を正規化し byte 列を検証 |
| `TestKittyExplicitShiftEnter` | BSD od の空白幅を固定し、od 起動前の shell に入力が届く競合もある（raw LF は shell の処理で 0d、Shift+Enter は出力なし） | テスト | 実 od の準備完了を待ち、tty の byte 変換を止めて空白を正規化し byte 列を検証 |
| `TestFixTermQueryResponse` | BSD od の空白幅を固定し、od 起動前の shell に入力が届く競合もある（raw LF は shell の処理で 0d、Shift+Enter は出力なし） | テスト | 実 od の準備完了を待ち、tty の byte 変換を止めて空白を正規化し byte 列を検証 |
| `TestG9ForegroundCacheReusesScanAndInvalidatesCwdAndProgram` | shell の cwd が二つの /proc 読み取り間で変わり、画面 cwd だけ先に更新 | テスト | 画面 cwd とキャッシュキーの cwd の両方の収束を待って更新を検証 |

音再生候補が全て無いときは、親の判断（2026-10-13）により音のみ抑制し、通知を保持し、候補名を含む警告を1回だけ出す。候補順は paplay、pw-play、ffplay、mpg123、mpv。

未修正版は失敗が前提のため原因表段階では全テスト成功を報告しない。最終の全テスト・race 成功は RESULT.md の実行証拠で確認する。

証拠: `.artifacts/linux/baseline234.json` に24件の親テスト失敗（子テストを含む失敗イベントは31件）。`focused.txt` に修正対象の実機テスト成功。
