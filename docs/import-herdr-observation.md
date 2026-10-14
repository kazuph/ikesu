# herdr 保存形式の観察（2026-10-14）

仕様と保存データだけを参照した。herdr の実装・テストコードは読んでいない。
本物の保存ファイルは読み取りのみ。一時の写しを検証に使う。

- session.json は version 5。トップには workspaces、active、selected、sidebar_width、sidebar_section_split、collapsed_space_keys、collapsed_workspace_sections がある。
- workspace は文字列 ID、nullable な custom_name、section、identity_cwd、public_tab_numbers、next_public_tab_number、tabs、active_tab を保存。
- tab は custom_name、layout、pane_order、数値文字列をキーにした panes、zoomed、focused、root_pane を保存。
- layout は `{"Pane":番号}` または `{"Split":{"direction":"Vertical|Horizontal","ratio":比率,"first":木,"second":木}}`。
- pane は global_pane_number、cwd と任意の label、title、agent_name、managed_agent_kind、launch_argv、agent_session を保存。
- agent_session は source、agent、kind、value。会話の正確な参照はここに記録される。
- agent-session-snapshot.json は saved_at と agents 配列。各行は pane_id、short_pane_id、workspace_id、cwd、agent、session_id、last_activity、launch_flags。
- agent-session-ledger.json は entries マップ。workspace_id・tab_id・pane_id・cwd と agent・session_id・source・observed_at・title を持つ。
- 観察時点では12スペース・12タブ・24pane。実行中の herdr は herdr.sock / herdr-client.sock を持つ。

## 実装と確認の対応

既存の gtx 保存形式へ変換し、既存の model.Restore と正確な ResumeCommand を使う。
既存の API の引数・出力は変更しない。取り込み専用の追加コマンドと追加メソッドに閉じる。
停止中は gtx セッションのロックを保持して既存保存内容へ追加し、稼働中は app loop 内で既存 pane を保ったまま追加する。
保存参照が曖昧・不正なら shell と理由を一覧に示す。壊れた構造・未知フィールド・未対応版は書き込み前に拒否する。

取り込み一覧、確認キャンセル、dry-run 無変更、構造・フォーカス対応、追加時の採番、正確 ID の argv、herdr 接続/ロック時の shell、エラー時の無変更をテストする。

## zoom の追加許可

親の追加指示（2026-10-14）により、gtx のタブ保存へ省略可能な `zoomed`（zoom 対象 pane ID）を追加する。
既存項目の意味・名前・版番号は変更しない。欠落・0 は zoom なし。
herdr の `zoomed=true` は保存時に focused が指す pane を zoom 対象として変換する。
取り込み時の採番で zoom 対象も置き換える。gtx 自身の保存・再起動復元、旧ファイル互換を確認する。

## 証拠の再現

`go build -o .artifacts/import-herdr/gtx ./cmd/gtx` の後、
`python3 tests/import_herdr/verify_live.py --binary .artifacts/import-herdr/gtx --source .artifacts/import-herdr/source --output .artifacts/import-herdr` を使う。
入力ファイルを一時フォルダへ再度写してから検証する。全 agent 起動を偽実行ファイルに限定し、元の state には書かない。
画面は実際の gtx クライアントを PTY へ接続して ANSI を保存し、保存データの全構造・比率・cwd・focus・zoom を機械比較する。
