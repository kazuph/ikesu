package tui

import (
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
	"reflect"
	"testing"
)

func TestG2GitLeftRightMenuOrderAndSectionAction(t *testing.T) {
	teststate.Lock(t)
	a, _ := worktreeApp(t)
	a.draw()
	y := sidebarCardY(a, 0)
	a.openWorkspaceMenu(3, y, 0)
	labels := func() []string {
		var out []string
		for _, it := range a.menu.m.Items {
			if it.Separator {
				out = append(out, "--")
			} else {
				out = append(out, it.Label)
			}
		}
		return out
	}
	right := labels()
	a.menu = nil
	sidebarEvent(a, input.KindMouseClick, 3, y)
	sidebarEvent(a, input.KindMouseRelease, 3, y)
	if a.menu == nil || a.menu.kind != menuWorkspace || !reflect.DeepEqual(labels(), right) {
		t.Fatal("Git left/right contract", a.menu)
	}
	idx := func(label string) int {
		for i, l := range labels() {
			if l == label {
				return i
			}
		}
		return -1
	}
	for _, l := range []string{"New worktree", "Open worktree..."} {
		if idx(l) < 4 || idx(l) >= idx("Duplicate") {
			t.Fatal(labels())
		}
	}
	if labels()[idx("Duplicate")+1] != "--" || labels()[idx("Rename")-1] != "--" {
		t.Fatal(labels())
	}
	a.st.CollapsedSections = map[model.Section]bool{model.SectionWork: true}
	activateLabel(t, a, "💼 work")
	if a.st.Workspaces[0].Section != model.SectionWork || a.st.CollapsedSections[model.SectionWork] {
		t.Fatal("section menu did not expand")
	}
	a.st.WorkspaceDensity = "slim"
	a.menu = nil
	y = sidebarCardY(a, 0)
	sidebarEvent(a, input.KindMouseClick, 3, y+1)
	sidebarEvent(a, input.KindMouseRelease, 3, y+1)
	if a.menu != nil || a.sidebarGesture != nil {
		t.Fatal("slim git row has hit rect")
	}
	a.openWorkspaceMenu(3, y, 0)
	activateLabel(t, a, "New worktree")
	*a.dialog.input = "feat/g2-matrix"
	d := a.dialog
	a.dialog = nil
	d.run(a)
	a.openWorkspaceMenu(3, y, 1)
	if idx("Delete worktree checkout...") < 0 || idx("Delete worktree checkout...") >= idx("Duplicate") {
		t.Fatal(labels())
	}
	rows := render.SidebarRows(a.st, a.paneSnaps())
	count := 0
	for _, r := range rows {
		if r.Ws == 1 && (r.Kind == render.RowWorkspace || r.Kind == render.RowGit) {
			count++
		}
	}
	if count != 1 {
		t.Fatal("indent child must remain one row", count)
	}
}

func TestG2WorkspaceReorderPreservesNumericSelection(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	for i := 0; i < 2; i++ {
		a.exec(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	}
	first, second, last := a.st.Workspaces[0].ID, a.st.Workspaces[1].ID, a.st.Workspaces[2].ID
	y, target := sidebarCardY(a, 0), sidebarCardY(a, 1)
	sidebarEvent(a, input.KindMouseClick, 3, y)
	sidebarEvent(a, input.KindMouseMotion, 3, target)
	sidebarEvent(a, input.KindMouseRelease, 3, target)
	if a.st.Workspaces[0].ID != second || a.st.Workspaces[1].ID != first || a.st.Workspaces[2].ID != last || a.menu != nil {
		t.Fatal("reorder indices")
	}
	a.selectWorkspace(1)
	if a.st.ActiveWorkspace().ID != first {
		t.Fatal("numeric position changed")
	}
}

func TestG2DangerousDialogTextAndStandaloneRestartState(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	tests := []struct {
		open                 func()
		title, detail, label string
	}{
		{a.openQuitDialog, "Stop server?", "Stops the gtx server and all running panes.", "stop"},
		{a.openRestartDialog, "Restart gtx?", "Restarts the gtx server. The saved session can be restored.", "restart"},
		{a.openRestoreDialog, "Restore agents?", "Types resume commands into panes with recorded agent sessions.", "restore"},
	}
	for _, tc := range tests {
		tc.open()
		d := a.dialog.d
		if d.Title != tc.title || d.Detail != tc.detail || d.Confirm != tc.label || d.Cancel != "cancel" || d.Width != 76 {
			t.Fatal(d)
		}
	}
	a.SetRestartHook(func() { a.standaloneRestart = true; a.exec(model.CmdQuit{}) })
	a.openRestartDialog()
	if a.quit || a.standaloneRestart {
		t.Fatal("restart before confirmation")
	}
	a.dialog.run(a)
	if !a.quit || !a.standaloneRestart {
		t.Fatal("restart must quit and request restart")
	}
}
