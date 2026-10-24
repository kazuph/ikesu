package tui

import (
	"io"
	"reflect"
	"testing"

	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
)

// These ownership transitions were measured against the released herdr 0.2.66
// with two/three real PTY clients; mouse presses/releases and blur do not claim it.
func TestViewerActivityClaimsVisibleTabSize(t *testing.T) {
	teststate.Lock(t)
	for _, tc := range []struct {
		name, bytes string
		claims      bool
	}{
		{"hover", "\x1b[<35;40;10M", true},
		{"wheel", "\x1b[<64;40;10M", true},
		{"key", "x", true},
		{"paste", "\x1b[200~abc\x1b[201~", true},
		{"focus", "\x1b[I", true},
		{"press", "\x1b[<0;40;10M", false},
		{"release", "\x1b[<0;40;10m", false},
		{"blur", "\x1b[O", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := sizeOwnerRuntimeApp(t)
			if err := a.StartInitialPane(); err != nil {
				t.Fatal(err)
			}
			large := a.NewViewer(io.Discard, 196, 50, nil)
			large.Begin(a, 0, 0)
			small := a.NewViewer(io.Discard, 82, 30, nil)
			small.Begin(a, 0, 0)
			before := viewerTerminalSizes(a)
			large.Input(a, []byte(tc.bytes))
			changed := !reflect.DeepEqual(before, viewerTerminalSizes(a))
			if changed != tc.claims {
				t.Fatalf("ownership changed=%t want %t: before=%v after=%v", changed, tc.claims, before, viewerTerminalSizes(a))
			}
		})
	}
}

func TestViewerActivityLeavesOtherTabsAndViewerSelectionIndependent(t *testing.T) {
	teststate.Lock(t)
	a := sizeOwnerRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	first := a.NewViewer(io.Discard, 196, 50, nil)
	first.Begin(a, 0, 0)
	second := a.NewViewer(io.Discard, 82, 30, nil)
	second.Begin(a, 0, 0)
	second.with(a, func(ui *App) { ui.exec(model.CmdNewTab{Workspace: 0}) })
	second.Input(a, []byte("x"))
	hidden := a.st.Workspaces[0].Tabs[1].Layout.Focused()
	hiddenSize := viewerTerminalSizes(a)[hidden]
	first.Input(a, []byte("\x1b[<35;40;10M"))
	if got := viewerTerminalSizes(a)[hidden]; got != hiddenSize {
		t.Fatalf("other viewer's tab resized: %v -> %v", hiddenSize, got)
	}
	if first.tabs[first.workspace] == second.tabs[second.workspace] {
		t.Fatal("viewer selection followed peer")
	}
	firstSize := viewerTerminalSizes(a)[a.st.Workspaces[0].Tabs[0].Layout.Focused()]
	second.Resize(a, 90, 32, 0, 0)
	if got := viewerTerminalSizes(a)[a.st.Workspaces[0].Tabs[0].Layout.Focused()]; got != firstSize {
		t.Fatal("resize of other tab changed first tab", got, firstSize)
	}
}

