package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestServerHiddenSidebarControlsIgnoreClicks(t *testing.T) {
	s := model.New("/tmp", 160, 45)
	a := NewApp(s, runtime.NewSupervisor(), nil, "/tmp")
	a.SetServerUI()
	width, density, scope := s.SidebarArea().W, s.WorkspaceDensity, s.AgentScope
	for _, pos := range [][2]int{{20, 0}, {20, render.SidebarSplitY(s) + 1}, {0, 44}, {24, 44}} {
		a.sidebarClick(uv.Mouse{X: pos[0], Y: pos[1], Button: uv.MouseLeft})
		if !s.SidebarVisible || s.SidebarArea().W != width || s.WorkspaceDensity != density || s.AgentScope != scope {
			t.Fatalf("hidden control acted at %v", pos)
		}
	}
}

func TestServerTabWheelConsumesWithoutCycling(t *testing.T) {
	s := model.New("/tmp", 90, 25)
	s.Apply(model.CmdNewTab{Workspace: 0})
	a := NewApp(s, runtime.NewSupervisor(), nil, "/tmp")
	a.SetServerUI()
	active := s.ActiveWorkspace().ActiveTab
	for _, button := range []uv.MouseButton{uv.MouseWheelUp, uv.MouseWheelDown} {
		a.wheel(uv.Mouse{X: s.TabBarArea().X + 1, Y: 0, Button: button})
		if s.ActiveWorkspace().ActiveTab != active {
			t.Fatal("the observed server tab wheel must leave focus unchanged")
		}
	}
}

func TestNewTabInitialNumberReplacedByFirstTypedText(t *testing.T) {
	for _, server := range []bool{false, true} {
		s := model.New("/tmp", 90, 25)
		a := NewApp(s, runtime.NewSupervisor(), nil, "/tmp")
		if server {
			a.SetServerUI()
		}
		a.newTabNameDialog()
		a.dialogKey(uv.Key{Code: 'T', Text: "TAB"})
		a.dialogKey(uv.Key{Code: '-', Text: "-2"})
		if a.dialog.d.Input != "TAB-2" {
			t.Fatal("new-tab typing retained its initial number", a.dialog.d.Input)
		}
		a.dialogKey(uv.Key{Code: uv.KeyEscape})
		a.newTabNameDialog()
		a.dialogKey(uv.Key{Code: uv.KeyBackspace})
		a.dialogKey(uv.Key{Code: 'x', Text: "x"})
		if a.dialog.d.Input != "x" {
			t.Fatal("backspace did not clear initial number")
		}
	}
}
