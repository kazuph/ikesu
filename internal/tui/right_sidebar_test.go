package tui

import (
	"bytes"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
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
		if a.menu == nil {
			t.Fatal("right-click menu missing")
		}
		a.menu = nil
		a.exec(model.CmdToggleSidebar{})
		sidebarEvent(a, input.KindMouseClick, 0, 0)
		if a.st.Active != 0 {
			t.Fatal("compact workspace click missed")
		}
	}
}
