package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"reflect"
	"testing"
	"time"
)

func sidebarCardY(a *App, wi int) int {
	_, y, _, _ := render.SidebarContent(a.st.SidebarArea())
	for i, r := range render.SidebarRows(a.st, a.paneSnaps()) {
		if r.Kind == render.RowWorkspace && r.Ws == wi {
			return y + i
		}
	}
	return -1
}
func sidebarSectionY(a *App, sec model.Section) int {
	_, y, _, _ := render.SidebarContent(a.st.SidebarArea())
	for i, r := range render.SidebarRows(a.st, a.paneSnaps()) {
		if r.Kind == render.RowSection && r.Section == sec {
			return y + i
		}
	}
	return -1
}
func sidebarEvent(a *App, kind input.Kind, x, y int) {
	a.onMouse(input.Event{Kind: kind, Mouse: uv.Mouse{X: x, Y: y, Button: uv.MouseLeft}})
}
func TestG2ActiveCardReleaseDragAndSection(t *testing.T) {
	a := specRuntimeApp(t)
	a.exec(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true, Section: model.SectionWork})
	y := sidebarCardY(a, 0)
	sidebarEvent(a, input.KindMouseClick, 3, y)
	if a.menu != nil {
		t.Fatal("menu on press")
	}
	sidebarEvent(a, input.KindMouseRelease, 3, y)
	if a.menu == nil || a.menu.ws != 0 || a.st.Active != 0 {
		t.Fatal("release menu")
	}
	a.menu = nil
	sidebarEvent(a, input.KindMouseClick, 3, y)
	sidebarEvent(a, input.KindMouseRelease, 3, y+1)
	if a.menu != nil {
		t.Fatal("different row release")
	}
	sidebarEvent(a, input.KindMouseClick, 3, y)
	sidebarEvent(a, input.KindMouseMotion, 4, y)
	sidebarEvent(a, input.KindMouseRelease, 4, y)
	if a.menu != nil {
		t.Fatal("one cell drag menu")
	}
	target := sidebarSectionY(a, model.SectionWork)
	before := []int{a.st.Workspaces[0].ID, a.st.Workspaces[1].ID}
	sidebarEvent(a, input.KindMouseClick, 3, y)
	sidebarEvent(a, input.KindMouseMotion, 3, target)
	sidebarEvent(a, input.KindMouseRelease, 3, target)
	if a.st.Workspaces[0].Section != model.SectionWork || !reflect.DeepEqual(before, []int{a.st.Workspaces[0].ID, a.st.Workspaces[1].ID}) {
		t.Fatal("section drag reordered")
	}
}
func TestG2HorizontalRulesAndBlankNeverActivate(t *testing.T) {
	a := specRuntimeApp(t)
	rows := render.SidebarRows(a.st, a.paneSnaps())
	_, y, _, _ := render.SidebarContent(a.st.SidebarArea())
	before := len(a.st.Workspaces)
	for i, r := range rows {
		if r.Kind != render.RowRule && r.Kind != render.RowBlank {
			continue
		}
		sidebarEvent(a, input.KindMouseClick, 3, y+i)
		sidebarEvent(a, input.KindMouseRelease, 3, y+i)
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: 3, Y: y + i, Button: uv.MouseRight}})
		if a.menu != nil || len(a.st.Workspaces) != before || a.st.CollapsedSections[model.SectionNone] {
			t.Fatal("inert row activated", r)
		}
	}
}
func TestG2SectionNewAndCycleVisibleWrap(t *testing.T) {
	a := specRuntimeApp(t)
	for i := 0; i < 3; i++ {
		a.exec(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	}
	for i, sec := range []model.Section{model.SectionWork, model.SectionNone, model.SectionFavorite, model.SectionPersonal} {
		a.setSection(i, sec)
	}
	a.exec(model.CmdSelectWorkspace{Index: 2})
	for _, want := range []int{0, 3, 1, 2} {
		a.cycleWorkspace(1)
		if a.st.Active != want {
			t.Fatal(a.st.Active, want)
		}
	}
	a.cycleWorkspace(-1)
	if a.st.Active != 1 {
		t.Fatal("reverse wrap")
	}
	a.toggleSection(model.SectionPersonal)
	a.exec(model.CmdSelectWorkspace{Index: 0})
	a.cycleWorkspace(1)
	if a.st.Active != 1 {
		t.Fatal("collapsed selected")
	}
	a.selectWorkspace(3)
	if a.st.Active != 3 {
		t.Fatal("number selection changed")
	}
	y := sidebarSectionY(a, model.SectionWork)
	ix, _, w, _ := render.SidebarContent(a.st.SidebarArea())
	r := render.SectionNewRect(ix, y, w)
	sidebarEvent(a, input.KindMouseClick, r.X, r.Y)
	if a.st.ActiveWorkspace().Section != model.SectionWork {
		t.Fatal("section new")
	}
}
func TestG2DividerResetAndDangerousCancel(t *testing.T) {
	a := specRuntimeApp(t)
	a.st.SidebarDefault = 26
	a.sidebarPreset("wide")
	a.dividerAt = time.Now()
	sidebarEvent(a, input.KindMouseClick, a.st.SidebarArea().W-1, 5)
	if a.st.ExpandedSidebarWidth() != 26 || a.st.SidebarManual || a.sidebarGesture != nil {
		t.Fatal("double reset")
	}
	a.openQuitDialog()
	if a.quit || a.dialog.d.Title != "Stop server?" || a.dialog.d.Confirm != "stop" || a.dialog.d.Width != 76 {
		t.Fatal("stop confirm")
	}
	sidebarEvent(a, input.KindMouseClick, 0, 0)
	if a.quit || a.dialog != nil {
		t.Fatal("outside cancelled")
	}
	restored := false
	a.restoreHook = func(bool) { restored = true }
	a.openRestoreDialog()
	if restored || a.quit {
		t.Fatal("restore immediate")
	}
	a.dialog.run(a)
	if !restored || a.quit {
		t.Fatal("restore quit")
	}
	restarted := false
	a.restartHook = func() { restarted = true }
	a.openRestartDialog()
	if restarted {
		t.Fatal("restart immediate")
	}
	a.dialog.run(a)
	if !restarted {
		t.Fatal("restart missing")
	}
}
func TestG2WorkspaceMenuExactAndUnsupported(t *testing.T) {
	a := specRuntimeApp(t)
	a.st.Workspaces[0].Cwd = t.TempDir()
	a.openWorkspaceMenu(3, 4, 0)
	var got []string
	for _, it := range a.menu.m.Items {
		if it.Separator {
			got = append(got, "--")
		} else {
			got = append(got, it.Label)
		}
	}
	want := []string{"New Claude Code agent", "New Codex agent", "New agy agent", "--", "Duplicate", "--", "Rename", "Close", "--", "⭐ favorite", "💼 work", "🏠 personal", "No section"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	a.duplicateDialog(99)
	if a.dialog == nil || a.dialog.d.Title != "duplicate workspace failed: workspace not found" {
		t.Fatal("missing diagnostic")
	}
}
