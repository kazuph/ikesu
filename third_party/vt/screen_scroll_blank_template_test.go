package vt

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"reflect"
	"testing"
)

func TestScrollBlankTemplateDoesNotAliasCells(t *testing.T) {
	for _, notify := range []bool{false, true} {
		t.Run(fmt.Sprint("notify=", notify), func(t *testing.T) {
			testScrollBlankTemplateDoesNotAliasCells(t, notify)
		})
	}
}

func testScrollBlankTemplateDoesNotAliasCells(t *testing.T, notify bool) {
	got, want := NewScreen(8, 3), NewScreen(8, 3)
	for _, s := range []*Screen{got, want} {
		s.cb = &Callbacks{}
		s.scrollback.SetMaxBytes(3 * (scrollbackRowBytes + 8*scrollbackCellBytes))
		if notify {
			s.scrollback.OnDrop = func() {}
		}
	}
	for _, width := range []int{1, 8, 160, 3, 160} {
		for _, s := range []*Screen{got, want} {
			s.Resize(width, 3)
			s.cur.X, s.cur.Y = 0, 0
		}
		for rep := 0; rep < 5; rep++ {
			for _, s := range []*Screen{got, want} {
				for row := 0; row < s.buf.Height(); row++ {
					for x := range s.buf.Line(row) {
						screenTestRaw(s, x, row, uv.Cell{Content: "x", Width: 1})
					}
				}
			}
			got.DeleteLine(1)
			want.originalDeleteLine(1)
			if !reflect.DeepEqual(screenTestState(got.buf), screenTestState(want.buf)) || !reflect.DeepEqual(got.scrollback.Lines(), want.scrollback.Lines()) || got.scrollback.Bytes() != want.scrollback.Bytes() {
				t.Fatalf("width=%d rep=%d", width, rep)
			}
			// Mutate the public cell from the cleared row; the next clear must not reuse it.
			for _, s := range []*Screen{got, want} {
				c := *s.CellAt(width-1, 2)
				c.Content = "edited"
				c.Style.Attrs = uv.AttrBold
				c.Link.URL = "changed"
				s.SetCell(width-1, 2, &c)
			}
			got.DeleteLine(1)
			want.originalDeleteLine(1)
			if !reflect.DeepEqual(screenTestState(got.buf), screenTestState(want.buf)) || !reflect.DeepEqual(got.scrollback.Lines(), want.scrollback.Lines()) || got.scrollback.Bytes() != want.scrollback.Bytes() {
				t.Fatalf("public alias width=%d rep=%d", width, rep)
			}
		}
	}
}
