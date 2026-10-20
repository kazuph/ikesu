package render

import (
	"testing"

	"github.com/kazuph/ikesu/internal/layout"
)

func TestParityHelpObservedGeometry(t *testing.T) {
	// Both dimensions come from the saved 160x45 and 100x25 distribution captures.
	for _, tc := range []struct {
		cols, rows int
		want       layout.Rect
	}{
		{160, 45, layout.Rect{X: 42, Y: 11, W: 76, H: 22}},
		{100, 25, layout.Rect{X: 12, Y: 1, W: 76, H: 22}},
	} {
		if got := HelpRect(tc.cols, tc.rows); got != tc.want {
			t.Fatalf("%dx%d: %+v want %+v", tc.cols, tc.rows, got, tc.want)
		}
	}
}
