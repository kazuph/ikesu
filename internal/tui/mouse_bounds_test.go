package tui

import (
	"fmt"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestOutsideHostMouseDoesNotEndTabDrag(t *testing.T) {
	teststate.Lock(t)
	for _, pixel := range []bool{false, true} {
		t.Run(fmt.Sprint(pixel), func(t *testing.T) {
			a := specRuntimeApp(t)
			a.exec(model.CmdNewTab{Workspace: 0})
			a.draw()
			a.hostPixelMouse, a.cellW, a.cellH = pixel, 8, 16
			ws := a.st.ActiveWorkspace()
			first := ws.Tabs[0].ID
			start, end := a.tabHits[0], a.tabHits[1]
			feed := func(kind input.Kind, x, y int) {
				if pixel {
					x, y = x*8, y*16
				}
				a.onInput(input.Event{Kind: kind, Mouse: uv.Mouse{X: x, Y: y, Button: uv.MouseLeft}})
			}
			feed(input.KindMouseClick, start.X+1, 0)
			feed(input.KindMouseMotion, end.X+end.W-1, 0)
			gesture, target := a.tabGesture, a.tabGesture.target
			for _, kind := range []input.Kind{input.KindMouseClick, input.KindMouseMotion, input.KindMouseRelease} {
				for _, point := range [][2]int{{-1, 0}, {0, -1}, {a.st.Cols, 0}, {0, a.st.Rows}} {
					feed(kind, point[0], point[1])
					if a.tabGesture != gesture || gesture.target != target || ws.Tabs[0].ID != first {
						t.Fatal("outside event changed the pending drag", kind, point)
					}
				}
			}
			feed(input.KindMouseRelease, end.X+end.W-1, 0)
			if a.tabGesture != nil || ws.Tabs[1].ID != first {
				t.Fatal("valid release did not finish the retained drag")
			}
		})
	}
}
