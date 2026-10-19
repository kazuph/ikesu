package tui

import (
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
)

func viewerTerminalSizes(a *App) map[int][2]int {
	sizes := map[int][2]int{}
	for _, ws := range a.st.Workspaces {
		for _, tab := range ws.Tabs {
			for _, id := range tab.Layout.PaneIDs() {
				if p := a.sup.Get(id); p != nil {
					p.WithEmu(func(e *vt.Emulator) { sizes[id] = [2]int{e.Width(), e.Height()} })
				}
			}
		}
	}
	return sizes
}

func TestViewerOrdinaryInputKeepsPTYSizeWithTwoOrThreeClientsAndSplits(t *testing.T) {
	teststate.Lock(t)
	for _, peers := range []int{1, 2, 3} {
		for _, split := range []bool{false, true} {
			t.Run(fmt.Sprintf("clients=%d/split=%t", peers, split), func(t *testing.T) {
				a := specRuntimeApp(t)
				if err := a.StartInitialPane(); err != nil {
					t.Fatal(err)
				}
				dimensions := [][2]int{{200, 50}, {100, 30}, {180, 40}}
				var viewers []*Viewer
				for _, size := range dimensions[:peers] {
					v := a.NewViewer(io.Discard, size[0], size[1], nil)
					v.Begin(a, 0, 0)
					viewers = append(viewers, v)
				}
				owner := viewers[len(viewers)-1]
				if split {
					owner.with(a, func(ui *App) { ui.exec(model.CmdSplitFocused{Dir: layout.Vertical}) })
				}
				want := viewerTerminalSizes(a)
				for _, v := range viewers {
					for _, input := range []string{"x", "\x1b[I", "\x1b[<0;40;10M\x1b[<0;40;10m", "\x1b[<64;40;10M"} {
						v.Input(a, []byte(input))
						if got := viewerTerminalSizes(a); !reflect.DeepEqual(got, want) {
							t.Fatalf("input %q on %dx%d changed PTYs: got %v, want %v", input, v.cols, v.rows, got, want)
						}
					}
				}
			})
		}
	}
}

func TestViewerOnlySizeOwnerResizeChangesPTYAndDrawsStayLocal(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	first := a.NewViewer(io.Discard, 200, 50, nil)
	first.Begin(a, 0, 0)
	second := a.NewViewer(io.Discard, 100, 30, nil)
	second.Begin(a, 0, 0)
	check := func(w, h int) {
		t.Helper()
		for id, size := range viewerTerminalSizes(a) {
			if size != [2]int{w, h} {
				t.Fatalf("pane%d: got %v want %dx%d", id, size, w, h)
			}
		}
	}
	check(71, 26)
	first.Resize(a, 220, 55, 0, 0)
	check(71, 26)
	for _, tc := range []struct {
		v    *Viewer
		w, h int
	}{{first, 220, 55}, {second, 100, 30}} {
		var got [2]int
		tc.v.SetSnapshotSink(func(in render.Input) error { got = [2]int{in.State.Cols, in.State.Rows}; return nil })
		tc.v.Draw(a, a.SessionPaneSnaps())
		if got != [2]int{tc.w, tc.h} {
			t.Fatal("render viewport followed another client", got)
		}
	}
	check(71, 26)
	second.Resize(a, 101, 31, 0, 0)
	check(72, 27)
	if a.st.Cols != 101 || a.st.Rows != 31 {
		t.Fatal("session dimensions were taken by a non-owner", a.st.Cols, a.st.Rows)
	}
}

func TestViewerExplicitSplitFromNonOwnerUsesSizeOwner(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	first := a.NewViewer(io.Discard, 200, 50, nil)
	first.Begin(a, 0, 0)
	owner := a.NewViewer(io.Discard, 100, 30, nil)
	owner.Begin(a, 0, 0)
	first.with(a, func(ui *App) { ui.exec(model.CmdSplitFocused{Dir: layout.Vertical}) })
	for id, size := range viewerTerminalSizes(a) {
		if size != [2]int{34, 26} {
			t.Fatalf("split pane%d used caller viewport: %v", id, size)
		}
	}
	owner.with(a, func(ui *App) { ui.exec(model.CmdZoomPane{Pane: ui.focusedPane(), Mode: "toggle"}) })
	focused := a.st.Workspaces[0].Tabs[0].Layout.Focused()
	if got := viewerTerminalSizes(a)[focused]; got != [2]int{71, 26} {
		t.Fatal("zoom did not use size owner", got)
	}
	owner.with(a, func(ui *App) {
		ui.exec(model.CmdZoomPane{Pane: ui.focusedPane(), Mode: "toggle"})
		ui.exec(model.CmdToggleSidebar{})
	})
	for id, size := range viewerTerminalSizes(a) {
		if size != [2]int{45, 26} {
			t.Fatalf("sidebar did not change pane%d: %v", id, size)
		}
	}
}

func TestSingleViewerResizeSidebarSplitAndZoomKeepExistingGeometry(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	v := a.NewViewer(io.Discard, 120, 35, nil)
	v.Begin(a, 0, 0)
	checkAll := func(w, h int) {
		t.Helper()
		for id, size := range viewerTerminalSizes(a) {
			if size != [2]int{w, h} {
				t.Fatalf("pane%d: got %v want %dx%d", id, size, w, h)
			}
		}
	}
	checkAll(91, 31)
	v.Resize(a, 140, 40, 0, 0)
	checkAll(111, 36)
	v.with(a, func(ui *App) { ui.exec(model.CmdToggleSidebar{}) })
	checkAll(133, 36)
	v.with(a, func(ui *App) { ui.exec(model.CmdSplitFocused{Dir: layout.Vertical}) })
	checkAll(65, 36)
	v.with(a, func(ui *App) { ui.exec(model.CmdZoomPane{Pane: ui.focusedPane(), Mode: "on"}) })
	focused := a.st.FocusedPane()
	if got := viewerTerminalSizes(a)[focused]; got != [2]int{133, 36} {
		t.Fatal("single viewer zoom no longer updates PTY", got)
	}
	v.with(a, func(ui *App) { ui.exec(model.CmdZoomPane{Pane: ui.focusedPane(), Mode: "off"}) })
	checkAll(65, 36)
}
