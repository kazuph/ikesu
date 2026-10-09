package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
)

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
	a.activateMenu(0)
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
