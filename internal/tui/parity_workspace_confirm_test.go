package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
	"path/filepath"
	"testing"
)

func TestParityNewWorkspaceNamesOnlyTheCreatedWorkspace(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	initial := a.st.ActiveWorkspace()
	initial.Name, initial.CustomName = "original", true
	a.newWorkspace()
	created := a.st.ActiveWorkspace()
	if created.ID == initial.ID || created.CustomName || !created.DefaultName || created.Name != filepath.Base(initial.Cwd) {
		t.Fatal("new workspace must retain its initial directory name", created)
	}
	if initial.Name != "original" || !initial.CustomName {
		t.Fatal("creation renamed the existing workspace")
	}
	a.openCloseWorkspaceDialog(a.st.Active)
	if a.dialog.d.Detail != created.Name+" — 1 pane" {
		t.Fatal("confirmation must use the created workspace's retained name", a.dialog.d)
	}
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	if a.st.ActiveWorkspace().ID != initial.ID || a.sup.Get(created.Tabs[0].Layout.Focused()) != nil {
		t.Fatal("confirmation did not close only the created terminal")
	}
	rp := a.sup.Get(a.focusedPane())
	specFeed(a, "printf 'ORIGINAL%s\\n' '_SHELL_ALIVE'\r")
	specWaitText(t, rp, "ORIGINAL_SHELL_ALIVE")
}

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

func TestParityServerWorkspaceCloseCancelDoesNotOpenMobileSwitcher(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.SetServerUI()
	a.prefixKey(uv.Key{Code: 'w'})
	if a.modeHint() == "" {
		t.Fatal("explicit workspace navigation must show its key hint")
	}
	a.st.Cols, a.st.Rows = 50, 15
	a.openCloseWorkspaceDialog(a.st.Active)
	a.dialogKey(uv.Key{Code: uv.KeyEscape})
	if a.dialog != nil || a.mobileSwitcher || !a.workspaceNavigate {
		t.Fatal("server cancellation must restore the pane and navigation without opening the switcher")
	}
	if a.modeHint() != "" {
		t.Fatal("dialog cancellation restores selection without the explicit navigation hint")
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
	specFeed(a, "printf 'NAV%s\\n' '_SHELL_INPUT'\r")
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
