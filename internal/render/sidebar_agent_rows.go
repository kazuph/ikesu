package render

import (
	"strings"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/detect"
	"github.com/kazuph/goterrace/internal/model"
)

func hasConfiguredAgentRows(cfg config.Config, agent string) bool {
	_, specific := cfg.UI.Sidebar.Agents.RowsByAgent[agent]
	return cfg.UI.Sidebar.Agents.Rows != nil || specific
}

func visibleAgentRows(s *model.AppState, panes map[int]*PaneSnap, r SidebarRow, rows [][]config.SidebarToken) [][]config.SidebarToken {
	sn := panes[r.Pane]
	var visible [][]config.SidebarToken
	for _, row := range rows {
		for _, token := range row {
			text := ""
			switch token.Token {
			case "state_icon":
				text = sn.Status.Rune
			case "state_text":
				text = sn.Status.Key
			case "pane":
				text = sn.Title + sn.Label + s.PaneLabels[r.Pane]
			case "workspace":
				text = WorkspaceDisplayName(s.Workspaces[r.Ws], panes)
			case "tab":
				for _, tab := range s.Workspaces[r.Ws].Tabs {
					if tab.Layout.HasPane(r.Pane) {
						text = tab.TabLabel()
					}
				}
			case "agent":
				text = sn.Agent + sn.DisplayAgent
			case "terminal_title":
				text = sn.Title
			case "terminal_title_stripped":
				text = detect.StripTerminalTitle(sn.Title)
			default:
				if strings.HasPrefix(token.Token, "$") {
					text = sn.Tokens[strings.TrimPrefix(token.Token, "$")]
				}
			}
			if strings.TrimSpace(text) != "" {
				visible = append(visible, row)
				break
			}
		}
	}
	return visible
}
