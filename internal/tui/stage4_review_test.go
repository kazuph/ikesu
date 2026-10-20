package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestStage4ReviewCollapsedWorkspaceOverflow(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	for i := 1; i < 60; i++ {
		s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	}
	s.SidebarVisible = false
	a := NewApp(s, runtime.NewSupervisor(), nil, t.TempDir())
	a.SetServerUI()
	a.sidebarScroll = 3
	a.sidebarClick(uv.Mouse{X: 0, Y: 0, Button: uv.MouseLeft})
	if s.Active != 3 {
		t.Errorf("visible workspace index 3 clicked index %d", s.Active)
	}
	for range 10 {
		a.sidebarWheel(uv.Mouse{X: 0, Y: 10}, false)
	}
	if want := len(s.Workspaces) - s.Rows; a.sidebarScroll != want {
		t.Fatalf("upper viewport cannot reach tail: offset=%d want=%d", a.sidebarScroll, want)
	}
	upper := a.sidebarScroll
	a.revealServerWorkspace()
	if a.sidebarScroll != upper {
		t.Fatal("collapsed selection changed upper offset using expanded card heights")
	}
	a.sidebarWheel(uv.Mouse{X: 0, Y: 25}, false)
	if a.sidebarScroll != upper {
		t.Fatal("wheel at the bottom moved beyond the final workspace")
	}
	a.sidebarClick(uv.Mouse{X: 1, Y: 25, Button: uv.MouseLeft})
	if s.Active != 3 {
		t.Fatal("divider selected a workspace")
	}
}
