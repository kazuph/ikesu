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
	for _, mutation := range []string{"none", "prefix-style", "prefix-wide", "empty-style", "empty-content", "suffix-style", "suffix-wide", "suffix-zero", "background", "empty-width", "buffer-width", "empty-color"} {
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
				s.writeScreenLine(0, row)
				drops := 0
				s.scrollback.OnDrop = func() {
					drops++
					switch mutation {
					case "buffer-width":
						s.buf.Resize(s.Width()+2, s.Height())
					case "empty-color":
						uv.EmptyCell.Style.Fg = blankPredicateSliceColor{1}
					case "background":
						s.cur.Pen.Bg = color.RGBA{R: 255, A: 255}
					case "empty-width":
						uv.EmptyCell.Width = 2
					case "suffix-style":
						fixtureCell := *s.CellAt(7, 0)
						fixtureCell.Style.Attrs = uv.AttrBold
						screenTestRaw(s, 7, 0, fixtureCell)
					case "suffix-wide":
						screenTestRaw(s, 6, 0, uv.Cell{Content: "世", Width: 2})
						screenTestRaw(s, 7, 0, uv.Cell{})
					case "suffix-zero":
						screenTestRaw(s, 7, 0, uv.Cell{})
					case "prefix-style":
						fixtureCell := *s.CellAt(0, 0)
						fixtureCell.Style.Attrs = uv.AttrBold
						screenTestRaw(s, 0, 0, fixtureCell)
					case "prefix-wide":
						screenTestRaw(s, 0, 0, uv.Cell{Content: "世", Width: 2})
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
			if gd != wd || !reflect.DeepEqual(screenTestState(got.buf), screenTestState(want.buf)) || !reflect.DeepEqual(got.scrollback.Lines(), want.scrollback.Lines()) || got.scrollback.Bytes() != want.scrollback.Bytes() {
				t.Fatalf("mutation=%s budgetRows=%d drops=%d/%d", mutation, budgetRows, gd, wd)
			}
		}
	}
}
