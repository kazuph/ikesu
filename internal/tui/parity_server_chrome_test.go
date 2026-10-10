package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestServerSlimCardGitLineSelectsItsWorkspace(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	s.WorkspaceDensity = "slim"
	a := NewApp(s, runtime.NewSupervisor(), nil, t.TempDir())
	a.SetServerUI()
	for _, pos := range a.sidebarView().Rows {
		if pos.Row.Kind != render.RowGit || pos.Row.Ws != 1 {
			continue
		}
		sidebarEvent(a, input.KindMouseClick, 3, pos.Y)
		sidebarEvent(a, input.KindMouseRelease, 3, pos.Y)
		if s.Active != 1 {
			t.Fatal("slim git-line click did not select its workspace")
		}
		return
	}
	t.Fatal("second workspace git line not found")
}

func TestServerHiddenSidebarControlsIgnoreClicks(t *testing.T) {
	s := model.New("/tmp", 160, 45)
	a := NewApp(s, runtime.NewSupervisor(), nil, "/tmp")
	a.SetServerUI()
	width, density, scope := s.SidebarArea().W, s.WorkspaceDensity, s.AgentScope
	for _, pos := range [][2]int{{20, 0}, {20, render.SidebarSplitY(s) + 1}, {24, 44}} {
		a.sidebarClick(uv.Mouse{X: pos[0], Y: pos[1], Button: uv.MouseLeft})
		if !s.SidebarVisible || s.SidebarArea().W != width || s.WorkspaceDensity != density || s.AgentScope != scope {
			t.Fatalf("hidden control acted at %v", pos)
		}
	}
}

func TestServerSidebarWidthButtonCyclesRecordedPresets(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	a := NewApp(s, runtime.NewSupervisor(), nil, t.TempDir())
	a.SetServerUI()
	for _, want := range []string{" WIDE ", " NARROW ", " NORMAL "} {
		a.sidebarClick(uv.Mouse{X: 0, Y: 44, Button: uv.MouseLeft})
		if s.PresetLabel() != want {
			t.Fatalf("width click: got %q, want %q", s.PresetLabel(), want)
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
