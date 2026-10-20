package tui

import (
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/render"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
)

func TestStage4ReviewCollapsedWorkspaceOverflow(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	for i := 1; i < 60; i++ {
		s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	}
	a := NewApp(s, runtime.NewSupervisor(), nil, t.TempDir())
	a.SetServerUI()
	a.sidebarScroll = 3
	expanded := a.sidebarView()
	s.SidebarVisible = false
	compact := a.sidebarView()
	if expanded.Workspace.Total != compact.Workspace.Total || expanded.Workspace.Offset != compact.Workspace.Offset || expanded.Workspace.Rect.Y != compact.Workspace.Rect.Y {
		t.Fatal("collapse changed workspace viewport")
	}
	var target int
	for _, pos := range compact.Rows {
		if !pos.Right && pos.Row.Kind == render.RowWorkspace {
			target = pos.Row.Ws
			a.sidebarClick(uv.Mouse{X: 0, Y: pos.Y, Button: uv.MouseLeft})
			a.sidebarGestureMouse(input.Event{Kind: input.KindMouseRelease, Mouse: uv.Mouse{X: 0, Y: pos.Y}})
			break
		}
	}
	if s.Active != target {
		t.Fatalf("expanded row selected wrong workspace: %d want %d", s.Active, target)
	}
	for range 100 {
		a.sidebarWheel(uv.Mouse{X: 0, Y: 10}, false)
	}
	v := a.sidebarView().Workspace
	if a.sidebarScroll != max(0, v.Total-v.Rect.H) {
		t.Fatalf("cannot reach tail: %d view=%+v", a.sidebarScroll, v)
	}
	before := s.Active
	a.sidebarClick(uv.Mouse{X: 1, Y: 25, Button: uv.MouseLeft})
	if s.Active != before {
		t.Fatal("divider selected workspace")
	}
}
