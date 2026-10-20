package render

import (
	"testing"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
)

func TestSidebarOverflowReservesTextScrollbarAndStatusCells(t *testing.T) {
	in := continuousInput(t, false, false)
	in.State.Rows = 12
	in.State.AgentScope = "all"
	in.SidebarConfig.UI.Sidebar.Spaces.Rows = [][]config.SidebarToken{{{Token: "state_icon"}, {Token: "workspace"}}}
	in.SidebarConfig.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "state_icon"}, {Token: "workspace"}}}
	for i := 0; i < 16; i++ {
		in.State.Apply(model.CmdNewWorkspace{Cwd: "/tmp/LONG_WORKSPACE_NAME_ABCDEFGHIJKLMNOPQRSTUVWXYZ", NoFocus: true})
		wi := len(in.State.Workspaces) - 1
		pid := in.State.Workspaces[wi].TabRef().Layout.Focused()
		in.Panes[pid] = &PaneSnap{Agent: "codex", Status: StatusGlyph{Rune: "●"}}
	}
	for _, ws := range in.State.Workspaces {
		ws.Name, ws.CustomName = "LONG_WORKSPACE_NAME_ABCDEFGHIJKLMNOPQRSTUVWXYZ", true
	}
	for _, width := range []int{26, 18} {
		continuousWidth(in, width)
		f := Compose(in).Frame
		view := SidebarViewForInput(in)
		for _, right := range []bool{false, true} {
			v := view.Workspace
			if right {
				v = view.Agents
			}
			if v.Total <= v.Rect.H {
				t.Fatal("fixture did not overflow", right)
			}
			found := false
			for _, pos := range view.Rows {
				if pos.Right != right || pos.Row.Kind != RowWorkspace && pos.Row.Kind != RowAgent {
					continue
				}
				bar := v.Rect.X + v.Rect.W - 1
				if got := f.Cell(bar-1, pos.Y).Content; got != "…" {
					t.Fatalf("width=%d right=%t text tail=%q; scrollbar overwrote text budget", width, right, got)
				}
				if got := f.Cell(bar, pos.Y).Content; got != "│" && got != "█" {
					t.Fatal("missing scrollbar", width, right, got)
				}
				if got := f.Cell(v.Rect.X+1, pos.Y).Content; got == "" || got == " " || got == "│" || got == "█" {
					t.Fatal("status cell lost", width, right, got)
				}
				found = true
				break
			}
			if !found {
				t.Fatal("card row missing", right)
			}
		}
	}
}
