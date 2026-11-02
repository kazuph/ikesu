package vt

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestStyledGlyphEncodingBoundaries(t *testing.T) {
	if unsafe.Sizeof(savedGlyph(0)) != 4 {
		t.Fatal("glyph record is not four bytes")
	}
	for _, width := range []int{0, 1, 2, 3} {
		for _, end := range []int{0, 1, glyphEndMask} {
			for _, zero := range []bool{false, true} {
				glyph := makeSavedGlyph(end, width, zero)
				if glyph.end() != end || glyph.width() != width || glyph.zero() != zero {
					t.Fatalf("roundtrip end=%d width=%d zero=%v", end, width, zero)
				}
			}
		}
	}
	for _, tc := range []struct {
		offset, n, width int
		want             bool
	}{
		{0, 0, 0, true}, {glyphEndMask, 0, 3, true}, {glyphEndMask - 1, 1, 2, true},
		{glyphEndMask, 1, 1, false}, {glyphEndMask + 1, 0, 1, false}, {-1, 0, 1, false},
		{0, -1, 1, false}, {0, 0, -1, false}, {0, 0, 4, false},
	} {
		if got := canSaveGlyph(tc.offset, tc.n, tc.width); got != tc.want {
			t.Fatalf("canSaveGlyph(%d,%d,%d)=%v", tc.offset, tc.n, tc.width, got)
		}
	}
}

func TestStyledGlyphUnrepresentableWidthRetainsExpandedCells(t *testing.T) {
	for _, width := range []int{-1, 4} {
		line := make(uv.Line, 8)
		for i := range line {
			line[i] = uv.Cell{Content: "x", Width: 1, Style: uv.Style{Fg: ansi.IndexedColor(2)}}
		}
		line[3].Width = width
		if got := packStyled(line, nil); got != nil {
			t.Fatalf("width %d unexpectedly packed", width)
		}
		sb := NewScrollback(1)
		sb.SetMaxBytes(scrollbackRowBytes + len(line)*scrollbackCellBytes)
		sb.Push(line)
		if sb.styled[sb.start] != nil || !reflect.DeepEqual(sb.Line(0), line) || sb.Bytes() != scrollbackRowBytes+len(line)*scrollbackCellBytes {
			t.Fatalf("width %d changed expanded row or charge", width)
		}
	}
}

// A row whose total text exceeds what a compact glyph can address must be
// rejected by packStyled and kept as expanded cells by Push, for fresh and
// reused storage alike. The text is shared, so the test allocates little.
func TestStyledHistoryRowBeyondTextLimitStaysExpanded(t *testing.T) {
	chunk := strings.Repeat("z", 64<<10)
	cells := (glyphEndMask + 1) / len(chunk) // total text is exactly 2^29 bytes
	row := make(uv.Line, cells)
	styles := []uv.Style{{Fg: historySliceColor{1}}, {Fg: historySliceColor{2}}}
	for x := range row {
		row[x] = uv.Cell{Content: chunk, Width: 1, Style: styles[x%2]}
	}
	if packStyled(row, nil) != nil {
		t.Fatal("fresh pack accepted text beyond the glyph offset limit")
	}
	small := make(uv.Line, cells)
	for x := range small {
		small[x] = uv.Cell{Content: "s", Width: 1, Style: styles[x%2]}
	}
	reuse := packStyled(small, nil)
	if reuse == nil {
		t.Fatal("small styled row was not packed")
	}
	if packStyled(row, reuse) != nil {
		t.Fatal("reused pack accepted text beyond the glyph offset limit")
	}

	sb := NewScrollback(4)
	sb.SetMaxBytes(4 * (scrollbackRowBytes + cells*scrollbackCellBytes))
	sb.Push(row)
	idx := sb.wrap(sb.start + sb.count - 1)
	if sb.Len() != 1 || sb.styled[idx] != nil || sb.Bytes() != scrollbackRowBytes+cells*scrollbackCellBytes {
		t.Fatalf("pushed row: len %d styled %v bytes %d", sb.Len(), sb.styled[idx] != nil, sb.Bytes())
	}
	got := sb.Line(0)
	if len(got) != cells || got[0].Content != chunk || !reflect.DeepEqual(got[cells-1].Style, styles[(cells-1)%2]) {
		t.Fatal("expanded row content changed")
	}
}
