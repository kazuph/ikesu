package vt

import (
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestScrollbackIndexedPrefixAndRawSuffixContracts(t *testing.T) {
	for _, limited := range []bool{false, true} {
		h := &reflowHistory{}
		for _, text := range []string{"abcdef", "ghijkl", "mnopqr"} {
			var cells uv.Line
			for _, r := range text {
				cells = append(cells, uv.Cell{Content: string(r), Width: 1})
			}
			h.append(cells)
		}
		h.view.width = 4
		h.view.rows = h.view.index.cost(4).rows
		got, want := NewScrollback(h.view.rows), NewScrollback(h.view.rows)
		if limited {
			limit := h.view.rows * (scrollbackRowBytes + 4*scrollbackCellBytes)
			got.SetMaxBytes(limit)
			want.SetMaxBytes(limit)
		}
		for y := 0; y < h.view.rows; y++ {
			want.Push(h.line(y))
		}
		got.indexed = h
		if limited {
			got.bytes = h.view.bytes()
		}
		gotDrops, wantDrops := 0, 0
		got.OnDrop = func() { gotDrops++ }
		want.OnDrop = func() { wantDrops++ }
		verify := func() {
			if !reflect.DeepEqual(got.Lines(), want.Lines()) || got.Len() != want.Len() || got.Bytes() != want.Bytes() || gotDrops != wantDrops {
				t.Fatalf("limited=%v got=%v want=%v bytes=%d/%d drops=%d/%d", limited, got.Lines(), want.Lines(), got.Bytes(), want.Bytes(), gotDrops, wantDrops)
			}
			var scratch uv.Line
			for y := 0; y < got.Len(); y++ {
				scratch = got.lineInto(y, scratch)
				if !reflect.DeepEqual(scratch, want.Line(y)) {
					t.Fatal("scratch line differs")
				}
				for x := 0; x <= 4; x++ {
					a, b := got.CellAt(x, y), want.CellAt(x, y)
					if !reflect.DeepEqual(a, b) {
						t.Fatalf("cell %d,%d differs", x, y)
					}
				}
			}
		}
		verify()
		newRow := uv.Line{{Content: "X", Width: 1}}
		got.Push(newRow)
		want.Push(newRow)
		verify()
		for i := 0; i < 2; i++ {
			if !reflect.DeepEqual(got.popLast(), want.popLast()) {
				t.Fatal("pulled row differs")
			}
			verify()
		}
		got.SetMaxLines(2)
		want.SetMaxLines(2)
		verify()
		got.SetMaxBytes(scrollbackRowBytes + scrollbackCellBytes)
		want.SetMaxBytes(scrollbackRowBytes + scrollbackCellBytes)
		verify()
		got.Push(newRow)
		want.Push(newRow)
		verify()
		got.SetMaxBytes(0)
		want.SetMaxBytes(0)
		verify()
		got.Clear()
		want.Clear()
		verify()
	}
}
