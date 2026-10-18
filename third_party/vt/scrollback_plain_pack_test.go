package vt

import (
	"image/color"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestPackedRowsRejectPartialPlainPrefix(t *testing.T) {
	plain := uv.Line{{Content: "a", Width: 1}, {Content: "b", Width: 1}, {Content: "c", Width: 1}}
	for _, middle := range []uv.Cell{
		{Content: "b", Width: 1, Style: uv.Style{Attrs: uv.AttrBold}},
		{Content: "b", Width: 1, Style: uv.Style{Fg: color.RGBA{R: 255, A: 255}}},
		{Content: "b", Width: 1, Style: uv.Style{Bg: plainRowPredicateSliceColor{1}}},
		{Content: "b", Width: 1, Link: uv.Link{URL: "https://example.com"}},
		{Content: "é", Width: 1},
		{Content: "界", Width: 2},
		{},
	} {
		sb := NewScrollback(1)
		sb.SetMaxBytes(2 * (scrollbackRowBytes + len(plain)*scrollbackCellBytes))
		var want []uv.Line
		for i := 0; i < 12; i++ {
			row := append(uv.Line(nil), plain...)
			if i%3 == 2 {
				row[1] = middle
			}
			sb.Push(row)
			want = append(want, row)
			if len(want) > 2 {
				want = want[1:]
			}
			if got := sb.Lines(); !reflect.DeepEqual(got, want) {
				t.Fatalf("middle=%+v iteration=%d: got=%v want=%v", middle, i, got, want)
			}
		}
	}
}
