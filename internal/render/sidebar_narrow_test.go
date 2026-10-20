package render

import (
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/config"
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

func TestNarrowSidebarPrefersTitleOverLabel(t *testing.T) {
	s := model.New(t.TempDir(), 160, 30)
	s.RightSidebarSize = 10
	s.RightSidebarMode = "compact"
	in := Input{State: s, Panes: map[int]*PaneSnap{1: {Agent: "codex", Title: "TITLE", Label: "LABEL", Status: StatusGlyph{Rune: "●", Key: "idle"}}}}
	f := Compose(in).Frame
	var text strings.Builder
	for x := s.RightSidebarArea().X + 2; x < 160; x++ {
		text.WriteString(f.Cell(x, 0).Content)
	}
	if strings.TrimSpace(text.String()) != "TITLE" {
		t.Fatalf("narrow name=%q", text.String())
	}
}

func TestMachineNarrowSidebarUsesCompactRows(t *testing.T) {
	s := model.New(t.TempDir(), 160, 30)
	s.RightSidebarSize = 10
	s.RightSidebarMode = "compact"
	in := Input{State: s, Panes: map[int]*PaneSnap{1: {Agent: "codex", Title: "TITLE", Label: "LABEL", Status: StatusGlyph{Rune: "●", Key: "idle"}}}}
	m := MachineSidebar{Groups: []MachineGroup{{ID: "local", Label: "local", Input: &in}}}
	rows := m.rightRows(in)
	if len(rows) != 2 || rows[0].header || rows[0].row.Kind != RowAgent {
		t.Fatalf("narrow rows=%+v", rows)
	}
}

func TestSidebarPaneNameConsistentAcrossWidths(t *testing.T) {
	for _, manual := range []string{"", "MANUAL"} {
		s := model.New(t.TempDir(), 160, 30)
		s.PaneLabels = map[int]string{1: manual}
		in := Input{State: s, Panes: map[int]*PaneSnap{1: {Agent: "codex", Title: "TITLE", Label: "LABEL", Status: StatusGlyph{Rune: "●", Key: "idle"}}}}
		in.SidebarConfig.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "pane"}}}
		want := "TITLE"
		if manual != "" {
			want = manual
		}
		for _, width := range []int{10, 26} {
			s.RightSidebarSize = width
			s.RightSidebarMode = ""
			if width < 18 {
				s.RightSidebarMode = "compact"
			}
			frame := Compose(in).Frame
			y, x := 2, s.RightSidebarArea().X+1
			if width < 18 {
				y = 0
				x++
			}
			text := ""
			for ; x < 160; x++ {
				text += frame.Cell(x, y).Content
			}
			if strings.TrimSpace(text) != want {
				t.Fatalf("width %d: got %q want %q", width, text, want)
			}
		}
	}
}
