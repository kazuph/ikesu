package render

import "github.com/kazuph/goterrace/internal/config"

// The divider row is not a workspace row in the connected collapsed strip.
func CollapsedWorkspaceViewport(in Input) SidebarViewport {
	area := in.State.SidebarArea()
	area.W = max(0, area.W-1)
	area.H = max(0, area.H-1)
	if in.ServerUI {
		area.H = max(0, SidebarSplitY(in.State)-area.Y)
	}
	v := SidebarViewport{Rect: area, Total: len(in.State.Workspaces)}
	v.Offset = max(0, min(in.SidebarScroll, max(0, v.Total-v.Rect.H)))
	return v
}

// CollapsedAgentRows uses the same ordering and card rows as the expanded panel.
func CollapsedAgentRows(in Input) []SidebarPosition {
	rows, viewport := collapsedAgents(in)
	var out []SidebarPosition
	for n := 0; n < viewport.Rect.H && viewport.Offset+n < len(rows); n++ {
		out = append(out, SidebarPosition{Row: rows[viewport.Offset+n], Y: viewport.Rect.Y + n})
	}
	return out
}

func CollapsedAgentViewport(in Input) SidebarViewport {
	_, viewport := collapsedAgents(in)
	return viewport
}

func collapsedAgents(in Input) ([]SidebarRow, SidebarViewport) {
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
	bottom := area.Y + area.H - 1
	area.Y = SidebarSplitY(s) + 1
	area.W = max(0, area.W-1)
	area.H = max(0, bottom-area.Y)
	v := SidebarViewport{Rect: area, Total: len(rows)}
	v.Offset = max(0, min(in.AgentScroll, max(0, v.Total-v.Rect.H)))
	return rows, v
}
