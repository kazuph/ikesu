package render

import (
	"github.com/kazuph/goterrace/internal/model"
	"strings"
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
		for _, pos := range SidebarViewForInput(in).Rows {
			if pos.Row.Kind == RowWorkspace {
				want := []string{"?", "●"}[pos.Row.Ws]
				if f.Cell(0, pos.Y).Content != want {
					t.Fatalf("workspace row %d status", pos.Y)
				}
			}
			if pos.Row.Kind == RowAgent && pos.Row.Line == 0 {
				want := in.Panes[pos.Row.Pane].Status.Rune
				if f.Cell(159, pos.Y).Content != want {
					t.Fatalf("agent row %d status", pos.Y)
				}
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

func rightSidebarLine(f *Frame, s *model.AppState, y int) string {
	a := s.RightSidebarArea()
	var text strings.Builder
	for x := a.X + 1; x < a.X+a.W; x++ {
		c := f.Cell(x, y)
		if c != nil && c.Width > 0 {
			text.WriteString(c.Content)
		}
	}
	return text.String()
}
