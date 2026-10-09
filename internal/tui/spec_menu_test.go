package tui

import (
	"reflect"
	"testing"

	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

func TestSPECExactPaneMenuOrder(t *testing.T) {
	for _, multi := range []bool{false, true} {
		for _, named := range []bool{false, true} {
			for _, zoom := range []bool{false, true} {
				st := model.New("", 90, 30)
				l := st.ActiveWorkspace().TabRef().Layout
				pid := l.Focused()
				if multi {
					l.SplitFocused(layout.Vertical, pid+1)
				}
				if named {
					st.PaneLabels = map[model.PaneID]string{model.PaneID(pid): "named"}
				}
				if zoom {
					st.ActiveWorkspace().TabRef().Zoomed = model.PaneID(pid)
				}
				a := &App{st: st}
				a.openPaneMenu(15, 8, pid)
				got := []string{}
				for _, item := range a.menu.m.Items {
					if item.Separator {
						got = append(got, "--")
					} else {
						got = append(got, item.Label)
					}
				}
				want := []string{"Split vertical", "Split horizontal", "--", "Rename pane"}
				if named {
					want = append(want, "Clear pane name")
				}
				want = append(want, "--", "New Claude Code agent", "New Codex agent", "New agy agent", "New grok agent", "New qwen agent", "--")
				if multi {
					want = append(want, "Move to left split", "Move to right split", "Move to upper split", "Move to lower split", "Equalize pane sizes", "Cycle pane layout", "Rotate panes", "Rotate panes reverse", "--")
					if zoom {
						want = append(want, "Unzoom")
					} else {
						want = append(want, "Zoom")
					}
				}
				want = append(want, "Close pane")
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("multi=%v named=%v zoom=%v menu=%v want=%v", multi, named, zoom, got, want)
				}
				if a.menu.pane != pid || a.menu.m.X != 15 || a.menu.m.Y != 8 {
					t.Fatal("menu target/coordinates")
				}
			}
		}
	}
}
