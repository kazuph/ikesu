package render

import (
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
)

func TestStage4ReviewStrippedTitleTransitions(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	panes := map[int]*PaneSnap{
		1: {Agent: "codex", Status: StatusGlyph{Key: "blocked"}},
		2: {Agent: "codex", Title: "SECOND_ROW", Status: StatusGlyph{Key: "idle"}},
	}
	cfg := config.Config{}
	cfg.UI.AgentPanelSort = "priority"
	cfg.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "terminal_title_stripped"}}}
	for _, title := range []string{"⠋", "⠋ Task", "⠋"} {
		panes[1].Title = title
		rows := SidebarRows(s, panes, cfg)
		var agents []SidebarRow
		for _, row := range rows {
			if row.Kind == RowAgent {
				agents = append(agents, row)
			}
		}
		want := 1
		if title == "⠋ Task" {
			want = 2
		}
		if len(agents) != want || agents[len(agents)-1].Pane != 2 {
			t.Errorf("title %q gives agent rows %+v; want %d with SECOND_ROW last", title, agents, want)
		}
		in := Input{State: s, Panes: panes, SidebarConfig: cfg, ServerUI: true}
		for _, collapsed := range []bool{false, true} {
			s.SidebarVisible = !collapsed
			positions, start := SidebarViewForInput(in).Rows, 26
			if collapsed {
				positions, start = CollapsedAgentRows(in), 24
			}
			found := false
			for _, pos := range positions {
				if pos.Row.Kind == RowAgent && pos.Row.Pane == 2 {
					found = true
					if pos.Y != start+want-1 {
						t.Errorf("title %q collapsed=%t next agent Y=%d want=%d", title, collapsed, pos.Y, start+want-1)
					}
				}
			}
			if !found {
				t.Errorf("next agent missing with title %q collapsed=%t", title, collapsed)
			}
		}
	}
}
