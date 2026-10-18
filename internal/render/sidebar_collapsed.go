package render

// The divider row is not a workspace row in the connected collapsed strip.
func CollapsedWorkspaceViewport(in Input) SidebarViewport {
	area := in.State.SidebarArea()
	area.W = max(0, area.W-1)

	v := SidebarViewport{Rect: area, Total: len(in.State.Workspaces)}
	v.Offset = max(0, min(in.SidebarScroll, max(0, v.Total-v.Rect.H)))
	return v
}

// CollapsedAgentRows uses the same ordering and card rows as the expanded panel.
func CollapsedAgentRows(in Input) []SidebarPosition {
	rows, viewport := collapsedAgents(in)
	var out []SidebarPosition
	for n := 0; n < viewport.Rect.H && viewport.Offset+n < len(rows); n++ {
		out = append(out, SidebarPosition{Right: true, Row: rows[viewport.Offset+n], Y: viewport.Rect.Y + n})
	}
	return out
}

func CollapsedAgentViewport(in Input) SidebarViewport {
	_, viewport := collapsedAgents(in)
	return viewport
}

func collapsedAgents(in Input) ([]SidebarRow, SidebarViewport) {
	rows := RightCompactRows(in)
	area := in.State.RightSidebarArea()
	area.X++
	area.W = max(0, area.W-1)
	v := SidebarViewport{Rect: area, Total: len(rows)}
	v.Offset = max(0, min(in.AgentScroll, max(0, v.Total-v.Rect.H)))
	return rows, v
}
