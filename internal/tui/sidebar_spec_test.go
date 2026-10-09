package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
	"reflect"
	"testing"
	"time"
)

func sidebarCardY(a *App, wi int) int {
	_, y, _, _ := render.SidebarContent(a.st.SidebarArea())
	for i, r := range render.SidebarRows(a.st, a.paneSnaps(), a.sidebarConfig) {
		if r.Kind == render.RowWorkspace && r.Ws == wi {
			return y + i
		}
	}
	return -1
}
func sidebarSectionY(a *App, sec model.Section) int {
	_, y, _, _ := render.SidebarContent(a.st.SidebarArea())
	for i, r := range render.SidebarRows(a.st, a.paneSnaps(), a.sidebarConfig) {
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
	teststate.Lock(t)
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
	teststate.Lock(t)
	a := specRuntimeApp(t)
	rows := render.SidebarRows(a.st, a.paneSnaps(), a.sidebarConfig)
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
	teststate.Lock(t)
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
	teststate.Lock(t)
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
	teststate.Lock(t)
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

func TestG2DuplicateRuntimeSpawnFailureRollsBack(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	before := len(a.st.Workspaces)
	active := a.st.Active
	a.st.Workspaces[0].Cwd = "/path/that/does/not/exist/gtx-test"
	if _, err := a.DuplicateWorkspace(0, true); err == nil {
		t.Fatal("spawn failure reported success")
	}
	if len(a.st.Workspaces) != before || a.st.Active != active {
		t.Fatal("failed duplicate changed source")
	}
	for _, w := range a.st.Workspaces {
		for _, tab := range w.Tabs {
			for _, id := range tab.Layout.PaneIDs() {
				if id > 1 {
					t.Fatal("failed clone remained")
				}
			}
		}
	}
}
func TestG2SidebarConfigReloadKeepsCollapseAndManualWidth(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	c := config.Config{}
	c.UI.SidebarStartCollapsed = true
	a.SetSidebarConfig(c, true)
	if a.st.SidebarVisible {
		t.Fatal("startup collapse ignored")
	}
	a.exec(model.CmdToggleSidebar{})
	a.sidebarPreset("wide")
	a.SetSidebarConfig(c, false)
	if !a.st.SidebarVisible || a.st.ExpandedSidebarWidth() != 72 {
		t.Fatal("reload overwrote UI choice")
	}
}

func TestG2HeaderTogglesPersistAndResetScroll(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "GTX_CONFIG", t.TempDir()+"/config.toml")
	a := specRuntimeApp(t)
	a.sidebarScroll = 12
	a.toggleSidebarSetting(true)
	c, err := config.Load()
	if err != nil || c.WorkspaceDensity() != "slim" || a.st.WorkspaceDensity != "slim" || a.sidebarScroll != 0 {
		t.Fatal(c, err)
	}
	a.sidebarScroll = 8
	a.toggleSidebarSetting(false)
	c, err = config.Load()
	if err != nil || c.AgentScope() != "sort" || a.st.AgentScope != "sort" || a.sidebarScroll != 0 {
		t.Fatal(c, err)
	}
	if c.UI.AgentPanelSort != "priority" || c.UI.AgentPanelScope != "all" {
		t.Fatal("priority toggle must save the observed sort setting", c.UI)
	}
	a.toggleSidebarSetting(false)
	c, err = config.Load()
	if err != nil || c.AgentScope() != "all" || c.UI.AgentPanelSort != "spaces" {
		t.Fatal("grouped toggle must restore workspace ordering", c, err)
	}
}

func TestG2FooterBlankAndSectionBody(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	r := render.SidebarMenuRect(a.st)
	n := render.SidebarNewRect(a.st)
	if r.X+r.W > n.X {
		t.Fatal("footer order")
	}
	sidebarEvent(a, input.KindMouseClick, r.X, r.Y)
	if a.menu == nil || a.menu.kind != menuMain {
		t.Fatal("footer menu missing")
	}
	var labels []string
	for _, it := range a.menu.m.Items {
		if it.Separator {
			labels = append(labels, "--")
		} else {
			labels = append(labels, it.Label)
		}
	}
	want := []string{"new workspace", "new tab", "--", "settings", "keybinds", "reload config", "sidebar narrow", "sidebar normal", "sidebar wide", "--", "restore agents...", "--", "detach", "--", "stop server", "restart"}
	if !reflect.DeepEqual(labels, want) {
		t.Fatal(labels)
	}
	a.menu = nil
	before := len(a.st.Workspaces)
	sidebarEvent(a, input.KindMouseClick, n.X, n.Y)
	if len(a.st.Workspaces) != before+1 || a.st.ActiveWorkspace().SidebarSection() != model.SectionNone {
		t.Fatal("footer new section")
	}
	a.setSection(0, model.SectionWork)
	source := a.st.Active
	y, body := sidebarCardY(a, source), sidebarCardY(a, 0)
	sidebarEvent(a, input.KindMouseClick, 3, y)
	sidebarEvent(a, input.KindMouseMotion, 3, body)
	sidebarEvent(a, input.KindMouseRelease, 3, body)
	if a.st.Workspaces[source].SidebarSection() != model.SectionWork {
		t.Fatal("section body drop")
	}
	a.menu = nil
	a.draw()
	for i := 0; i < 2; i++ {
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: 3, Y: a.st.SidebarArea().H - 4, Button: uv.MouseRight}})
		if a.menu != nil {
			t.Fatal("blank menu")
		}
	}
	a.openQuitDialog()
	if a.quit {
		t.Fatal("quit before confirm")
	}
	a.dialog.run(a)
	if !a.quit {
		t.Fatal("quit did not confirm")
	}
}

func TestG2SectionCommandResetsScrollForAPIAndUI(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.sidebarScroll = 12
	a.ExecCmd(model.CmdSetSection{Index: 0, Section: model.SectionWork})
	if a.sidebarScroll != 0 {
		t.Fatal("API command retained old section scroll")
	}
	a.sidebarScroll = 12
	a.ExecCmd(model.CmdToggleSection{Section: model.SectionWork})
	if a.sidebarScroll != 0 {
		t.Fatal("collapse command retained old section scroll")
	}
}
