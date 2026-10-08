package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kazuph/goterrace/internal/layout"
)

// thumbGeom/offFromThumb round-trip: a thumb dragged to a position maps
// back to the offset the renderer shows, clamped at both ends.
func TestThumbGeomRoundTrip(t *testing.T) {
	const h, scrollN = 20, 100
	// Each thumb row quantizes to scrollN/track lines; allow one row.
	q := scrollN/(h-1) + 2
	for _, off := range []int{0, 25, 50, 100} {
		_, top := thumbGeom(h, scrollN, off)
		got := offFromThumb(h, scrollN, top)
		if diff := got - off; diff < -q || diff > q {
			t.Fatalf("off %d → top %d → %d", off, top, got)
		}
	}
	// Off the ends clamps instead of wrapping.
	if got := offFromThumb(h, scrollN, -5); got != scrollN {
		t.Fatalf("top beyond end → %d, want %d", got, scrollN)
	}
	if got := offFromThumb(h, scrollN, h+5); got != 0 {
		t.Fatalf("top below start → %d, want 0", got)
	}
	// No scrollback: thumb fills the track.
	th, top := thumbGeom(h, 0, 0)
	if th != h || top != 0 {
		t.Fatalf("empty track th=%d top=%d", th, top)
	}
}

// A grab inside the thumb preserves the press offset through the drag
// (SPEC G6): sb.grab equals press-row minus thumb-top.
func TestScrollbarGrabOffset(t *testing.T) {
	const h, scrollN = 20, 100
	// offset 50 → thumb top at track-middle.
	th, top := thumbGeom(h, scrollN, 50)
	press := top + th/2
	grab := press - top
	newTop := press + 4 - grab // drag 4 rows down
	got := offFromThumb(h, scrollN, newTop)
	if got >= 50 {
		t.Fatalf("dragging thumb down should reduce offset toward 0, got %d", got)
	}
	if diff := 50 - got; diff < 15 || diff > 30 {
		t.Fatalf("4-row drag moved offset by %d lines (want ~24)", diff)
	}
}

// SPEC G6 SGR 1016: pixel mode converts a cell offset to that cell's
// center pixel; cell mode leaves coordinates untouched.
func TestPaneMouseCoordPixel(t *testing.T) {
	inner := layout.Rect{X: 10, Y: 5, W: 40, H: 20}
	m := uv.Mouse{X: 13, Y: 9, Button: uv.MouseLeft}
	// Cell mode: simple offset.
	got := paneMouseCoord(inner, false, 16, 32, m)
	if got.X != 3 || got.Y != 4 {
		t.Fatalf("cell coord = %v", got)
	}
	// Pixel mode with a 16x32 host cell: center of cell (3,4).
	got = paneMouseCoord(inner, true, 16, 32, m)
	if got.X != 56 || got.Y != 144 {
		t.Fatalf("pixel coord = %v, want 56,144", got)
	}
	// Unknown cell size falls back to 1x1 (coords stay at cell index).
	got = paneMouseCoord(inner, true, 0, 0, m)
	if got.X != 3 || got.Y != 4 {
		t.Fatalf("unknown cell size coord = %v", got)
	}
}
