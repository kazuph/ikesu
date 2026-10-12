# guard の適用範囲: 修正前の再現

計画 PLAN-guard-coverage.md の最初の区切り。開始コミットは 14c190f。
mini の本物の Claude Code 2.1.296 を一時 HOME で実行し、普段の認証を CLAUDE_CONFIG_DIR で直接参照した。GUI を無効化し Slack 承認 script が一時 HOME に無いことを起動前に確認した。削除対象は隔離 root 内かつ起動 cwd/TMPDIR の外。

| 起動順 | 観測した状態 | 結果 |
|---|---|---|
| guard 有効 → zsh ログイン pane → 手動 Claude → Bash | 印パスと enforcing は継承されるが shim PATH は /bin より後ろ | 判断待ちなしで隔離対象が消える。ai_guard ログ増加なし |
| guard 無効 → pane → guard 有効・再読込 → Claude → Bash | 印と shim 環境なし | gtx 判断待ちなし、隔離 ai_guard が却下、ログ増加 |
| zsh/bash ログイン pane → agent メタデータ → 実 shell | 印と shim あり、PATH の shim が後ろ | 判断待ちなしで隔離対象が消える |
| 上記 → 起動環境を保存・新 shell に読み込み | 同じ PATH の順を継承 | 判断待ちなしで隔離対象が消える |
| 同じ pane で agent メタデータを解除・再設定 | 印と shim は再作成される、PATH は後ろ | 判断待ちなしで隔離対象が消える |
| live handoff → 実 shell | 印と shim は維持、PATH は後ろ | 判断待ちなしで隔離対象が消える |
| session 再起動で shell pane を復元 → agent メタデータ → 実 shell | 印と shim は作成、PATH は後ろ | 判断待ちなしで隔離対象が消える |

shell のメタデータ試験を本物 agent の試験として扱わない。agent の session 復元・handoff・再起動は修正後に本物の経路で確認する。

証拠: `.artifacts/guard-coverage/RESULT.md`（コミット対象外）。mini の gofmt・go build・go vet・go test（TestRelative/TestAbsolute を除外）は通過。
