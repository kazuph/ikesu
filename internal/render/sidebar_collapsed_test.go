package render

import (
	"testing"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
)

func TestStage4RightCollapsedAgentsKeepPriorityAndSingleStatusCells(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	s.RightSidebarMode = "compact"
	s.AgentScope = "sort"
	cfg := config.Config{}
	cfg.UI.TransparentSidebarCards = true
	cfg.UI.AgentPanelSort = "priority"
	panes := map[int]*PaneSnap{
		1: {Agent: "codex", Status: StatusGlyph{Key: "idle", Rune: "○"}},
		2: {Agent: "codex", Status: StatusGlyph{Key: "blocked", Rune: "●"}},
	}
	in := Input{State: s, ServerUI: true, SidebarConfig: cfg, Panes: panes}
	positions := CollapsedAgentRows(in)
	if len(positions) != 5 {
		t.Fatalf("got %d card rows, want 5", len(positions))
	}
	for i, wantPane := range []int{2, 1} {
		if positions[1+2*i].Y != 2+2*i || positions[1+2*i].Row.Pane != wantPane || positions[1+2*i].Row.Line != 0 {
			t.Fatalf("row %d: %+v", i, positions[i])
		}
	}
	out := Compose(in)
	for _, check := range []struct {
		y    int
		text string
	}{{0, "["}, {1, " "}, {2, "●"}, {3, " "}, {4, "○"}, {5, " "}} {
		cell := out.Frame.Cell(s.RightSidebarArea().X+1, check.y)
		if cell.Content != check.text || check.y >= 2 && cell.Style.Bg != nil {
			t.Fatalf("cell (1,%d): %+v", check.y, cell)
		}
	}
}
