package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestEndpointMenusOutsideRightKeepTargetAndOutsideLeftClose(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.serverUI = true
	a.st.Cols, a.st.Rows = 160, 45
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	for _, open := range []func(){
		func() { a.openTabMenu(27, 0, 0, 0) },
		func() { a.openPaneMenu(27, 5, a.focusedPane()) },
		func() { a.openWorkspaceMenu(2, 5, 0) },
	} {
		open()
		menu := a.menu
		for _, kind := range []input.Kind{input.KindMouseClick, input.KindMouseRelease} {
			a.onMouse(input.Event{Kind: kind, Mouse: uv.Mouse{X: 155, Y: 42, Button: uv.MouseRight}})
			if a.menu != menu {
				t.Fatal("outside right click replaced the endpoint menu target", menu.kind)
			}
		}
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: 155, Y: 42, Button: uv.MouseLeft}})
		if a.menu != nil {
			t.Fatal("outside left click did not dismiss the endpoint menu", menu.kind)
		}
	}
}

func TestParityTabRenameSelectsTargetAndKeepsDefaultName(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	first := a.focusedPane()
	a.exec(model.CmdNewTab{Workspace: 0})
	second := a.focusedPane()
	w := a.st.ActiveWorkspace()
	a.openTabMenu(27, 0, 0, 0)
	if w.ActiveTab != 1 {
		t.Fatal("opening the menu must keep the selected tab")
	}
	wid, height := render.MenuSize(&a.menu.m)
	if wid != 14 || height != 5 || a.menu.m.Sel != 0 || a.menu.m.Items[0].Label != "New tab" || a.menu.m.Items[1].Label != "Rename" || a.menu.m.Items[2].Label != "Close" {
		t.Fatal("tab menu does not match the observed actions and initial selection")
	}
	a.activateMenu(1)
	if w.ActiveTab != 0 || a.focusedPane() != first || a.dialog == nil || a.dialog.d.Input != "1" {
		t.Fatal("rename must select the target and show its default number")
	}
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	if w.Tabs[0].Name != "" || a.sup.Get(first) == nil || a.sup.Get(second) == nil {
		t.Fatal("unchanged default name must retain both real terminals and default styling")
	}
	a.openRenameTabDialog(0, 0)
	a.dialogKey(uv.Key{Code: 'c', Mod: uv.ModCtrl})
	a.dialogKey(uv.Key{Code: 'x', Text: "日本語"})
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	if w.Tabs[0].Name != "日本語" || w.Tabs[1].Name != "" {
		t.Fatal("rename changed the wrong tab")
	}
}
