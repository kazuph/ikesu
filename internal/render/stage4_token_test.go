package render

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
)

func TestStage4ConfiguredAgentDimSurvivesDrawing(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	on, off := true, false
	cfg := config.Config{}
	cfg.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{
		{{Token: "agent", Dim: &on}}, {{Token: "agent", Dim: &off}},
	}
	in := Input{State: s, ServerUI: true, SidebarConfig: cfg,
		Panes: map[int]*PaneSnap{1: {Agent: "codex"}}}
	out := Compose(in)
	for _, pos := range SidebarViewForInput(in).Rows {
		if pos.Row.Kind != RowAgent {
			continue
		}
		found := false
		for x := s.RightSidebarArea().X + 1; x < s.Cols; x++ {
			cell := out.Frame.Cell(x, pos.Y)
			if cell.Content == "c" {
				found = true
				if faint := cell.Style.Attrs&uv.AttrFaint != 0; faint != (pos.Row.Line == 0) {
					t.Fatalf("line %d: faint=%t", pos.Row.Line, faint)
				}
			}
		}
		if !found {
			t.Fatalf("line %d: agent text missing", pos.Row.Line)
		}
	}
}

func TestStage4CustomAgentUsesOnlyRequestedTokensAndOmitsEmptyRows(t *testing.T) {
	s := model.New("/tmp/project", 160, 45)
	s.Workspaces[0].Name, s.Workspaces[0].CustomName = "project", true
	s.PaneLabels = map[int]string{1: "CUSTOM_LABEL"}
	for _, token := range []string{"pane", "workspace"} {
		cfg := config.Config{}
		cfg.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: token}}, {{Token: "$absent"}}}
		in := Input{State: s, ServerUI: true, SidebarConfig: cfg,
			Panes: map[int]*PaneSnap{1: {Agent: "codex", Title: "TITLE", Label: "codex", Status: StatusGlyph{Rune: "○"}}}}
		out := Compose(in)
		count := 0
		for _, pos := range SidebarViewForInput(in).Rows {
			if pos.Row.Kind != RowAgent {
				continue
			}
			count++
			line := rightSidebarLine(out.Frame, s, pos.Y)
			want := "project"
			if token == "pane" {
				want = "CUSTOM_LABEL"
			}
			if strings.TrimSpace(strings.TrimSuffix(line, "○")) != want || out.Frame.Cell(s.Cols-1, pos.Y).Content != "○" {
				t.Fatalf("%s: got %q want %q", token, line, want)
			}
		}
		if count != 1 {
			t.Fatalf("%s: %d rows, want 1", token, count)
		}
	}
}
