package vt

import (
	"image/color"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestUnchangedBoundaryProjectionMatchesStreaming(t *testing.T) {
	for _, limited := range []bool{false, true} {
		for _, wide := range []bool{false, true} {
			old, indexed := NewScreen(6, 3), NewScreen(6, 3)
			row := uv.Line{{Content: "a", Width: 1}, {Content: "b", Width: 1}, {Content: "c", Width: 1}, {Content: "d", Width: 1}, {Content: "e", Width: 1}, {Content: "f", Width: 1}}
			if wide {
				row = uv.Line{{Content: "e\u0301", Width: 1}, uv.EmptyCell, {Content: "界", Width: 2, Style: uv.Style{Attrs: uv.AttrBold}, Link: uv.Link{URL: "https://example.com"}}, {}, uv.EmptyCell, {Content: "z", Width: 1}}
			}
			for _, s := range []*Screen{old, indexed} {
				if limited {
					s.scrollback.SetMaxBytes(8 * (scrollbackRowBytes + 6*scrollbackCellBytes))
				}
				for i := 0; i < 8; i++ {
					s.scrollback.Push(row)
				}
				copy(s.buf.Line(0), row)
				s.cur.X = 3
			}
			flags := make([]ReflowRow, 11)
			for y := range flags[:9] {
				flags[y] = ReflowRow{Continued: y > 0, Width: 6}
			}
			for step, size := range [][2]int{{4, 3}, {8, 4}, {2, 3}, {7, 2}, {1, 3}, {6, 3}, {9, 4}} {
				get := func(y int) ReflowRow {
					if y < len(flags) {
						return flags[y]
					}
					return ReflowRow{}
				}
				if step == 0 {
					indexed.prepareReflowHistory(get)
				}
				view := indexed.reflowIndexed(size[0], size[1], get, false, true)
				flags = old.referenceStreamingReflow(size[0], size[1], flags, false)
				if !reflect.DeepEqual(old.buf.Lines, indexed.buf.Lines) || old.cur.Position != indexed.cur.Position || !reflect.DeepEqual(old.scrollback.Lines(), indexed.scrollback.Lines()) || old.scrollback.Bytes() != indexed.scrollback.Bytes() || !reflect.DeepEqual(flags, view.materialize()) {
					t.Fatalf("limited=%v wide=%v step=%d size=%v projection changed contents", limited, wide, step, size)
				}
			}
		}
	}
}

func TestIndexedReflowMatchesStreamingThroughResizeAndOutput(t *testing.T) {
	for _, limited := range []bool{false, true} {
		for _, phantom := range []bool{false, true} {
			old, indexed := NewScreen(6, 3), NewScreen(6, 3)
			style := uv.Style{Fg: color.RGBA{R: 255, A: 255}}
			row := uv.Line{{Content: "a", Width: 1}, {Content: "b", Width: 1}, {Content: "界", Width: 2, Style: style, Link: uv.Link{URL: "https://example.com"}}, {}, uv.EmptyCell, {Content: "e\u0301", Width: 1}}
			for _, s := range []*Screen{old, indexed} {
				if limited {
					s.scrollback.SetMaxBytes(4 * (scrollbackRowBytes + 6*scrollbackCellBytes))
				}
				for i := 0; i < 4; i++ {
					s.scrollback.Push(row)
				}
				copy(s.buf.Line(0), row)
				copy(s.buf.Line(1), row)
				s.cur.X, s.cur.Y = 6-1, 1
			}
			flags := []ReflowRow{{Width: 6}, {Continued: true, Width: 6}, {Width: 6}, {Width: 6}, {Continued: true, Width: 6}, {Width: 6}, {}}
			for step, size := range [][2]int{{4, 3}, {8, 5}, {3, 2}, {6, 3}, {1, 3}, {9, 4}, {6, 3}} {
				get := func(y int) ReflowRow {
					if y >= 0 && y < len(flags) {
						return flags[y]
					}
					return ReflowRow{}
				}
				snapshot := indexed.reflowIndexed(size[0], size[1], get, phantom && step == 0, true)
				flags = old.referenceStreamingReflow(size[0], size[1], flags, phantom && step == 0)
				if !reflect.DeepEqual(old.buf.Lines, indexed.buf.Lines) || old.cur.Position != indexed.cur.Position || !reflect.DeepEqual(old.scrollback.Lines(), indexed.scrollback.Lines()) || old.scrollback.Bytes() != indexed.scrollback.Bytes() {
					for source, rows := range [][]uv.Line{old.scrollback.Lines(), old.buf.Lines} {
						other := indexed.scrollback.Lines()
						if source == 1 {
							other = indexed.buf.Lines
						}
						for y, row := range rows {
							if y < len(other) && len(row) == 0 && (row == nil) != (other[y] == nil) {
								t.Fatalf("step=%d source=%d row=%d empty nil=%v/%v", step, source, y, row == nil, other[y] == nil)
							}
							if y >= len(other) || len(row) != len(other[y]) {
								t.Fatalf("source=%d row=%d row lengths differ: %d", source, y, len(row))
							}
							for x := range row {
								if !reflect.DeepEqual(row[x], other[y][x]) {
									t.Fatalf("step=%d source=%d cell=%d,%d old=%+v indexed=%+v", step, source, x, y, row[x], other[y][x])
								}
							}
						}
					}
					t.Fatalf("limited=%v phantom=%v step=%d size=%v cursor=%v/%v history=%v/%v screen=%v/%v", limited, phantom, step, size, old.cur.Position, indexed.cur.Position, old.scrollback.Lines(), indexed.scrollback.Lines(), old.buf.Lines, indexed.buf.Lines)
				}
				for y, want := range flags {
					var got ReflowRow
					if y < snapshot.HistoryLen() {
						got = snapshot.HistoryRow(y)
					} else {
						got = snapshot.MainRow(y - snapshot.HistoryLen())
					}
					if got != want {
						t.Fatalf("step=%d row=%d flag=%v want=%v", step, y, got, want)
					}
				}
				// The mutable visible screen must win over cached former content.
				for _, s := range []*Screen{old, indexed} {
					s.SetCell(0, 0, &uv.Cell{Content: "N", Width: 1})
				}
			}
		}
	}
}
