# サイドバー設定

サイドバーは設定ファイル（既定 `~/.config/ikesu/config.toml`、`IKESU_CONFIG` で変更）を読みます。設定例の最大幅は72列です。

```toml
[ui]
sidebar_min_width = 18
sidebar_max_width = 72
sidebar_width = 26
sidebar_start_collapsed = false
workspace_panel_density = "full"
agent_panel_scope = "all"
```

幅は global menu の `sidebar narrow` / `sidebar normal` / `sidebar wide`、agents footer の幅ボタン、divider dragで変更します。normal は設定値をmin/max内へ制限した幅、narrowはmin、wideはmaxです。minがmaxを超える設定は診断を表示し、実行時の境界を18〜72列へ戻します。設定再読込みでは現在の折りたたみ状態を保持します。

`workspace_panel_density` は `full` / `slim`、`agent_panel_scope` は `all` / `sort`。ヘッダーのトグルで設定へ保存します。slimでも名前とgit情報を2行に表示し、クリック対象は名前行のみです。worktreeの子は1行です。sortはBlocked、未確認Idle、Working、確認済みIdle、Unknownの順で、同順位の順序を保持します。旧 `current` 値はallとして扱います。

workspaceのsectionは `favorite` / `work` / `personal` / `none`。表示順はfavorites、work、personal、spacesです。空sectionは表示せず、折りたたんだsectionのworkspaceとagentを隠します。次／前workspace移動は表示順、番号選択は元の配列順です。

`ikesu --default-config` は上記の既定設定例を標準出力へ出します。

`[ui.sidebar.agents]` / `[ui.sidebar.spaces]` の `rows` は文字列トークン、または `{ token = "workspace", fg = "#89b4fa", bold = true, dim = false }` の配列です。`fg` は `#RGB` または `#RRGGBB`、省略した属性は既定の色・太字・DIMを保持し、同じトークンを複数回出しても指定はその出現だけに適用します。`row_gap` は各項目の後の空行数です。agent別行は `rows_by_agent` へ指定できます。

workspaceの独自トークンは `$名前` で表示します。`ikesu workspace report-metadata s1 --source build --token phase=compile --seq 1 --ttl-ms 1000` で更新し、`--clear-token phase` で消します。未対応のトークンを指定した設定は明示的なエラーにします。

`ui.mobile_width_threshold` は mobile layout に切り替わる最大の列数です。
既定は配布用ビルドの設定例と同じ `64`。指定値以下で desktop sidebar を
mobile header と switcher に置き換えます。`0` は mobile layout を無効にします。
mobile switcher の section header を tap すると折りたたみ／展開し、直後の空行は
操作対象にしません。workspace 選択後は switcher を閉じます。

agents の隣の `[jobs]` は jobs snapshot の全履歴を表示します。カードの dot は
既存の期限と runner 生存条件で絞り込みますが、jobs の履歴行は間引きません。

global menu の `settings` または `prefix+s` で、Theme → Sound → Toasts の順の
設定を開きます。↑↓で候補、Tabで節を選び、Enterで保存・適用して閉じます。
Escapeは保存せず閉じます。旧pane label toggleとIntegrationsは表示しません。
テーマは配布用ビルドと同じ13候補で、`[theme] name` へ保存します。
手動選択は `auto_switch = false` も保存します。音の選択は `[ui.sound] enabled`、
通知先は `[ui.toast] delivery = "off" | "ikesu" | "terminal" | "system"` へ保存します。

agent側の独自トークンも `$名前` で表示できます。
`ikesu pane report-metadata p1 --source build --token phase=compile --seq 1 --ttl-ms 1000`
は `pane.report_metadata` を呼びます。`--clear-token phase` で消去します。
`--title`、`--display-agent`、`--state-label STATUS=TEXT` と各clearフラグも対応します。
`--agent` / `--applies-to-source` は表示用metadataに適用し、token値はsourceによらず
最後の有効な更新を表示します。seqはsource単位、TTLは指定された値に適用します。
未対応の機能は明示的なエラーを返します。G8に従い更新確認・updater・update badge
とwhat's newは表示しません。
