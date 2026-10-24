package vt

import (
	"image/color"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestIndexedHistoryReadsDropsAndPullsMatchRealReflow(t *testing.T) {
	style := uv.Style{Fg: color.RGBA{R: 255, A: 255}}
	lines := []uv.Line{
		{{Content: "a", Width: 1}, {Content: "b", Width: 1}, {Content: "c", Width: 1}},
		{{Content: "界", Width: 2, Style: style, Link: uv.Link{URL: "https://example.com"}}, {}, uv.EmptyCell, {Content: "e\u0301", Width: 1}},
		nil,
		{{Content: "z", Width: 1}},
	}
	for width := 1; width <= 6; width++ {
		h := &reflowHistory{}
		var want []uv.Line
		for _, line := range lines {
			h.append(line)
			s := NewScreen(max(1, len(line)+1), 1)
			copy(s.buf.Line(0), line)
			s.reflow(width, 1, nil, false)
			for _, row := range append(s.scrollback.Lines(), s.buf.Line(0)) {
				trim := NewScrollback(1)
				trim.Push(row)
				want = append(want, trim.Line(0))
			}
		}
		h.view.width, h.view.rows = width, len(want)
		snapshot := h.view
		verify := func() {
			charge := NewScrollback(len(want))
			charge.SetMaxBytes(len(want) * (scrollbackRowBytes + width*scrollbackCellBytes))
			for y, row := range want {
				charge.Push(row)
				if got := h.line(y); !reflect.DeepEqual(got, row) && !(len(got) == 0 && len(row) == 0) {
					t.Fatalf("width=%d row=%d got=%v want=%v", width, y, got, row)
				}
				for x := range row {
					if got := h.cellAt(x, y); got == nil || !got.Equal(&row[x]) {
						t.Fatalf("width=%d row=%d col=%d", width, y, x)
					}
				}
				if h.cellAt(len(row), y) != nil {
					t.Fatal("out-of-range cell exists")
				}
			}
			if charge.Bytes() != h.view.bytes() {
				t.Fatal("charge differs")
			}
		}
		verify()
		before := h.view.bytes()
		removed := h.dropRows(1)
		want = want[1:]
		verify()
		if before-removed != h.view.bytes() {
			t.Fatal("front charge differs")
		}
		for len(want) > 0 {
			row, charge := h.popLast()
			if !reflect.DeepEqual(row, want[len(want)-1]) && !(len(row) == 0 && len(want[len(want)-1]) == 0) || charge < scrollbackRowBytes {
				t.Fatal("pulled row or charge differs")
			}
			want = want[:len(want)-1]
			verify()
		}
		if len(h.pool.refs)+len(h.pool.plain)+len(h.pool.general) != 0 {
			t.Fatal("evicted text shape retained")
		}
		if snapshot.rows == 0 || snapshot.bytes() <= 0 {
			t.Fatal("previous metadata snapshot changed")
		}
		for _, line := range h.lines {
			if line != nil {
				t.Fatal("evicted cell data retained")
			}
		}
	}
}
