package tui

import (
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/session"
	"github.com/kazuph/goterrace/internal/teststate"
	"path/filepath"
	"testing"
)

func TestG2TabOneCellDragSuppressesClickAndKeepsActiveIdentity(t *testing.T) {
	teststate.Lock(t)
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
	sidebarEvent(a, input.KindMouseMotion, other.X+other.W-1, other.Y)
	sidebarEvent(a, input.KindMouseRelease, other.X+other.W-1, other.Y)
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

func TestTabDragMidpointCancellationAndRestore(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.exec(model.CmdNewTab{Workspace: 0})
	a.exec(model.CmdNewTab{Workspace: 0})
	a.draw()
	ws := a.st.ActiveWorkspace()
	active := ws.TabRef().ID
	first := ws.Tabs[0].ID
	last := a.tabHits[2]
	start := a.tabHits[0]
	sidebarEvent(a, input.KindMouseClick, start.X+1, start.Y)
	for x := start.X + 2; x < last.X+last.W/2; x++ {
		a.dirty = false
		sidebarEvent(a, input.KindMouseMotion, x, 0)
		if !a.dirty {
			t.Fatal("motion did not schedule frame", x)
		}
		if ws.Tabs[0].ID != first || ws.TabRef().ID != active {
			t.Fatal("motion changed order/focus")
		}
	}
	sidebarEvent(a, input.KindMouseRelease, last.X+1, 0)
	if ws.Tabs[1].ID != first || ws.TabRef().ID != active {
		t.Fatal("left half did not insert before last while retaining focus")
	}
	a.draw()
	before := []int{ws.Tabs[0].ID, ws.Tabs[1].ID, ws.Tabs[2].ID}
	sidebarEvent(a, input.KindMouseClick, a.tabHits[0].X+1, 0)
	sidebarEvent(a, input.KindMouseMotion, last.X+1, 0)
	sidebarEvent(a, input.KindMouseMotion, last.X+1, 2)
	if a.tabGesture.drop != -1 {
		t.Fatal("outside target retained marker")
	}
	sidebarEvent(a, input.KindMouseRelease, last.X+1, 2)
	for i, id := range before {
		if ws.Tabs[i].ID != id {
			t.Fatal("outside release reordered")
		}
	}
	if ws.TabRef().ID != active || a.tabGesture != nil {
		t.Fatal("cancel retained gesture or changed focus")
	}
	path := filepath.Join(t.TempDir(), "session.json")
	if err := session.Save(path, a.st.Snapshot()); err != nil {
		t.Fatal(err)
	}
	saved, err := session.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	restored := model.Restore(saved).ActiveWorkspace()
	for i, id := range before {
		if restored.Tabs[i].ID != id {
			t.Fatal("save/restore lost order")
		}
	}
	if restored.TabRef().ID != active {
		t.Fatal("save/restore lost focus")
	}
}
