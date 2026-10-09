package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestParityWorkspaceCloseConfirmKeepsTerminalsUntilAccepted(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.st.ActiveWorkspace().Name = "日本語"
	a.st.ActiveWorkspace().CustomName = true
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	ids := a.st.ActiveWorkspace().TabRef().Layout.PaneIDs()
	a.openCloseWorkspaceDialog(0)
	if a.dialog.d.Detail != "日本語 — 2 panes" || a.dialog.d.Confirm != "confirm" {
		t.Fatal(a.dialog.d)
	}
	for _, id := range ids {
		if a.sup.Get(id) == nil {
			t.Fatal("opening confirmation killed a real terminal")
		}
	}
	a.dialogKey(uv.Key{Code: uv.KeyEscape})
	if !a.workspaceNavigate || a.dialog != nil {
		t.Fatal("cancel must return to navigation")
	}
	for _, id := range ids {
		if a.sup.Get(id) == nil {
			t.Fatal("cancellation killed a real terminal")
		}
	}
	a.openCloseWorkspaceDialog(0)
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	for _, id := range ids {
		if a.sup.Get(id) != nil {
			t.Fatal("confirmation did not close its terminal")
		}
	}
}

func TestParityNavigatePrefixReturnsToShellAndDirectActionsLeaveNavigation(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.workspaceNavigate = true
	a.handleKey(uv.Key{Code: 'b', Mod: uv.ModCtrl}, []byte{2})
	if a.workspaceNavigate || a.prefix {
		t.Fatal("Ctrl-b in navigation must return to the shell without arming prefix")
	}
	rp := a.sup.Get(a.focusedPane())
	a.handleKey(uv.Key{Code: 'p', Text: "printf"}, []byte("printf 'NAV%s\\n' '_SHELL_INPUT'\r"))
	specWaitText(t, rp, "NAV_SHELL_INPUT")
	a.workspaceNavigate = true
	a.modeKey(uv.Key{Code: 'c'})
	if a.workspaceNavigate || a.dialog == nil {
		t.Fatal("new-tab input must leave navigation")
	}
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	if len(a.st.ActiveWorkspace().Tabs) != 2 {
		t.Fatal("new-tab confirmation must create a real tab")
	}
	a.workspaceNavigate = true
	a.modeKey(uv.Key{Code: 'v'})
	if a.workspaceNavigate || a.st.ActiveWorkspace().TabRef().Layout.PaneCount() != 2 {
		t.Fatal("direct vertical split must create the pane and leave navigation")
	}
}
