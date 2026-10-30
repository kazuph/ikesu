package render

import (
	"fmt"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/model"
)

func TestRestoreFailureDoesNotOverwriteBottomPaneRow(t *testing.T) {
	for _, height := range []int{4, 15} {
		for _, borders := range []bool{false, true} {
			t.Run(fmt.Sprintf("height=%d/borders=%v", height, borders), func(t *testing.T) {
				s := model.New(t.TempDir(), 120, height)
				s.SidebarVisible = false
				s.PaneBorders = borders
				pr := s.DisplayPaneRects()[0]
				inner, _ := model.PaneContentRect(pr.Rect, borders)
				grid := make([][]uv.Cell, inner.H)
				for y := range grid {
					grid[y] = make([]uv.Cell, inner.W)
					for x := range grid[y] {
						grid[y][x] = uv.Cell{Content: "Q", Width: 1}
					}
				}
				in := Input{State: s, Panes: map[int]*PaneSnap{pr.ID: {Grid: grid}}}
				before := Compose(in).Frame
				in.RestoreFailures = map[int]string{999: "failure in another tab"}
				after := Compose(in).Frame
				y := pr.Rect.Y + pr.Rect.H - 1
				for x := pr.Rect.X; x < pr.Rect.X+pr.Rect.W; x++ {
					if *before.Cell(x, y) != *after.Cell(x, y) {
						t.Fatalf("bottom row changed at (%d,%d): %q -> %q", x, y, before.Cell(x, y).Content, after.Cell(x, y).Content)
					}
				}
			})
		}
	}
}
