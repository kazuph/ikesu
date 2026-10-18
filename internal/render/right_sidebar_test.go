package render

import (
	"github.com/kazuph/goterrace/internal/model"
	"testing"
)

func TestRightSidebarStatusColumnsAndPanelOwnership(t *testing.T) {
	for _, server := range []bool{false, true} {
		s := model.New("/tmp", 160, 35)
		s.Apply(model.CmdNewWorkspace{Cwd: "/tmp/next"})
		ids := []int{s.Workspaces[0].TabRef().Layout.Focused(), s.FocusedPane()}
		in := Input{State: s, ServerUI: server, Panes: map[int]*PaneSnap{
			ids[0]: {Agent: "codex", Status: StatusGlyph{Key: "blocked", Rune: "?", Color: Red}},
			ids[1]: {Agent: "codex", Status: StatusGlyph{Key: "working", Rune: "●", Color: Green}},
		}, Jobs: []JobRow{{Label: "build", Color: Green}}}
		v := SidebarViewForInput(in)
		for _, pos := range v.Rows {
			if (pos.Row.Kind == RowAgent || pos.Row.Kind == RowSep) && !pos.Right {
				t.Fatal("agent in left sidebar")
			}
		}
		f := Compose(in).Frame
		right := s.RightSidebarArea()
		if f.Cell(right.X, 0).Content != "│" {
			t.Fatal("right boundary missing")
		}
		if f.Cell(2, 0).Content != "p" && f.Cell(2, 0).Content != "s" {
			t.Fatal("spaces title missing")
		}
		s.Apply(model.CmdToggleSidebar{})
		s.Apply(model.CmdToggleRightSidebar{})
		f = Compose(in).Frame
		if f.Cell(0, 0).Content != "?" || f.Cell(0, 1).Content != "●" {
			t.Fatal("workspace status column")
		}
		right = s.RightSidebarArea()
		for y, want := range []string{"?", "●", "─", "●"} {
			if f.Cell(right.X+1, y).Content != want {
				t.Fatalf("server=%v row=%d: %q", server, y, f.Cell(right.X+1, y).Content)
			}
		}
		in.JobsPanel = true
		s.Apply(model.CmdToggleRightSidebar{})
		s.Apply(model.CmdToggleRightSidebar{})
		v = SidebarViewForInput(in)
		foundSpace, foundJob := false, false
		for _, p := range v.Rows {
			if p.Row.Kind == RowWorkspace {
				foundSpace = true
			}
			if p.Row.Kind == RowJob && p.Right {
				foundJob = true
			}
		}
		if !foundSpace || !foundJob {
			t.Fatal("jobs replaced workspace panel")
		}
	}
}
