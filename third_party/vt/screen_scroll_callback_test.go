package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"image/color"
	"reflect"
	"testing"
)

func TestScrollDropCallbackMatchesOriginal(t *testing.T) {
	originalEmpty := uv.EmptyCell
	defer func() { uv.EmptyCell = originalEmpty }()
	for _, mutation := range []string{"none", "prefix-style", "prefix-wide", "empty-style", "empty-content", "suffix-style", "suffix-wide", "suffix-zero", "background", "empty-width"} {
		for _, budgetRows := range []int{0, 1, 2} {
			run := func(reference bool) (*Screen, int) {
				uv.EmptyCell = originalEmpty
				s := NewScreen(8, 3)
				s.cb = &Callbacks{}
				s.scrollback = NewScrollback(1)
				s.scrollback.SetMaxBytes(budgetRows * (scrollbackRowBytes + 3*scrollbackCellBytes))
				row := uv.Line{{Content: "a", Width: 1}, {Content: "b", Width: 1}, {Content: "c", Width: 1}}
				for i := 0; i < 3; i++ {
					s.scrollback.Push(row)
				}
				copy(s.buf.Lines[0], row)
				drops := 0
				s.scrollback.OnDrop = func() {
					drops++
					switch mutation {
					case "background":
						s.cur.Pen.Bg = color.RGBA{R: 255, A: 255}
					case "empty-width":
						uv.EmptyCell.Width = 2
					case "suffix-style":
						s.buf.Lines[0][7].Style.Attrs = uv.AttrBold
					case "suffix-wide":
						s.buf.Lines[0][6] = uv.Cell{Content: "世", Width: 2}
						s.buf.Lines[0][7] = uv.Cell{}
					case "suffix-zero":
						s.buf.Lines[0][7] = uv.Cell{}
					case "prefix-style":
						s.buf.Lines[0][0].Style.Attrs = uv.AttrBold
					case "prefix-wide":
						s.buf.Lines[0][0] = uv.Cell{Content: "世", Width: 2}
					case "empty-style":
						uv.EmptyCell.Style.Attrs = uv.AttrBold
					case "empty-content":
						uv.EmptyCell.Content = "z"
					}
				}
				if reference {
					s.originalDeleteLine(1)
				} else {
					s.DeleteLine(1)
				}
				return s, drops
			}
			got, gd := run(false)
			want, wd := run(true)
			if gd != wd || !reflect.DeepEqual(got.buf, want.buf) || !reflect.DeepEqual(got.scrollback.Lines(), want.scrollback.Lines()) || got.scrollback.Bytes() != want.scrollback.Bytes() {
				t.Fatalf("mutation=%s budgetRows=%d drops=%d/%d", mutation, budgetRows, gd, wd)
			}
		}
	}
}
