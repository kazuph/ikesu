package tui

import (
	"bytes"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
	"testing"
)

func TestRightSidebarViewerPreferencesIndependentAndRestored(t *testing.T) {
	st := model.New(t.TempDir(), 160, 45)
	app := NewApp(st, runtime.NewSupervisor(), &bytes.Buffer{}, "")
	first := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	second := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	var saved endpoint.ViewPreferences
	first.Configure(app, &config.Config{}, nil, func(p endpoint.ViewPreferences) error { saved = p; return nil })
	first.with(app, func(ui *App) {
		ui.exec(model.CmdToggleSidebar{})
		ui.exec(model.CmdToggleSidebar{})
		ui.exec(model.CmdToggleRightSidebar{})
	})
	if !saved.SidebarHidden || saved.RightSidebarMode != "compact" || !saved.SidebarCollapsed {
		t.Fatalf("saved=%+v", saved)
	}
	second.with(app, func(ui *App) {
		if !ui.st.SidebarVisible || ui.st.SidebarHidden || ui.st.RightSidebarMode != "" {
			t.Fatal("another viewer changed")
		}
	})
	restored := app.NewViewer(&bytes.Buffer{}, 160, 45, nil)
	restored.Configure(app, &config.Config{}, &saved, nil)
	restored.with(app, func(ui *App) {
		if ui.st.SidebarArea().W != 0 || ui.st.RightSidebarArea().W != 2 {
			t.Fatal("not restored")
		}
	})
}

func TestRightSidebarActualPTYGeometryAndClicks(t *testing.T) {
	for _, server := range []bool{false, true} {
		a := specRuntimeApp(t)
		a.st.Cols, a.st.Rows = 160, 35
		if server {
			a.SetServerUI()
		}
		if err := a.StartInitialPane(); err != nil {
			t.Fatal(err)
		}
		a.exec(model.CmdNewWorkspace{Cwd: a.cwd, NoFocus: true})
		a.SetReported(1, "codex", "idle")
		a.SetReported(2, "codex", "blocked")
		a.st.AgentScope = "sort"
		for left := 0; left < 3; left++ {
			for right := 0; right < 3; right++ {
				a.syncLayout()
				for _, pr := range a.st.PaneRectsForDisplay(a.st.ActiveWorkspace().TabRef(), a.st.PanesArea()) {
					w, h := model.PaneTermSize(pr.Rect, a.st.PaneBorders)
					p := a.sup.Get(pr.ID)
					command := fmt.Sprintf("set -- $(stty size); printf '\\nRS%d%d_%%s_%%s\\n' \"$1\" \"$2\"\r", left, right)
					if _, err := p.WriteToPTY([]byte(command)); err != nil {
						t.Fatal(err)
					}
					specWaitText(t, p, fmt.Sprintf("RS%d%d_%d_%d", left, right, h, w))
				}
				a.exec(model.CmdToggleRightSidebar{})
			}
			a.exec(model.CmdToggleSidebar{})
		}
		a.exec(model.CmdToggleRightSidebar{})
		x := a.st.RightSidebarArea().X + 1
		sidebarEvent(a, input.KindMouseClick, x, 0)
		if a.focusedPane() != 2 || a.st.Active != 1 {
			t.Fatal("compact priority click missed pane")
		}
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: x, Y: 1, Button: uv.MouseRight}})
		if a.menu == nil || a.focusedPane() != 1 || a.st.Active != 0 {
			t.Fatal("right-click menu must select its target pane before operating")
		}
		a.menu = nil
		a.exec(model.CmdToggleSidebar{})
		sidebarEvent(a, input.KindMouseClick, 0, 0)
		if a.st.Active != 0 {
			t.Fatal("compact workspace click missed")
		}
	}
}

