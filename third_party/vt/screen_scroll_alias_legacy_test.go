//go:build !flatcells

package vt

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"reflect"
	"testing"
)

func TestLegacyScrollRowWindowRetainedCellPointers(t *testing.T) {
	for _, height := range []int{1, 3, 45} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			got, want := NewScreen(8, height), NewScreen(8, height)
			var aliases [2][]*uv.Cell
			for i, s := range []*Screen{got, want} {
				s.cb = &Callbacks{}
				for y := 0; y < height; y++ {
					aliases[i] = append(aliases[i], s.CellAt(0, y))
				}
			}
			for step := 0; step < 4*height+8; step++ {
				got.DeleteLine(1)
				want.originalDeleteLine(1)
				for _, cells := range aliases {
					c := cells[step%height]
					c.Content = fmt.Sprint(step)
					c.Style.Attrs = uv.AttrBold
				}
				if !reflect.DeepEqual(screenTestState(got.buf), screenTestState(want.buf)) || !reflect.DeepEqual(got.scrollback.Lines(), want.scrollback.Lines()) {
					t.Fatalf("step %d changed retained cell pointer behavior", step)
				}
			}
		})
	}
}
