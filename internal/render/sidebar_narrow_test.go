package render

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
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
		for _, pos := range SidebarViewForInput(in).Rows {
			if pos.Row.Kind == RowWorkspace && f.Cell(min(1, width-2), pos.Y).Content != "●" {
				t.Fatalf("width=%d workspace status missing at original row", width)
			}
			if pos.Right && pos.Row.Kind == RowAgent && pos.Row.Line == 0 && f.Cell(right.X+min(2, width-1), pos.Y).Content != "●" {
				t.Fatalf("width=%d agent status missing at original row", width)
			}
		}
	}
}

func TestNarrowSidebarPrefersTitleOverLabel(t *testing.T) {
	s := model.New(t.TempDir(), 160, 30)
	s.RightSidebarSize = 10
	s.RightSidebarMode = "compact"
	in := Input{State: s, Panes: map[int]*PaneSnap{1: {Agent: "codex", Title: "TITLE", Label: "LABEL", Status: StatusGlyph{Rune: "●", Key: "idle"}}}}
	in.SidebarConfig.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "pane"}}}
	f := Compose(in).Frame
	var text strings.Builder
	for x := s.RightSidebarArea().X + 1; x < 159; x++ {
		text.WriteString(f.Cell(x, 2).Content)
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
	if len(rows) != 2 || !rows[0].header || rows[1].row.Kind != RowAgent {
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

			text := ""
			for ; x < 159; x++ {
				text += frame.Cell(x, y).Content
			}
			if strings.TrimSpace(text) != want {
				t.Fatalf("width %d: got %q want %q", width, text, want)
			}
		}
	}
}

func TestNarrowSidebarReferenceDoesNotReapplyTerminalOrConfiguredWidthLimits(t *testing.T) {
	for _, cols := range []int{80, 100} {
		for _, maximum := range []int{10, 72} {
			s := model.New(t.TempDir(), cols, 30)
			s.SidebarMin, s.SidebarMax = 1, maximum
			s.SidebarVisible, s.SidebarSize = true, min(26, maximum)
			s.RightSidebarMode, s.RightSidebarSize = "compact", 2
			before := *s
			frame := Compose(Input{State: s}).Frame
			if frame.W != cols || s.Cols != before.Cols || s.SidebarArea() != before.SidebarArea() || s.RightSidebarArea() != before.RightSidebarArea() {
				t.Fatal("reference render changed live geometry")
			}
		}
	}
}

func TestNarrowSidebarSingleMachineKeepsStandaloneCells(t *testing.T) {
	in := continuousInput(t, true, false)
	for _, width := range []int{18, 17, 5, 3, 2} {
		continuousWidth(in, width)
		plain := Compose(in).Frame
		m := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Input: &in}}}
		got := ComposeMachines(in, &m).Frame
		for _, area := range []model.Rect{in.State.SidebarArea(), in.State.RightSidebarArea()} {
			for y := area.Y; y < area.Y+area.H; y++ {
				for x := area.X; x < area.X+area.W; x++ {
					if !reflect.DeepEqual(got.Cell(x, y), plain.Cell(x, y)) {
						t.Fatal("single machine changed sidebar cell", width, x, y)
					}
				}
			}
		}
	}
}
