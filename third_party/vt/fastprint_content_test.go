package vt

import (
	"image/color"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestPrintRunPreservesExistingCellFields(t *testing.T) {
	cells := []uv.Cell{
		{Content: "x", Width: 1},
		{Content: "x", Width: 1, Style: uv.Style{Attrs: uv.AttrBold}},
		{Content: "x", Width: 1, Style: uv.Style{Underline: uv.UnderlineSingle}},
		{Content: "x", Width: 1, Style: uv.Style{Fg: color.RGBA{R: 1}}},
		{Content: "x", Width: 1, Style: uv.Style{Bg: color.RGBA{G: 1}}},
		{Content: "x", Width: 1, Style: uv.Style{UnderlineColor: color.RGBA{B: 1}}},
		{Content: "x", Width: 1, Style: uv.Style{Fg: blankPredicateSliceColor{1}}},
		{Content: "x", Width: 1, Link: uv.Link{URL: "url"}},
		{Content: "x", Width: 1, Link: uv.Link{Params: "params"}},
	}
	cases := 0
	for index, cell := range cells {
		for column := 0; column < 8; column++ {
			for _, input := range []string{"abcde\r", "\x1b[31mabcde\r", "\x1b]8;;url\x1b\\abcde\r", "ab́c\r"} {
				fast, slow := NewEmulator(8, 3), NewEmulator(8, 3)
				slow.disableFastPrint = true
				for _, e := range []*Emulator{fast, slow} {
					e.Write([]byte("xxxxxx\r"))
					*e.CellAt(column, 0) = cell
					e.scr.buf.Touched = make([]*uv.LineData, 3)
					e.scr.buf.Touched[0] = &uv.LineData{FirstCell: 0, LastCell: 8}
					e.Write([]byte(input))
				}
				if !reflect.DeepEqual(fast.scr.buf, slow.scr.buf) || dumpEmulator(fast) != dumpEmulator(slow) {
					t.Fatalf("cell=%d column=%d input=%q", index, column, input)
				}
				cases++
			}
		}
	}
	t.Logf("cells, styles, links, dirty ranges and cursor matched in %d cases", cases)
}