func TestViewerHiddenTabsKeepSizesAndBasisUntilSelected(t *testing.T) {
	teststate.Lock(t)
	a := hiddenSizeApp(t)
	first := a.NewViewer(io.Discard, 196, 50, nil)
	first.Begin(a, 0, 0)
	second := a.NewViewer(io.Discard, 82, 30, nil)
	second.Begin(a, 0, 0)
	before := viewerTerminalSizes(a)
	visible := map[int]bool{}
	for _, id := range a.st.ActiveWorkspace().TabRef().Layout.PaneIDs() {
		visible[id] = true
	}
	for _, event := range []string{"x", "\x1b[<35;40;10M", "\x1b[<64;40;10M", "\x1b[I"} {
		first.Input(a, []byte(event))
		for id, size := range viewerTerminalSizes(a) {
			if !visible[id] && size != before[id] {
				t.Fatalf("hidden pane %d changed: %v -> %v", id, before[id], size)
			}
		}
	}
	// A viewer's later resize on a different tab must not change the stored
	// geometry used by an API command targeting its former tab.
	first.Rendered(a, render.Output{}, first.FocusEpoch())
	first.Input(a, []byte{2, '2'})
	if first.tabs[first.workspace] == second.tabs[second.workspace] {
		t.Fatal("tab selection did not complete")
	}
	oldPane := a.st.Workspaces[0].Tabs[0].Layout.Focused()
	oldRect := a.ptyRectForPane(oldPane)
	first.Resize(a, 210, 55, 0, 0)
	if got := a.ptyRectForPane(oldPane); got != oldRect {
		t.Fatal("hidden tab basis followed another tab's resize", oldRect, got)
	}
}

func TestViewerDisconnectOnlyReplacesDepartingOwner(t *testing.T) {
	teststate.Lock(t)
	for gone := 0; gone < 3; gone++ {
		for active := 1; active < 3; active++ {
			a := sizeOwnerRuntimeApp(t)
			if err := a.StartInitialPane(); err != nil {
				t.Fatal(err)
			}
			var views []*Viewer
			for _, size := range [][2]int{{196, 50}, {82, 30}, {140, 40}} {
				v := a.NewViewer(io.Discard, size[0], size[1], nil)
				v.Begin(a, 0, 0)
				views = append(views, v)
			}
			views[active].Input(a, []byte("x"))
			views[gone].Detach(a)
			expected := active
			if gone == active {
				expected = 0
				if gone == 0 {
					expected = 1
				}
			}
			if a.sizeOwners[a.currentSizeTab()].viewer != views[expected] {
				t.Fatalf("gone=%d active=%d: wrong remaining owner", gone, active)
			}
			checkCurrentTabSizes(t, a, views[expected].cols, views[expected].rows)
		}
	}
}

func TestViewerAPIHiddenSplitKeepsItsOwnGeometry(t *testing.T) {
	teststate.Lock(t)
	a := sizeOwnerRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	first := a.NewViewer(io.Discard, 196, 50, nil)
	first.Begin(a, 0, 0)
	a.exec(model.CmdNewTab{Workspace: 0, NoFocus: true})
	first.Input(a, []byte{2, '2'})
	second := a.NewViewer(io.Discard, 82, 30, nil)
	second.Begin(a, 0, 0)
	second.Input(a, []byte{2, '1'})
	tab := a.st.Workspaces[0].Tabs[1]
	pid := tab.Layout.Focused()
	before := viewerTerminalSizes(a)[pid]
	a.exec(model.CmdSplitPane{Pane: pid, Dir: layout.Vertical, Ratio: .5})
	after := viewerTerminalSizes(a)
	for _, id := range tab.Layout.PaneIDs() {
		if after[id][1] != before[1] || after[id][0] != (before[0]-3)/2 {
			t.Fatal("hidden API split used another tab's viewport", before, after[id])
		}
	}
}

func TestViewerSizeBasisRetainsExactCellPixels(t *testing.T) {
	teststate.Lock(t)
	a := sizeOwnerRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	v := a.NewViewer(io.Discard, 196, 50, nil)
	v.Begin(a, 196*8, 50*16)
	p := a.sup.Get(a.st.FocusedPane())
	if w, h := p.CellPixels(); w != 8 || h != 16 {
		t.Fatal("attachment pixels lost", w, h)
	}
	v.Input(a, []byte("\x1b[6;18;9t"))
	if w, h := p.CellPixels(); w != 9 || h != 18 {
		t.Fatal("cell size reply lost", w, h)
	}
	v.Resize(a, 210, 55, 210*10, 55*20)
	if w, h := p.CellPixels(); w != 10 || h != 20 {
		t.Fatal("resize pixels lost", w, h)
	}
}
