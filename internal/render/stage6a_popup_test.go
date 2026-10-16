package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
	"reflect"
	"testing"
)

func TestStage6aPopupDoesNotPaintWhenMinimumExceedsScreen(t *testing.T) {
	s := model.New("/tmp", 5, 3)
	s.MobileWidthThreshold = 5
	s.Apply(model.CmdOpenPopup{WorkspaceID: s.ActiveWorkspace().ID, Width: api.PopupSize{Cells: 6}, Height: api.PopupSize{Cells: 4}})
	f := NewFrame(5, 3)
	f.Fill(0, 0, 5, 3, "B", uv.Style{})
	before := make([][]uv.Cell, 3)
	for y := 0; y < 3; y++ {
		for x := 0; x < 5; x++ {
			before[y] = append(before[y], *f.Cell(x, y))
		}
	}
	drawPopup(f, Input{State: s}, &Output{})
	for y := 0; y < 3; y++ {
		for x := 0; x < 5; x++ {
			if !reflect.DeepEqual(before[y][x], *f.Cell(x, y)) {
				t.Fatalf("popup painted undersized screen at %d,%d", x, y)
			}
		}
	}
}
