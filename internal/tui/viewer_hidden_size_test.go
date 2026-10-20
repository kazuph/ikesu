package tui

import (
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
)

func hiddenSizeApp(t *testing.T) *App {
	t.Helper()
	a := specRuntimeApp(t)
	a.st.RightSidebarMode = "hidden"
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdNewTab{Workspace: 0, NoFocus: true})
	a.exec(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	a.exec(model.CmdNewTab{Workspace: 1, NoFocus: true})
	for wi, ws := range a.st.Workspaces {
		for _, tab := range ws.Tabs {
			a.exec(model.CmdSplitPane{Workspace: wi, Pane: tab.Layout.Focused(), Dir: layout.Vertical, Ratio: .5})
		}
	}
	return a
}

func checkAllHiddenSizes(t *testing.T, a *App, cols, rows int) {
	t.Helper()
	want := map[int][2]int{}
	state := *a.st
	state.Cols, state.Rows = cols, rows
	for wi, ws := range state.Workspaces {
		state.Active = wi
		for _, tab := range ws.Tabs {
			for _, pr := range state.PaneRectsForDisplay(tab, state.PanesArea()) {
				w, h := model.PaneTermSize(pr.Rect, state.PaneBorders)
				want[pr.ID] = [2]int{w, h}
			}
		}
	}
	if got := viewerTerminalSizes(a); !reflect.DeepEqual(got, want) {
		t.Fatalf("all panes must update before selection at %dx%d: got %v want %v", cols, rows, got, want)
	}
}

func TestViewerHiddenPanesUpdateAtSizeEventsBeforeAnySelection(t *testing.T) {
	teststate.Lock(t)
	for _, peers := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(peers), func(t *testing.T) {
			a := hiddenSizeApp(t)
			var viewers []*Viewer
			for _, size := range [][2]int{{200, 50}, {100, 30}, {180, 40}}[:peers] {
				v := a.NewViewer(io.Discard, size[0], size[1], nil)
				v.Begin(a, 0, 0)
				viewers = append(viewers, v)
				checkAllHiddenSizes(t, a, size[0], size[1])
			}
			owner := viewers[len(viewers)-1]
			owner.Resize(a, 190, 45, 0, 0)
			checkAllHiddenSizes(t, a, 190, 45)
			if peers > 1 {
				viewers[0].Resize(a, 220, 55, 0, 0)
				checkAllHiddenSizes(t, a, 190, 45)
				owner.Detach(a)
				if peers == 3 {
					checkAllHiddenSizes(t, a, 190, 45)
					viewers[1].Detach(a)
				}
				checkAllHiddenSizes(t, a, 220, 55)
			}
		})
	}
}

func hiddenSizeNavigation(t *testing.T, a *App, v *Viewer) {
	t.Helper()
	want := viewerTerminalSizes(a)
	check := func() {
		t.Helper()
		if got := viewerTerminalSizes(a); !reflect.DeepEqual(got, want) {
			t.Fatalf("selection changed PTYs: got %v want %v", got, want)
		}
	}
	v.Input(a, []byte{2, '2'})
	if v.tabs[a.st.Workspaces[0].ID] != a.st.Workspaces[0].Tabs[1].ID {
		t.Fatal("tab key did not select second tab")
	}
	check()
	var region render.Output
	workspaceY := -1
	v.SetSnapshotSink(func(in render.Input) error {
		region = *render.Compose(in)
		for _, pos := range render.SidebarViewForInput(in).Rows {
			if !pos.Right && pos.Row.Kind == render.RowWorkspace && pos.Row.Ws == 1 {
				workspaceY = pos.Y
			}
		}
		return nil
	})
	v.Draw(a, a.SessionPaneSnaps())
	v.Rendered(a, region, 0)
	if len(region.TabHits) < 2 {
		t.Fatal("tab click targets absent")
	}
	click := func(x, y int) {
		v.Input(a, []byte(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x+1, y+1, x+1, y+1)))
		check()
	}
	click(region.TabHits[0].X, region.TabHits[0].Y)
	if v.tabs[a.st.Workspaces[0].ID] != a.st.Workspaces[0].Tabs[0].ID {
		t.Fatal("tab click did not select first tab")
	}
	if workspaceY < 0 {
		t.Fatal("workspace click target absent")
	}
	click(2, workspaceY)
	if v.workspace != a.st.Workspaces[1].ID {
		t.Fatal("workspace click did not select second workspace")
	}
	v.Input(a, []byte{2, 'o'})
	check()
	if v.workspace != a.st.Workspaces[0].ID {
		t.Fatal("workspace key did not return to first workspace")
	}
}

func TestViewerHiddenPaneSizesDoNotChangeOnKeyOrClickSelection(t *testing.T) {
	teststate.Lock(t)
	for _, peers := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(peers), func(t *testing.T) {
			a := hiddenSizeApp(t)
			cfg, err := config.Decode([]byte("[keys]\nnext_workspace='prefix+o'\nopen_notification_target=''\n"))
			if err != nil {
				t.Fatal(err)
			}
			a.sidebarConfig = cfg
			var viewers []*Viewer
			for _, size := range [][2]int{{200, 50}, {100, 30}, {180, 40}}[:peers] {
				v := a.NewViewer(io.Discard, size[0], size[1], nil)
				v.Begin(a, 0, 0)
				viewers = append(viewers, v)
			}
			hiddenSizeNavigation(t, a, viewers[0])
			owner := viewers[len(viewers)-1]
			owner.Resize(a, 190, 45, 0, 0)
			hiddenSizeNavigation(t, a, viewers[0])
			if peers > 1 {
				owner.Detach(a)
				if peers == 3 {
					viewers[1].Detach(a)
				}
				hiddenSizeNavigation(t, a, viewers[0])
			}
		})
	}
}
