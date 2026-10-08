package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"testing"
)

func TestSPECRightClickWorkspaceTabFrameAndTabWheel(t *testing.T) {
	a := specRuntimeApp(t)
	a.exec(model.CmdNewTab{Workspace: 0})
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	a.draw()
	_, y, _, _ := render.SidebarContent(a.st.SidebarArea())
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: 2, Y: y, Button: uv.MouseRight}})
	if a.menu == nil || a.menu.kind != menuWorkspace || a.menu.ws != 0 {
		t.Fatal("workspace row ownership", a.menu)
	}
	a.menu = nil
	r := a.tabHits[0]
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: r.X, Y: r.Y, Button: uv.MouseRight}})
	if a.st.ActiveWorkspace().ActiveTab != 0 || a.menu == nil || a.menu.kind != menuTab || a.menu.tab != 0 {
		t.Fatal("tab ownership", a.menu)
	}
	a.menu = nil
	a.onMouse(input.Event{Kind: input.KindMouseWheel, Mouse: uv.Mouse{X: r.X, Y: r.Y, Button: uv.MouseWheelDown}})
	if a.st.ActiveWorkspace().ActiveTab != 1 {
		t.Fatal("tab wheel did not switch")
	}
	a.draw()
	pr := a.st.ActiveWorkspace().TabRef().PaneRects(a.st.PanesArea())[0]
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: pr.Rect.X, Y: pr.Rect.Y, Button: uv.MouseRight}})
	if a.menu == nil || a.menu.kind != menuPane || a.menu.pane != pr.ID || a.focusedPane() != pr.ID {
		t.Fatal("frame ownership", a.menu)
	}
}

func TestSPECLeftDownPriorityAndActionOverlapOrder(t *testing.T) {
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	a.draw()
	l := a.st.ActiveWorkspace().TabRef().Layout
	panes := l.Panes(a.st.PanesArea())
	first := panes[0]
	x := first.Rect.X + first.Rect.W - 1
	y := first.Rect.Y + 2
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: x, Y: y, Button: uv.MouseLeft}})
	if a.sep == nil || a.sb != nil || a.sel != nil || a.st.ActiveWorkspace().TabRef().Zoomed != 0 {
		t.Fatal("split precedence")
	}
	a.sep = nil
	inner, _ := model.PaneContentRect(first.Rect, a.st.PaneBorders)
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: inner.X + inner.W - 1, Y: inner.Y, Button: uv.MouseLeft}})
	if a.sb == nil || a.sel != nil || a.st.ActiveWorkspace().TabRef().Zoomed != 0 {
		t.Fatal("scrollbar precedence")
	}
	a.sb = nil
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: inner.X + 1, Y: inner.Y + 1, Button: uv.MouseLeft}})
	if a.sel == nil || a.sep != nil || a.sb != nil {
		t.Fatal("body selection")
	}
	a.sel = nil
	r := a.actionBar.Cycle
	a.actionBar.Rotate = r
	a.actionBar.Equalize = r
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: r.X, Y: r.Y, Button: uv.MouseLeft}})
	if l.Root().Direction != layout.Horizontal || a.sel != nil || a.sep != nil || a.sb != nil {
		t.Fatal("cycle overlap precedence")
	}
	a.draw()
	r = a.actionBar.Rotate
	a.actionBar.Equalize = r
	before := l.LeafIDs()[0]
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: r.X, Y: r.Y, Button: uv.MouseLeft}})
	if l.LeafIDs()[0] == before {
		t.Fatal("rotate overlap precedence")
	}
}
