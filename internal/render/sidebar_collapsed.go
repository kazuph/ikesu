package render

import "github.com/kazuph/goterrace/internal/config"

// CollapsedAgentRows uses the same ordering and card rows as the expanded panel.
func CollapsedAgentRows(in Input) []SidebarPosition {
	s := in.State
	cfg := in.SidebarConfig
	if len(cfg.UI.Sidebar.Agents.Rows) == 0 {
		cfg.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "workspace"}}, {{Token: "agent"}}}
	}
	rows := SidebarRows(s, in.Panes, cfg)
	for i, row := range rows {
		if row.Kind == RowSep {
			rows = rows[i+1:]
			break
		}
	}
	area := s.SidebarArea()
	start := SidebarSplitY(s) + 1
	height := max(0, area.Y+area.H-1-start)
	offset := max(0, min(in.AgentScroll, max(0, len(rows)-height)))
	var out []SidebarPosition
	for n := 0; n < height && offset+n < len(rows); n++ {
		out = append(out, SidebarPosition{Row: rows[offset+n], Y: start + n})
	}
	return out
}
