package vt

import (
	"reflect"
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
