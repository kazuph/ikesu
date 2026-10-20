package render

import (
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/model"
)

func TestNarrowSidebarNamesStayWithinDividers(t *testing.T) {
	for _, width := range []int{2, 3, 5, 10, 17, 18} {
		s := model.New(t.TempDir(), 160, 30)
		s.Workspaces[0].Name = "workspace-name-long"
		s.Workspaces[0].CustomName = true
		s.SidebarSize, s.RightSidebarSize = width, width
		s.SidebarVisible = width >= 18
		if width < 18 {
			s.RightSidebarMode = "compact"
		}
		in := Input{State: s, ServerUI: true, Panes: map[int]*PaneSnap{1: {Agent: "codex", Label: "agent-name-long", Status: StatusGlyph{Rune: "●", Key: "idle"}}}}
		f := Compose(in).Frame
		left, right := s.SidebarArea(), s.RightSidebarArea()
		for y := 0; y < 30; y++ {
			if f.Cell(left.W-1, y).Content != "│" || f.Cell(right.X, y).Content != "│" {
				t.Fatalf("width=%d row=%d divider overwritten", width, y)
			}
		}
		if width < 18 {
			l, r := "", ""
			for x := 1; x < width-1; x++ {
				l += f.Cell(x, 0).Content
				r += f.Cell(right.X+x+1, 0).Content
			}
			if l != "workspace-name-long"[:width-2] || r != "agent-name-long"[:min(width-2, len("agent-name-long"))]+strings.Repeat(" ", max(0, width-2-len("agent-name-long"))) {
				t.Fatalf("width=%d names left=%q right=%q", width, l, r)
			}
			if f.Cell(right.X+1, 0).Content != "●" {
				t.Fatal("status missing")
			}
		}
	}
}
