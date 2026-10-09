package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestParityOutsideMenuConsumesLeftAndRetargetsRight(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	panes := a.st.ActiveWorkspace().TabRef().PaneRects(a.st.PanesArea())
	left, right := panes[0], panes[1]
	a.exec(model.CmdFocusPane{Pane: left.ID})
	a.openPaneMenu(left.Rect.X+2, left.Rect.Y+2, left.ID)
	r := render.MenuRect(&a.menu.m, a.st.Cols, a.st.Rows)
	m := uv.Mouse{X: right.Rect.X + right.Rect.W - 3, Y: right.Rect.Y + right.Rect.H - 3, Button: uv.MouseLeft}
	if pointIn(m.X, m.Y, r) {
		t.Fatal("outside test point falls inside menu")
	}
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
	a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: m})
	if a.menu != nil || a.focusedPane() != left.ID || a.sel != nil {
		t.Fatal("outside left press must dismiss without focusing or selecting the underlying pane")
	}
	a.openPaneMenu(left.Rect.X+2, left.Rect.Y+2, left.ID)
	m.Button = uv.MouseRight
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
	a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: m})
	if a.menu == nil || a.menu.pane != right.ID || a.menu.m.X != m.X || a.menu.m.Y != m.Y {
		t.Fatal("outside right press must open the target pane menu and survive its release")
	}
}
