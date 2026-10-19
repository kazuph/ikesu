package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestParityTabbarOverflowKeepsRealPTYSelectionAndManualScroll(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.st.RightSidebarMode = "hidden" // Preserve the original pane geometry for this operation regression.
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdResize{Cols: 160, Rows: 45})
	a.exec(model.CmdRenameTab{Workspace: 0, Tab: 0, Label: "LONG TAB NAME"})
	for i := 1; i < 8; i++ {
		a.exec(model.CmdNewTab{Workspace: 0})
	}
	a.exec(model.CmdResize{Cols: 80, Rows: 25})
	a.draw()
	if a.tabScroll != 38 || a.tabHits[7].W != 8 {
		t.Fatal("selected last tab is hidden", a.tabScroll, a.tabHits)
	}
	click := func(x int) {
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: x, Y: 0, Button: uv.MouseLeft}})
		a.draw()
	}
	click(27)
	if a.tabScroll != 29 || a.st.ActiveWorkspace().ActiveTab != 7 {
		t.Fatal("left scroll changed selection", a.tabScroll)
	}
	click(27)
	if a.tabScroll != 20 {
		t.Fatal("second left scroll", a.tabScroll)
	}
	click(74)
	if a.tabScroll != 29 {
		t.Fatal("right scroll", a.tabScroll)
	}
	a.exec(model.CmdSelectTab{Workspace: 0, Tab: 0})
	a.draw()
	if a.tabScroll != 0 || a.tabHits[0].W != 19 {
		t.Fatal("prefix selection did not reveal first tab", a.tabScroll, a.tabHits[0])
	}
	if a.sup.Get(a.focusedPane()) == nil {
		t.Fatal("selected tab has no real PTY")
	}
}

func TestParityTabbarAddWaitsForNameAndCancelKeepsRealPTY(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.draw()
	ws := a.st.ActiveWorkspace()
	old := a.focusedPane()
	for _, button := range []uv.MouseButton{uv.MouseMiddle} {
		r := a.tabHits[0]
		a.tabBarClick(uv.Mouse{X: r.X + 1, Y: r.Y, Button: button})
		if len(ws.Tabs) != 1 || a.focusedPane() != old || a.sup.Get(old) == nil {
			t.Fatal("middle click must preserve the tab and its shell")
		}
	}
	r := a.newTabRect
	m := uv.Mouse{X: r.X + 1, Y: r.Y, Button: uv.MouseLeft}
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
	a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: m})
	if a.dialog == nil || a.dialog.d.Title != "New tab" || len(ws.Tabs) != 1 {
		t.Fatal("add must open the name dialog before creating a tab")
	}
	a.dialogKey(uv.Key{Code: uv.KeyEscape})
	if a.dialog != nil || len(ws.Tabs) != 1 || a.focusedPane() != old || a.sup.Get(old) == nil {
		t.Fatal("cancellation changed the original shell")
	}
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
	a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: m})
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	if a.dialog != nil || len(ws.Tabs) != 2 || a.focusedPane() == old || a.sup.Get(a.focusedPane()) == nil {
		t.Fatal("confirmation must create and select a real shell")
	}
}

func TestServerTabArrowLeadingBlankAndGlyphUseRecordedHitRegionsRealPTY(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.SetServerUI()
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdRenameTab{Workspace: 0, Tab: 0, Label: "LONG TAB NAME"})
	for i := 1; i < 8; i++ {
		a.exec(model.CmdNewTab{Workspace: 0})
	}
	a.exec(model.CmdResize{Cols: 80, Rows: 25})
	a.draw()
	start := a.tabScroll
	click := func(x int) {
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: x, Y: 0, Button: uv.MouseLeft}})
		a.draw()
	}
	click(a.tabLeftRect.X)
	if a.tabScroll != start {
		t.Fatal("server left-arrow leading blank must leave the strip unchanged")
	}
	want := a.tabScrollLeft
	click(a.tabLeftRect.X + 1)
	if a.tabScroll != want || a.st.ActiveWorkspace().ActiveTab != 7 {
		t.Fatal("server arrow glyph must scroll without selecting another tab")
	}
	a.exec(model.CmdSelectTab{Workspace: 0, Tab: 0})
	a.draw()
	want = a.tabScrollRight
	click(a.tabRightRect.X)
	if a.tabScroll != want || a.st.ActiveWorkspace().ActiveTab != 0 {
		t.Fatal("server right-arrow leading blank must scroll without changing selection")
	}
}
