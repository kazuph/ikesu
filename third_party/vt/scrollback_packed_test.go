package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"image/color"
	"reflect"
	"testing"
)

func TestPackedByteRowsPreserveCellsAndReuse(t *testing.T) {
	plain := uv.Line{{Content: "a", Width: 1}, {Content: " ", Width: 1}, {Content: "z", Width: 1}}
	styled := uv.Line{{Content: "色", Width: 2, Style: uv.Style{Fg: color.RGBA{R: 255, A: 255}, Attrs: uv.AttrBold}, Link: uv.Link{URL: "https://example.com", Params: "id=x"}}, {}}
	combining := uv.Line{{Content: "e\u0301", Width: 1}, {Content: "😀", Width: 2}, {}}
	rows := []uv.Line{plain, styled, combining, plain, nil, styled, plain}
	sb := NewScrollback(1)
	sb.SetMaxBytes(2 * (scrollbackRowBytes + len(plain)*scrollbackCellBytes))
	for _, src := range rows {
		sb.Push(src)
		if sb.Bytes() > sb.maxBytes {
			t.Fatal("exceeded byte budget")
		}
		if got := sb.Line(sb.Len() - 1); !reflect.DeepEqual([]uv.Cell(got), []uv.Cell(src)) && !(len(got) == 0 && len(src) == 0) {
			t.Fatalf("got %v want %v", got, src)
		}
		for x := range src {
			if got := sb.CellAt(x, sb.Len()-1); got == nil || !got.Equal(&src[x]) {
				t.Fatalf("cell %d: %v want %v", x, got, src[x])
			}
		}
	}
	if sb.packed[sb.wrap(sb.start+sb.count-1)] == nil {
		t.Fatal("plain row not packed")
	}
	plain[0].Content = "changed"
	if sb.Line(sb.Len() - 1)[0].Content != "a" {
		t.Fatal("source aliases packed text")
	}
	// Configuration changes must use the expanded byte count and preserve ring order.
	before := sb.Bytes()
	sb.SetMaxBytes(sb.maxBytes)
	if sb.Bytes() != before {
		t.Fatal("budget changed after recomputation")
	}
	sb.SetMaxLines(1)
	if sb.Len() != 1 || sb.Bytes() != scrollbackRowBytes+3*scrollbackCellBytes {
		t.Fatal("shrink", sb.Len(), sb.Bytes())
	}
	sb.SetMaxLines(5)
	if got := sb.Lines(); len(got) != 1 || got[0][2].Content != "z" {
		t.Fatal("grow", got)
	}
	if got := sb.popLast(); got[0].Content != "a" || sb.Bytes() != 0 {
		t.Fatal("pull", got, sb.Bytes())
	}
	sb.Push(styled)
	sb.Push(uv.Line{{Content: "x", Width: 1}})
	sb.Clear()
	if sb.Len() != 0 || sb.Bytes() != 0 {
		t.Fatal("clear")
	}
	for i := range sb.packed {
		if sb.packed[i] != nil || sb.lines[i] != nil {
			t.Fatal("clear retains storage")
		}
	}
	sb.SetMaxBytes(0)
	sb.Push(uv.Line{{Content: "x", Width: 1}})
	if sb.Len() != 0 {
		t.Fatal("zero budget")
	}
}

func TestPackedRowsKeepExpandedByteBudgetAndLineCount(t *testing.T) {
	row := make(uv.Line, 160)
	for x := range row {
		row[x] = uv.Cell{Content: "x", Width: 1}
	}
	cost := scrollbackRowBytes + len(row)*scrollbackCellBytes
	sb := NewScrollback(1)
	sb.SetMaxBytes(10000000)
	want := 10000000 / cost
	for i := 0; i < 2*want; i++ {
		sb.Push(row)
	}
	if sb.Len() != want || sb.Bytes() != want*cost {
		t.Fatal("expanded budget", sb.Len(), sb.Bytes(), want, cost)
	}
	physical := 0
	for _, p := range sb.packed {
		physical += cap(p)
	}
	if physical != want*len(row) || physical >= sb.Bytes() {
		t.Fatal("packed storage", physical, sb.Bytes())
	}
	got := sb.Lines()
	for i := range got {
		if !reflect.DeepEqual(got[i], row) {
			t.Fatal("row", i)
		}
	}
	// Returned rows own their decoded cells, including after another row is read.
	got[0][0].Content = "changed"
	if sb.Line(0)[0].Content != "x" {
		t.Fatal("decoded row aliases saved storage")
	}
}
