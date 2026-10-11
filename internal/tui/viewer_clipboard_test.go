package tui

import (
	"bytes"
	"testing"

	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestViewerClipboardVisibilityObeysSplitZoomTabAndBlur(t *testing.T) {
	st := model.New(t.TempDir(), 120, 40)
	tab := st.ActiveWorkspace().TabRef()
	first := tab.Layout.Focused()
	second := first + 1
	tab.Layout.SplitFocused(layout.Vertical, second)
	app := NewApp(st, runtime.NewSupervisor(), &bytes.Buffer{}, "")
	viewer := app.NewViewer(&bytes.Buffer{}, 120, 40, nil)
	if !viewer.DisplaysPane(app, first) || !viewer.DisplaysPane(app, second) {
		t.Fatal("unfocused split was excluded")
	}
	tab.Zoomed = second
	if viewer.DisplaysPane(app, first) || !viewer.DisplaysPane(app, second) {
		t.Fatal("zoomed visibility was ignored")
	}
	tab.Zoomed = 0
	st.Overlay = &model.OverlayPaneState{WorkspaceID: st.ActiveWorkspace().ID, TabID: tab.ID, TargetPaneID: first, PaneID: second + 1}
	if viewer.DisplaysPane(app, first) || !viewer.DisplaysPane(app, second) || !viewer.DisplaysPane(app, second+1) {
		t.Fatal("overlay visibility was ignored")
	}
	st.Overlay = nil
	st.Popup = &model.PopupPaneState{WorkspaceID: st.ActiveWorkspace().ID, PaneID: second + 2}
	if !viewer.DisplaysPane(app, second+2) {
		t.Fatal("displayed popup was excluded")
	}
	st.Popup = nil
	viewer.tabs[st.ActiveWorkspace().ID] = tab.ID + 1
	if viewer.DisplaysPane(app, first) || viewer.DisplaysPane(app, second) {
		t.Fatal("hidden tab was included")
	}
	viewer.tabs[st.ActiveWorkspace().ID] = tab.ID
	viewer.app.windowBlurred = true
	if viewer.DisplaysPane(app, first) {
		t.Fatal("blurred viewer was included")
	}
}

func TestNotificationSuppressionIncludesUnfocusedAndZoomHiddenTabPanes(t *testing.T) {
	st := model.New(t.TempDir(), 120, 40)
	tab := st.ActiveWorkspace().TabRef()
	first := tab.Layout.Focused()
	second := first + 1
	tab.Layout.SplitFocused(layout.Vertical, second)
	app := NewApp(st, runtime.NewSupervisor(), &bytes.Buffer{}, "")
	viewer := app.NewViewer(&bytes.Buffer{}, 120, 40, nil)
	tab.Zoomed = second
	if !viewer.PaneOnSelectedTab(app, first) || !viewer.PaneOnSelectedTab(app, second) {
		t.Fatal("active tab must suppress all its panes")
	}
	if viewer.PaneOnSelectedTab(app, second+1) {
		t.Fatal("unrelated pane suppressed")
	}
}
