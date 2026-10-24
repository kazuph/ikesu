package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
)

func TestParityPaneBorderDragKeepsPressedBorderCellActualPTY(t *testing.T) {
	teststate.Lock(t)
	for _, dir := range []layout.Direction{layout.Vertical, layout.Horizontal} {
		for _, edge := range []int{-1, 0} {
			a := specRuntimeApp(t)
			if err := a.StartInitialPane(); err != nil {
				t.Fatal(err)
			}
			a.exec(model.CmdSplitFocused{Dir: dir})
			l := a.st.ActiveWorkspace().ActiveLayout()
			area := a.st.PanesArea()
			b := l.Splits(area)[0]
			x, y := b.Pos+edge, area.Y+area.H/2
			if dir == layout.Horizontal {
				x, y = area.X+area.W/2, b.Pos+edge
			}
			a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: x, Y: y, Button: uv.MouseLeft}})
			if a.sep == nil {
				t.Fatal("border press did not begin drag", dir, edge)
			}
			delta := 4
			if dir == layout.Vertical {
				x += delta
			} else {
				y += delta
			}
			a.onMouse(input.Event{Kind: input.KindMouseMotion, Mouse: uv.Mouse{X: x, Y: y, Button: uv.MouseLeft}})
			got := l.Splits(area)[0].Pos
			if got != b.Pos+delta {
				t.Fatal("pressed border cell must retain its offset", dir, edge, got, b.Pos+delta)
			}
			if dir == layout.Vertical {
				x += delta
			} else {
				y += delta
			}
			a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: uv.Mouse{X: x, Y: y, Button: uv.MouseLeft}})
			if a.sep != nil {
				t.Fatal("release did not end divider drag")
			}
			if got := l.Splits(area)[0].Pos; got != b.Pos+2*delta {
				t.Fatal("release position was not applied", dir, edge, got, b.Pos+2*delta)
			}
		}
	}
}
