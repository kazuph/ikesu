package tui

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
	"reflect"
	"testing"
)

func TestG2MobileTapCollapseBlankCycleAndWorkspaceSelectionActualPTY(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.effect(model.EffSpawnPane{Pane: a.focusedPane(), Cwd: a.cwd})
	a.SetSidebarConfig(config.Config{}, true)
	a.exec(model.CmdResize{Cols: 60, Rows: 30})
	a.exec(model.CmdSetSection{Index: 0, Section: model.SectionWork})
	for _, sec := range []model.Section{model.SectionNone, model.SectionFavorite, model.SectionPersonal} {
		a.exec(model.CmdNewWorkspace{Cwd: a.cwd, Section: sec})
	}
	order := []int{2, 0, 3, 1}
	if !reflect.DeepEqual(a.st.VisibleWorkspaceOrder(), order) {
		t.Fatal(a.st.VisibleWorkspaceOrder())
	}
	sw := render.MobileSwitchRect(a.st)
	sidebarEvent(a, input.KindMouseClick, sw.X+1, 1)
	if !a.mobileSwitcher {
		t.Fatal("switch button did not open")
	}
	workY := -1
	for i, r := range render.MobileRows(a.st) {
		if r.Kind == render.RowSection && r.Section == model.SectionWork {
			workY = render.MobileViewport(a.st).Y + i
		}
	}
	sidebarEvent(a, input.KindMouseClick, 1, workY+1)
	if a.st.CollapsedSections[model.SectionWork] || !a.mobileSwitcher {
		t.Fatal("blank header row activated")
	}
	sidebarEvent(a, input.KindMouseClick, 1, workY)
	if !a.st.CollapsedSections[model.SectionWork] {
		t.Fatal("tap did not collapse")
	}
	var cards []int
	for _, r := range render.MobileRows(a.st) {
		if r.Kind == render.RowWorkspace {
			cards = append(cards, r.Ws)
		}
	}
	if !reflect.DeepEqual(cards, []int{2, 3, 1}) {
		t.Fatal(cards)
	}
	a.exec(model.CmdSelectWorkspace{Index: 2})
	for _, want := range []int{3, 1, 2} {
		a.cycleWorkspace(1)
		if a.st.Active != want {
			t.Fatal(a.st.Active, want)
		}
	}
	a.cycleWorkspace(-1)
	if a.st.Active != 1 {
		t.Fatal("previous wrap")
	}
	sidebarEvent(a, input.KindMouseClick, 1, workY)
	if a.st.CollapsedSections[model.SectionWork] {
		t.Fatal("tap did not expand")
	}
	rows := render.MobileRows(a.st)
	for i, r := range rows {
		if r.Kind == render.RowWorkspace && r.Ws == 0 {
			sidebarEvent(a, input.KindMouseClick, 4, render.MobileViewport(a.st).Y+i)
			break
		}
	}
	if a.st.Active != 0 || a.mobileSwitcher {
		t.Fatal("workspace tap did not select and close")
	}
	if a.sup.Get(a.focusedPane()) == nil {
		t.Fatal("selected workspace PTY missing")
	}
}
