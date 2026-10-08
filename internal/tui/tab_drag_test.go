package tui

import (
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"testing"
)

func TestG2TabOneCellDragSuppressesClickAndKeepsActiveIdentity(t *testing.T) {
	a := specRuntimeApp(t)
	a.exec(model.CmdNewTab{Workspace: 0})
	a.draw()
	ws := a.st.ActiveWorkspace()
	active := ws.TabRef().ID
	first := ws.Tabs[0].ID
	r, other := a.tabHits[0], a.tabHits[1]
	sidebarEvent(a, input.KindMouseClick, r.X+1, r.Y)
	sidebarEvent(a, input.KindMouseMotion, r.X+2, r.Y)
	sidebarEvent(a, input.KindMouseRelease, r.X+2, r.Y)
	if ws.TabRef().ID != active {
		t.Fatal("1-cell drag activated pressed tab")
	}
	sidebarEvent(a, input.KindMouseClick, r.X+1, r.Y)
	sidebarEvent(a, input.KindMouseMotion, other.X+1, other.Y)
	sidebarEvent(a, input.KindMouseRelease, other.X+1, other.Y)
	if ws.Tabs[1].ID != first || ws.TabRef().ID != active || a.menu != nil {
		t.Fatal("drag did not reorder or changed focus")
	}
	a.draw()
	r = a.tabHits[1]
	sidebarEvent(a, input.KindMouseClick, r.X+1, r.Y)
	sidebarEvent(a, input.KindMouseRelease, r.X+1, r.Y)
	if ws.TabRef().ID != first {
		t.Fatal("stationary click did not select")
	}
	a.exec(model.CmdNewWorkspace{Cwd: t.TempDir()})
	before := ws.Tabs[0].ID
	a.tabGesture = &tabGesture{ws: 0, tab: 0, x: 0, y: 0, drop: 2, moved: true}
	sidebarEvent(a, input.KindMouseRelease, 0, 0)
	if ws.Tabs[0].ID != before {
		t.Fatal("inactive workspace tabs reordered")
	}
}