func TestRightSidebarStandaloneRestoresSameLocalPreferences(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GTX_STATE_DIR", root)
	path := endpoint.ViewPreferencesPath(api.DefaultSession)
	first := NewApp(model.New(root, 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, root)
	if err := first.configureStandalonePreferences(); err != nil {
		t.Fatal(err)
	}
	first.exec(model.CmdToggleSidebar{})
	first.exec(model.CmdToggleSidebar{})
	first.exec(model.CmdToggleRightSidebar{})
	saved, err := endpoint.LoadViewPreferences(path)
	if err != nil || saved == nil || !saved.SidebarHidden || saved.RightSidebarMode != "compact" {
		t.Fatal("not saved", saved, err)
	}
	second := NewApp(model.New(root, 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, root)
	if err := second.configureStandalonePreferences(); err != nil {
		t.Fatal(err)
	}
	if second.st.SidebarArea().W != 0 || second.st.RightSidebarArea().W != 2 {
		t.Fatal("standalone restart did not restore")
	}
	second.st.Cols = 41
	second.saveViewPreferences()
	saved, err = endpoint.LoadViewPreferences(path)
	if err != nil || saved.RightSidebarMode != "compact" {
		t.Fatal("automatic hide leaked into preference", saved, err)
	}
}

func TestRightSidebarDefaultAndCustomKeyAndDividerCells(t *testing.T) {
	root := t.TempDir()
	a := NewApp(model.New(root, 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, root)
	if !a.configuredKey(uv.Key{Code: 'B'}, true) || a.st.RightSidebarMode != "compact" {
		t.Fatal("default prefix+shift+b missing")
	}
	if a.st.SidebarArea().W != 26 {
		t.Fatal("right key changed left sidebar")
	}
	c, err := config.Decode([]byte("[keys]\ntoggle_right_sidebar='prefix+t'\n"))
	if err != nil {
		t.Fatal(err)
	}
	a.sidebarConfig = c
	if a.configuredKey(uv.Key{Code: 'B'}, true) {
		t.Fatal("old default survives explicit rebind")
	}
	if !a.configuredKey(uv.Key{Code: 't'}, true) || a.st.RightSidebarMode != "hidden" {
		t.Fatal("custom key missing")
	}
	a.configuredKey(uv.Key{Code: 't'}, true)
	right := a.st.RightSidebarArea()
	sidebarEvent(a, input.KindMouseClick, right.X, 4)
	if a.st.RightSidebarArea().W != 2 {
		t.Fatal("right divider did not collapse")
	}
	sidebarEvent(a, input.KindMouseClick, a.st.RightSidebarArea().X, 4)
	if a.st.RightSidebarArea().W != 26 {
		t.Fatal("right divider did not expand")
	}
	sidebarEvent(a, input.KindMouseClick, 25, 4)
	sidebarEvent(a, input.KindMouseRelease, 25, 4)
	if a.st.SidebarArea().W != 2 {
		t.Fatal("left divider did not collapse")
	}
	if a.st.RightSidebarArea().W != 26 {
		t.Fatal("left divider changed right")
	}
}

func TestRightSidebarLeftCompactLastRowStillSelectsWorkspace(t *testing.T) {
	root := t.TempDir()
	s := model.New(root, 160, 4)
	for i := 1; i < 4; i++ {
		s.Apply(model.CmdNewWorkspace{Cwd: root, NoFocus: true})
	}
	a := NewApp(s, runtime.NewSupervisor(), &bytes.Buffer{}, root)
	a.exec(model.CmdToggleSidebar{})
	sidebarEvent(a, input.KindMouseClick, 0, 3)
	if s.Active != 3 || s.SidebarArea().W != 2 {
		t.Fatal("bottom status cell was intercepted by obsolete expand button")
	}
}

func TestRightSidebarCompactScrollAndJobCallerActualPTY(t *testing.T) {
	for _, server := range []bool{false, true} {
		a := specRuntimeApp(t)
		a.st.Cols, a.st.Rows = 160, 8
		if server {
			a.SetServerUI()
		}
		if err := a.StartInitialPane(); err != nil {
			t.Fatal(err)
		}
		for i := 1; i < 12; i++ {
			a.exec(model.CmdNewWorkspace{Cwd: a.cwd, NoFocus: true})
		}
		for _, ws := range a.st.Workspaces {
			a.SetReported(ws.TabRef().Layout.Focused(), "codex", "idle")
		}
		a.st.RightSidebarMode = "compact"
		a.st.AgentScope = "all"
		a.jobRows = []render.JobRow{{Label: "first", CallerPane: "p2"}, {Label: "second", CallerPane: "p1"}}
		x := a.st.RightSidebarArea().X + 1
		a.wheel(uv.Mouse{X: x, Y: 0, Button: uv.MouseWheelDown})
		sidebarEvent(a, input.KindMouseClick, x, 0)
		if a.agentScroll != 1 || a.focusedPane() != 2 {
			t.Fatal("compact scroll click missed the next agent")
		}
		for i := 0; i < 20; i++ {
			a.wheel(uv.Mouse{X: x, Y: 0, Button: uv.MouseWheelDown})
		}
		sidebarEvent(a, input.KindMouseClick, x, 7)
		if a.focusedPane() != 1 || a.st.Active != 0 {
			t.Fatal("compact job click did not focus its caller")
		}
		a.st.SidebarVisible = false
		a.sidebarScroll = 0
		a.wheel(uv.Mouse{X: 0, Y: 0, Button: uv.MouseWheelDown})
		sidebarEvent(a, input.KindMouseClick, 0, 0)
		if a.sidebarScroll != 3 || a.st.Active != 3 {
			t.Fatal("left compact scroll click missed workspace")
		}
	}
}

func TestRightSidebarWidthConfigurationIsIndependent(t *testing.T) {
	c, err := config.Decode([]byte("[ui]\nsidebar_width=30\nright_sidebar_width=42\n"))
	if err != nil || len(c.Warnings) > 0 {
		t.Fatal(c.Warnings, err)
	}
	root := t.TempDir()
	a := NewApp(model.New(root, 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, root)
	a.SetSidebarConfig(c, true)
	if a.st.SidebarArea().W != 30 || a.st.RightSidebarArea().W != 42 {
		t.Fatal("configured widths lost independence")
	}
	a.exec(model.CmdToggleRightSidebar{})
	a.exec(model.CmdToggleRightSidebar{})
	a.exec(model.CmdToggleRightSidebar{})
	if a.st.RightSidebarArea().W != 42 {
		t.Fatal("open did not restore configured right width")
	}
	c.UI.RightSidebarWidth = nil
	a.SetSidebarConfig(c, false)
	if a.st.RightSidebarArea().W != 30 {
		t.Fatal("missing right width must use left width")
	}
}
