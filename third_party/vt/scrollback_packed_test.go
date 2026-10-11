package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"image/color"
	"reflect"
	"testing"
	"unsafe"
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

func TestStyledPackedRowsPreserveAppearanceGraphemesAndLifecycle(t *testing.T) {
	row := make(uv.Line, 60)
	red := uv.Style{Fg: color.RGBA{R: 255, A: 255}, Bg: color.RGBA{B: 40, A: 255}, Underline: uv.UnderlineCurly, UnderlineColor: color.RGBA{G: 255, A: 255}, Attrs: uv.AttrBold | uv.AttrItalic}
	link := uv.Link{URL: "https://example.com/色", Params: "id=1"}
	for x := range row {
		row[x] = uv.Cell{Content: "x", Width: 1}
		if x < 30 {
			row[x].Style = red
			row[x].Link = link
		}
	}
	row[5].Content = "界"
	row[5].Width = 2
	row[6] = uv.Cell{}
	row[40].Content = "e\u0301"
	row[45].Content = "👩‍💻"
	row[45].Width = 2
	row[46] = uv.Cell{}
	cost := scrollbackRowBytes + len(row)*scrollbackCellBytes
	sb := NewScrollback(1)
	sb.SetMaxBytes(3 * cost)
	for i := 0; i < 12; i++ {
		sb.Push(row)
	}
	if sb.Len() != 3 || sb.Bytes() != 3*cost {
		t.Fatal("rows/budget", sb.Len(), sb.Bytes())
	}
	if sb.styled[sb.start] == nil {
		t.Fatal("styled row not packed")
	}
	for _, got := range sb.Lines() {
		if !reflect.DeepEqual(got, row) {
			t.Fatal("appearance or grapheme changed")
		}
	}
	for x := range row {
		if c := sb.CellAt(x, 0); c == nil || !reflect.DeepEqual(*c, row[x]) {
			t.Fatal("cell", x, c, row[x])
		}
	}
	if sb.CellAt(-1, 0) != nil || sb.CellAt(len(row), 0) != nil {
		t.Fatal("bounds")
	}
	sb.SetMaxBytes(2 * cost)
	sb.SetMaxLines(1)
	sb.SetMaxLines(5)
	if sb.Len() != 1 || sb.Bytes() != cost || !reflect.DeepEqual(sb.Line(0), row) {
		t.Fatal("config/reorder")
	}
	if !reflect.DeepEqual(sb.popLast(), row) || sb.Bytes() != 0 {
		t.Fatal("restore")
	}
	sb.Push(row)
	row[0].Content = "changed"
	if sb.Line(0)[0].Content != "x" {
		t.Fatal("source alias")
	}
	sb.Clear()
	for _, r := range sb.styled {
		if r != nil {
			t.Fatal("clear retains styled data")
		}
	}
}

func TestSavedAppearanceLayout(t *testing.T) {
	if appearanceBytes != 11*unsafe.Sizeof(uintptr(0)) || unsafe.Offsetof(uv.Cell{}.Link)-unsafe.Offsetof(uv.Cell{}.Style) != unsafe.Sizeof(uv.Style{}) || unsafe.Offsetof(savedStyle{}.link)-unsafe.Offsetof(savedStyle{}.style) != unsafe.Sizeof(uv.Style{}) || unsafe.Offsetof(savedStyle{}.end)-unsafe.Offsetof(savedStyle{}.style) != appearanceBytes {
		t.Fatal("appearance fields must be adjacent for read-only identity comparison")
	}
}

func TestStyledRowsKeepExpandedByteBudgetAndDenseCells(t *testing.T) {
	row := make(uv.Line, 30)
	for x := range row {
		row[x] = uv.Cell{Content: "x", Width: 1, Style: uv.Style{Fg: color.RGBA{R: 255, A: 255}, Attrs: uv.AttrBold}}
	}
	sb := NewScrollback(1000)
	sb.SetMaxBytes(10000000)
	for i := 0; i < 5000; i++ {
		sb.Push(row)
	}
	cost := scrollbackRowBytes + len(row)*scrollbackCellBytes
	if sb.Len() != 10000000/cost || sb.Bytes() != sb.Len()*cost {
		t.Fatal("expanded budget", sb.Len(), sb.Bytes())
	}
	for x := range row {
		row[x].Style.Fg = color.RGBA{R: uint8(x), A: 255}
	}
	sb.Clear()
	sb.Push(row)
	if sb.styled[sb.start] != nil || !reflect.DeepEqual(sb.Line(0), row) {
		t.Fatal("dense styles must retain cells losslessly without a larger packed payload")
	}
}
