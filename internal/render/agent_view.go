package render

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/agentview"
	"github.com/kazuph/goterrace/internal/model"
)

func projectAgentRows(s *model.AppState, panes map[int]*PaneSnap, rows []SidebarRow) []SidebarRow {
	if s.AgentView == nil {
		return rows
	}
	positions := map[int][3]uint64{}
	for wi, ws := range s.Workspaces {
		for ti, tab := range ws.Tabs {
			for pi, pid := range tab.Layout.PaneIDs() {
				positions[pid] = [3]uint64{uint64(wi), uint64(ti), uint64(pi)}
			}
		}
	}
	candidates := make([]agentview.Row, 0, len(rows))
	original := map[int]SidebarRow{}
	for _, row := range rows {
		pane := panes[row.Pane]
		pos := positions[row.Pane]
		ws := s.Workspaces[row.Ws]
		tab := ws.Tabs[pos[1]]
		fields := map[string]any{"status": pane.Status.Key, "agent": pane.Agent, "seen": pane.Seen, "state_change_seq": pane.StateChangeSeq, "workspace_id": fmt.Sprintf("s%d", ws.ID), "tab_id": fmt.Sprintf("s%d:t%d", ws.ID, tab.ID), "pane_id": fmt.Sprintf("p%d", row.Pane), "workspace_order": pos[0], "tab_order": pos[1], "pane_order": pos[2], "attention": uint64(agentRank(pane.Status.Key))}
		for key, value := range pane.Tokens {
			fields["token."+key] = value
		}
		candidates = append(candidates, agentview.Row{Pane: row.Pane, Fields: fields})
		original[row.Pane] = row
	}
	context := map[string]string{}
	if ws := s.ActiveWorkspace(); ws != nil {
		context["current_workspace_id"] = fmt.Sprintf("s%d", ws.ID)
		if tab := ws.TabRef(); tab != nil {
			context["current_tab_id"] = fmt.Sprintf("s%d:t%d", ws.ID, tab.ID)
		}
	}
	projected := s.AgentView.Project(candidates, context)
	out := make([]SidebarRow, 0, len(projected))
	for _, row := range projected {
		out = append(out, original[row.Pane])
	}
	return out
}
