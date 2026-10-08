# サイドバー設定

サイドバーは設定ファイル（既定 `~/.config/gtx/config.toml`、`GTX_CONFIG` で変更）を読みます。設定例の最大幅は72列です。

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

`gtx --default-config` は上記の既定設定例を標準出力へ出します。

`[ui.sidebar.agents]` / `[ui.sidebar.spaces]` の `rows` は文字列トークン、または `{ token = "workspace", fg = "#89b4fa", bold = true, dim = false }` の配列です。`fg` は `#RGB` または `#RRGGBB`、省略した属性は既定の色・太字・DIMを保持し、同じトークンを複数回出しても指定はその出現だけに適用します。`row_gap` は各項目の後の空行数です。agent別行は `rows_by_agent` へ指定できます。

workspaceの独自トークンは `$名前` で表示します。`gtx workspace report-metadata s1 --source build --token phase=compile --seq 1 --ttl-ms 1000` で更新し、`--clear-token phase` で消します。未対応のトークンを指定した設定は明示的なエラーにします。
