package render

import (
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
		for x := 0; x < s.SidebarArea().W-1; x++ {
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
