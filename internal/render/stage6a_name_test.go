package render

import (
	"github.com/kazuph/goterrace/internal/layout"
	"strings"
	"testing"
)

func TestStage6aNarrowNameInputRetainsFrameMargins(t *testing.T) {
	for _, tc := range []struct{ cols, rows, w, h int }{{20, 7, 16, 5}, {56, 7, 52, 5}, {160, 45, 56, 7}} {
		d := &Dialog{InputEdit: true, Title: "Rename workspace", Input: "parity"}
		r, _, _ := DialogRects(d, layout.Rect{W: tc.cols, H: tc.rows})
		if r.W != tc.w || r.H != tc.h || r.X != (tc.cols-tc.w)/2 || r.Y != (tc.rows-tc.h)/2 {
			t.Fatal(tc, r)
		}
		f := NewFrame(tc.cols, tc.rows)
		out := &Output{}
		drawNameInput(f, d, out)
		if tc.rows == 7 {
			var s strings.Builder
			for y := 0; y < tc.rows; y++ {
				for x := 0; x < tc.cols; x++ {
					s.WriteString(f.Cell(x, y).Content)
				}
			}
			if strings.Contains(s.String(), "save") || strings.Contains(s.String(), "rename") || out.AnchorVisible {
				t.Fatal("undersized frame showed controls", s.String(), out.AnchorVisible)
			}
		}
	}
}
