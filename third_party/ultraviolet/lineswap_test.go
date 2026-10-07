package uv

import (
	"fmt"
	"testing"
)

// refInsertLineArea is the pre-optimization cell-by-cell implementation of
// [Buffer.InsertLineArea]. It is kept here as the reference the fast path is
// verified against.
func refInsertLineArea(b *Buffer, y, n int, c *Cell, area Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= b.Height() {
		return
	}

	if y+n > area.Max.Y {
		n = area.Max.Y - y
	}

	for i := area.Max.Y - 1; i >= y+n; i-- {
		for x := area.Min.X; x < area.Max.X; x++ {
			b.Lines[i][x] = b.Lines[i-n][x]
		}
	}

	for i := y; i < y+n; i++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			b.SetCell(x, i, c)
		}
	}
}

// refDeleteLineArea is the pre-optimization cell-by-cell implementation of
// [Buffer.DeleteLineArea].
func refDeleteLineArea(b *Buffer, y, n int, c *Cell, area Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= b.Height() {
		return
	}

	if n > area.Max.Y-y {
		n = area.Max.Y - y
	}

	for dst := y; dst < area.Max.Y-n; dst++ {
		src := dst + n
		for x := area.Min.X; x < area.Max.X; x++ {
			b.Lines[dst][x] = b.Lines[src][x]
		}
	}

	for i := area.Max.Y - n; i < area.Max.Y; i++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			b.SetCell(x, i, c)
		}
	}
}

// patternedBuffer fills a buffer with a mix of plain, styled, wide, and blank
// cells so that the scroll paths are exercised against realistic content.
func patternedBuffer(w, h int) *Buffer {
	b := NewBuffer(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := Cell{Content: string(rune('a' + (x+y)%26)), Width: 1}
			if (x+y)%7 == 0 {
				c.Style.Attrs = AttrBold
			}
			b.Lines[y][x] = c
		}
	}
	// Sprinkle wide cells and their zero-width placeholders.
	for y := 1; y < h-1; y += 4 {
		if w > 6 {
			b.Lines[y][3] = Cell{Content: "世", Width: 2}
			b.Lines[y][4] = Cell{}
		}
	}
	return b
}

func cellsEqual(a, b *Buffer) bool {
	if a.Width() != b.Width() || a.Height() != b.Height() {
		return false
	}
	for y := 0; y < a.Height(); y++ {
		for x := 0; x < a.Width(); x++ {
			if a.Lines[y][x] != b.Lines[y][x] {
				return false
			}
		}
	}
	return true
}

func diffCell(a, b *Buffer) string {
	for y := 0; y < a.Height(); y++ {
		for x := 0; x < a.Width(); x++ {
			if a.Lines[y][x] != b.Lines[y][x] {
				return fmt.Sprintf("cell (%d,%d): got %+v want %+v",
					x, y, a.Lines[y][x], b.Lines[y][x])
			}
		}
	}
	return ""
}

type lineAreaCase struct {
	name      string
	y, n      int
	minX      int
	minY      int
	maxY      int // maxX is always the buffer width in this table
	shrinkMax int // subtract from maxX to exercise narrow areas
}

var lineAreaCases = []lineAreaCase{
	{"full scroll one line", 0, 1, 0, 0, 45, 0},
	{"full scroll three lines", 0, 3, 0, 0, 45, 0},
	{"decstbm region scroll", 5, 1, 0, 5, 40, 0},
	{"decstbm scroll several", 5, 4, 0, 5, 40, 0},
	{"decstbm mid region", 10, 2, 0, 5, 40, 0},
	{"scroll larger than region", 5, 100, 0, 5, 40, 0},
	{"left right margins", 0, 1, 10, 0, 45, 10},
	{"left margin only", 0, 1, 10, 0, 45, 0},
	{"right margin only", 0, 2, 0, 0, 45, 10},
	{"margins and decstbm", 6, 2, 10, 5, 40, 10},
	{"bottom of region", 39, 2, 0, 5, 40, 0},
	{"n covers whole region", 5, 35, 0, 5, 40, 0},
}

func TestInsertLineAreaLineSwap(t *testing.T) {
	const w, h = 160, 45
	for _, tc := range lineAreaCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, fill := range []*Cell{nil, {Content: " ", Width: 1, Style: Style{Attrs: AttrReverse}}, {Content: "世", Width: 2}, {Content: "", Width: 0}} {
				area := Rect(tc.minX, tc.minY, w-tc.shrinkMax-tc.minX, tc.maxY-tc.minY)
				got := patternedBuffer(w, h)
				want := patternedBuffer(w, h)
				got.InsertLineArea(tc.y, tc.n, fill, area)
				refInsertLineArea(want, tc.y, tc.n, fill, area)
				if !cellsEqual(got, want) {
					t.Fatalf("%s", diffCell(got, want))
				}
			}
		})
	}
}

func TestDeleteLineAreaLineSwap(t *testing.T) {
	const w, h = 160, 45
	for _, tc := range lineAreaCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, fill := range []*Cell{nil, {Content: " ", Width: 1, Style: Style{Attrs: AttrReverse}}, {Content: "世", Width: 2}, {Content: "", Width: 0}} {
				area := Rect(tc.minX, tc.minY, w-tc.shrinkMax-tc.minX, tc.maxY-tc.minY)
				got := patternedBuffer(w, h)
				want := patternedBuffer(w, h)
				got.DeleteLineArea(tc.y, tc.n, fill, area)
				refDeleteLineArea(want, tc.y, tc.n, fill, area)
				if !cellsEqual(got, want) {
					t.Fatalf("%s", diffCell(got, want))
				}
			}
		})
	}
}
