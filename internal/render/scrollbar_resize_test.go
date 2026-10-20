package render

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kazuph/ikesu/internal/layout"
)

func TestResizeScrollbarVisibilityPreservesContentAndUsesCurrentHistory(t *testing.T) {
	// Observations 853/855: a three-row pane has a one-row thumb with
	// existing history; resize-created history stays hidden until output.
	inner := layout.Rect{X: 1, Y: 1, W: 5, H: 3}
	for _, suppressed := range []bool{true, false} {
		f := NewFrame(8, 5)
		for y := inner.Y; y < inner.Y+inner.H; y++ {
			f.Set(inner.X+inner.W-1, y, "Z", uv.Style{})
		}
		drawGutter(f, inner, &PaneSnap{ScrollN: 40, SuppressScrollbar: suppressed}, 0, true)
		for y := 0; y < inner.H; y++ {
			want := "Z"
			if !suppressed {
				want = "▕"
				if y == inner.H-1 {
					want = "▐"
				}
			}
			if got := f.Cell(inner.X+inner.W-1, inner.Y+y).Content; got != want {
				t.Fatalf("suppressed=%v row=%d got=%q want=%q", suppressed, y, got, want)
			}
		}
	}
}
