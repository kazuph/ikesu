package render

import (
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
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
	if len(positions) != 3 {
		t.Fatalf("got %d card rows, want 3", len(positions))
	}
	for i, wantPane := range []int{2, 1} {
		if positions[i].Y != i || positions[i].Row.Pane != wantPane || positions[i].Row.Line != 0 {
			t.Fatalf("row %d: %+v", i, positions[i])
		}
	}
	out := Compose(in)
	for _, check := range []struct {
		y    int
		text string
	}{{0, "●"}, {1, "○"}, {2, "─"}} {
		cell := out.Frame.Cell(s.RightSidebarArea().X+1, check.y)
		if cell.Content != check.text || cell.Style.Bg != nil {
			t.Fatalf("cell (1,%d): %+v", check.y, cell)
		}
	}
}
