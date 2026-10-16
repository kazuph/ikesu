package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
)

func TestStage4CollapsedAgentClickSelectsItsPane(t *testing.T) {
	a := specRuntimeApp(t)
	s := a.st
	s.Cols, s.Rows = 160, 45
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	s.SidebarVisible = false
	s.AgentScope = "sort"
	a.SetServerUI()
	a.SetReported(1, "codex", "idle")
	a.SetReported(2, "codex", "blocked")
	for _, y := range []int{24, 25} {
		s.Active = 0
		a.sidebarClick(uv.Mouse{X: 1, Y: y, Button: uv.MouseLeft})
		if s.Active != 1 || a.focusedPane() != 2 {
			t.Fatalf("row %d selected workspace %d pane %d", y, s.Active, a.focusedPane())
		}
	}
	a.sidebarClick(uv.Mouse{X: 1, Y: 26, Button: uv.MouseLeft})
	if s.Active != 0 || a.focusedPane() != 1 {
		t.Fatal("second agent did not select first workspace")
	}
	a.sidebarClick(uv.Mouse{X: 1, Y: 30, Button: uv.MouseLeft})
	if s.Active != 0 {
		t.Fatal("blank lower-panel row changed workspace")
	}
}
