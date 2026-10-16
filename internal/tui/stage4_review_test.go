package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestStage4ReviewCollapsedWorkspaceOverflow(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	for i := 1; i < 30; i++ {
		s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	}
	s.SidebarVisible = false
	a := NewApp(s, runtime.NewSupervisor(), nil, t.TempDir())
	a.SetServerUI()
	a.sidebarScroll = 3
	a.sidebarClick(uv.Mouse{X: 1, Y: 0, Button: uv.MouseLeft})
	if s.Active != 3 {
		t.Errorf("visible workspace index 3 clicked index %d", s.Active)
	}
	for range 10 {
		a.sidebarWheel(uv.Mouse{X: 1, Y: 10}, false)
	}
	if want := 30 - render.SidebarSplitY(s); a.sidebarScroll != want {
		t.Fatalf("upper viewport cannot reach tail: offset=%d want=%d", a.sidebarScroll, want)
	}
	upper := a.sidebarScroll
	a.revealServerWorkspace()
	if a.sidebarScroll != upper {
		t.Fatal("collapsed selection changed upper offset using expanded card heights")
	}
	a.sidebarWheel(uv.Mouse{X: 1, Y: 25}, false)
	if a.sidebarScroll != upper {
		t.Fatal("lower wheel moved upper workspaces")
	}
	a.sidebarClick(uv.Mouse{X: 1, Y: render.SidebarSplitY(s), Button: uv.MouseLeft})
	if s.Active != 3 {
		t.Fatal("divider selected a workspace")
	}
}
