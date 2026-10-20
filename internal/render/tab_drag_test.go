package render

import (
	"reflect"
	"testing"

	"github.com/kazuph/ikesu/internal/layout"
)

func TestTabDropMidpointsAndObservedMarkerCells(t *testing.T) {
	hits := []layout.Rect{{X: 26, Y: 0, W: 8, H: 1}, {X: 35, Y: 0, W: 8, H: 1}, {X: 44, Y: 0, W: 8, H: 1}}
	for _, tc := range []struct{ x, y, want int }{{25, 0, -1}, {26, 0, 0}, {29, 0, 0}, {30, 0, 1}, {33, 0, 1}, {34, 0, 1}, {35, 0, 1}, {38, 0, 1}, {39, 0, 2}, {43, 0, 2}, {44, 0, 2}, {47, 0, 2}, {48, 0, 3}, {51, 0, 3}, {52, 0, -1}, {38, 1, -1}} {
		if got := TabDropIndex(hits, tc.x, tc.y); got != tc.want {
			t.Fatalf("(%d,%d)=%d want %d", tc.x, tc.y, got, tc.want)
		}
	}
	for _, tc := range []struct{ index, x int }{{0, 26}, {1, 34}, {2, 43}, {3, 51}, {-1, -1}} {
		out := &Output{Frame: NewFrame(80, 2), TabHits: hits}
		cells := append(out.Frame.Cells[:0:0], out.Frame.Cells...)
		DrawTabDrop(out, tc.index, false)
		for i, c := range out.Frame.Cells {
			if i == tc.x {
				if c.Content != "│" || c.Style.Fg != out.Frame.Palette.Accent || !reflect.DeepEqual(c.Style.Bg, cells[i].Style.Bg) || c.Style.Attrs != cells[i].Style.Attrs {
					t.Fatal("marker differs from observed cell", c)
				}
			} else if c != cells[i] {
				t.Fatalf("unrelated cell %d changed", i)
			}
		}
	}
	out := &Output{Frame: NewFrame(80, 2), TabHits: hits}
	DrawTabDrop(out, len(hits), true)
	if out.Frame.Cell(52, 0).Content != "│" || out.Frame.Cell(51, 0).Content == "│" {
		t.Fatal("attached display trailing marker must follow the final chip")
	}
}
