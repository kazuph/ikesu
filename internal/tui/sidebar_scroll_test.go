package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestG2IndependentPanelWheelThumbDragAndCollapse(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	a := specRuntimeApp(t)
	a.exec(model.CmdResize{Cols: 100, Rows: 30})
	for i := 0; i < 16; i++ {
		if i > 0 {
			a.exec(model.CmdNewWorkspace{Cwd: a.cwd, Section: model.SectionWork, NoFocus: true})
		}
		ws := a.st.Workspaces[i]
		id := ws.Tabs[0].Layout.Focused()
		p := a.sup.Get(id)
		if p == nil {
			var err error
			p, err = a.sup.Spawn(id, a.cwd, 80, 20)
			if err != nil {
				t.Fatal(err)
			}
		}
		p.SetAgent("codex")
	}
	v := a.sidebarView()
	a.sidebarWheel(uv.Mouse{X: 2, Y: v.Workspace.Rect.Y}, false)
	if a.sidebarScroll != 3 || a.agentScroll != 0 {
		t.Fatal("workspace wheel crossed panel")
	}
	a.sidebarWheel(uv.Mouse{X: 2, Y: v.Agents.Rect.Y}, false)
	if a.sidebarScroll != 3 || a.agentScroll != 3 {
		t.Fatal("agent wheel crossed panel")
	}
	for _, agents := range []bool{false, true} {
		v = a.sidebarView()
		panel := v.Workspace
		if agents {
			panel = v.Agents
		}
		x := panel.Rect.X + panel.Rect.W - 1
		if !a.sidebarScrollbarPress(uv.Mouse{X: x, Y: panel.Rect.Y, Button: uv.MouseLeft}) {
			t.Fatal("missing scrollbar hit")
		}
		a.sidebarScrollbarMouse(input.Event{Kind: input.KindMouseMotion, Mouse: uv.Mouse{X: x, Y: panel.Rect.Y + panel.Rect.H - 1}})
		a.sidebarScrollbarMouse(input.Event{Kind: input.KindMouseRelease})
		if a.sidebarBar != nil {
			t.Fatal("drag leaked")
		}
		got := a.sidebarScroll
		if agents {
			got = a.agentScroll
		}
		if got != panel.Total-panel.Rect.H {
			t.Fatal("thumb did not reach end", got, panel)
		}
	}
	a.toggleSection(model.SectionWork)
	v = a.sidebarView()
	if v.Agents.Total != 1 || v.Agents.Offset != 0 || v.Workspace.Offset != 0 {
		t.Fatal("collapse retained hidden cards in scroll geometry", v)
	}
}
