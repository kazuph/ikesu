package render

// LegacyRightSidebarRows is the compact display protocol used by peers that
// do not advertise sidebar_continuous: one row per agent, starting at y=0.
func LegacyRightSidebarRows(in Input) []SidebarRow {
	rows := sidebarAgentTargets(in.State, in.Panes, in.SidebarConfig)
	rows = append(rows, SidebarRow{Kind: RowSep})
	for i := range in.Jobs {
		rows = append(rows, SidebarRow{Kind: RowJob, Line: i})
	}
	return rows
}
