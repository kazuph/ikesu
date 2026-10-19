package tui

import (
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
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
	for _, peers := range []int{2, 3} {
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
