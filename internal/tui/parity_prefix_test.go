package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestParityPrefixTabCommandsAndRename(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	specFeed(a, "\x02c")
	if a.dialog == nil || !a.dialog.d.InputEdit || len(a.st.ActiveWorkspace().Tabs) != 1 {
		t.Fatal("new tab must prompt first")
	}
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	a.exec(model.CmdNewTab{Workspace: 0})
	specFeed(a, "\x02p")
	if a.st.ActiveWorkspace().ActiveTab != 1 {
		t.Fatal("previous tab")
	}
	specFeed(a, "\x02n")
	if a.st.ActiveWorkspace().ActiveTab != 2 {
		t.Fatal("next tab")
	}
	specFeed(a, "\x021")
	specFeed(a, "\x029")
	if a.st.ActiveWorkspace().ActiveTab != 0 {
		t.Fatal("missing tab must not wrap")
	}
	specFeed(a, "\x02T")
	if a.dialog == nil || a.dialog.d.Title != "Rename tab" {
		t.Fatal("rename tab")
	}
	a.dialogKey(uv.Key{Code: uv.KeyEscape})
	specFeed(a, "\x02W")
	if a.dialog == nil || a.dialog.d.Title != "Rename workspace" {
		t.Fatal("rename workspace")
	}
}

func TestParityFinalShellCloseReplacesOnlyTheShell(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	ws := a.st.ActiveWorkspace().ID
	old := a.focusedPane()
	a.closeFocusedPane()
	if a.quit || a.dialog != nil || len(a.st.Workspaces) != 1 || a.st.ActiveWorkspace().ID != ws || a.focusedPane() == old || a.sup.Get(old) != nil || a.st.ActiveWorkspace().TabRef().Layout.PaneCount() != 1 {
		t.Fatal("final shell must be replaced without quitting or prompting to stop the server")
	}
	old = a.focusedPane()
	a.closeActiveTab()
	if a.quit || a.dialog != nil || len(a.st.ActiveWorkspace().Tabs) != 1 || a.focusedPane() == old || a.sup.Get(old) != nil {
		t.Fatal("final tab must be replaced")
	}
}

func TestParityResizeUsesObservedStepAndEscape(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.st.Cols, a.st.Rows = 160, 45
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	specFeed(a, "\x02r")
	specFeed(a, "h")
	panes := a.st.ActiveWorkspace().TabRef().PaneRects(a.st.PanesArea())
	if !a.resizeMode || panes[0].Rect.W != 60 {
		t.Fatal("observed left step is 67 to 60 cells", panes)
	}
	specFeed(a, "l")
	panes = a.st.ActiveWorkspace().TabRef().PaneRects(a.st.PanesArea())
	if panes[0].Rect.W != 67 {
		t.Fatal("right step must restore width", panes)
	}
	a.modeKey(uv.Key{Code: uv.KeyEscape})
	if a.resizeMode {
		t.Fatal("resize escape")
	}
}
