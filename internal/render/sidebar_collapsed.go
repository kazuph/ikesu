package render

// The divider row is not a workspace row in the connected collapsed strip.
func CollapsedWorkspaceViewport(in Input) SidebarViewport {
	return SidebarViewForInput(in).Workspace
}

func CollapsedAgentRows(in Input) []SidebarPosition {
	var rows []SidebarPosition
	for _, pos := range SidebarViewForInput(in).Rows {
		if pos.Right {
			rows = append(rows, pos)
		}
	}
	return rows
}

func CollapsedAgentViewport(in Input) SidebarViewport {
	return SidebarViewForInput(in).Agents
}
