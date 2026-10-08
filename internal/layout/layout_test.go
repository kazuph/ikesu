package layout

import (
	"testing"
)

func area() Rect { return Rect{X: 0, Y: 0, W: 80, H: 24} }

func paneMap(l *Layout, a Rect) map[int]Rect {
	m := map[int]Rect{}
	for _, p := range l.Panes(a) {
		m[p.ID] = p.Rect
	}
	return m
}

// Splitting then panes is a partition; a second split divides the right
// child top/bottom.
func TestSplitPartitions(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2)
	l.SplitFocused(Horizontal, 3)

	got := paneMap(l, area())
	if got[1] != (Rect{0, 0, 40, 24}) {
		t.Fatalf("pane1 = %+v", got[1])
	}
	if got[2] != (Rect{40, 0, 40, 12}) {
		t.Fatalf("pane2 = %+v", got[2])
	}
	if got[3] != (Rect{40, 12, 40, 12}) {
		t.Fatalf("pane3 = %+v", got[3])
	}
	if l.Focused() != 3 {
		t.Fatalf("focus = %d, want 3", l.Focused())
	}
}

// Closing removes the rect and the sibling subtree expands into the space.
func TestCloseExpandsSibling(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2)
	l.SplitFocused(Horizontal, 3)

	if !l.Close(2) {
		t.Fatal("Close(2) failed")
	}
	got := paneMap(l, area())
	if _, ok := got[2]; ok {
		t.Fatal("pane2 still present")
	}
	if got[3] != (Rect{40, 0, 40, 24}) {
		t.Fatalf("pane3 = %+v, want sibling-expanded", got[3])
	}
	if len(got) != 2 {
		t.Fatalf("panes = %d, want 2", len(got))
	}
	if l.Focused() != 3 {
		t.Fatalf("focus = %d, want sibling first leaf 3", l.Focused())
	}

	// Last pane cannot be closed.
	l2 := New(1)
	if l2.Close(1) {
		t.Fatal("closing the last pane must fail")
	}
}

// FocusDir picks the pane that shares the edge, overlaps orthogonally and is
// nearest to the focused pane's centre.
func TestFocusDir(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2)   // 1 | 2
	l.SplitFocused(Horizontal, 3) // 2 over 3
	l.Focus(1)

	l.FocusDir(NavRight, area())
	if l.Focused() != 2 {
		t.Fatalf("right of 1 = %d, want 2 (nearest centre, ties to top)", l.Focused())
	}
	l.FocusDir(NavDown, area())
	if l.Focused() != 3 {
		t.Fatalf("down of 2 = %d, want 3", l.Focused())
	}
	l.FocusDir(NavLeft, area())
	if l.Focused() != 1 {
		t.Fatalf("left of 3 = %d, want 1", l.Focused())
	}
	l.FocusDir(NavUp, area())
	if l.Focused() != 1 {
		t.Fatalf("up of 1 should stay, got %d", l.Focused())
	}
}

// Cycle walks the creation order both directions and wraps.
func TestCycle(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2)
	l.SplitFocused(Horizontal, 3)
	if got := l.PaneIDs(); len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("order = %v", got)
	}
	if l.Focused() != 3 {
		t.Fatalf("focus = %d", l.Focused())
	}
	l.Cycle(true) // 3 -> 1
	if l.Focused() != 1 {
		t.Fatalf("cycle next from 3 = %d, want wrap to 1", l.Focused())
	}
	l.Cycle(true)
	if l.Focused() != 2 {
		t.Fatalf("cycle next = %d, want 2", l.Focused())
	}
	l.Cycle(false)
	if l.Focused() != 1 {
		t.Fatalf("cycle prev = %d, want 1", l.Focused())
	}
	l.Cycle(false) // 1 -> 3
	if l.Focused() != 3 {
		t.Fatalf("cycle prev from 1 = %d, want wrap to 3", l.Focused())
	}
}

// Odd widths give the remainder to the second child.
func TestOddWidthRoundsUpFirst(t *testing.T) {
	// SPEC G1: cells = axis_len × ratio in f32; an exact .5 rounds up to
	// the first side and the second owns the remainder.
	l := New(1)
	l.SplitFocused(Vertical, 2)
	got := paneMap(l, Rect{0, 0, 81, 24})
	if got[1].W != 41 || got[2].W != 40 {
		t.Fatalf("widths = %d/%d, want 41/40", got[1].W, got[2].W)
	}
}

// Split borders are listed, hit-tested on both divider cells, and draggable.
func TestSplitBorders(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2)
	l.SplitFocused(Horizontal, 3)

	splits := l.Splits(area())
	if len(splits) != 2 {
		t.Fatalf("splits = %d, want 2", len(splits))
	}
	root, inner := splits[0], splits[1]
	if root.Direction != Vertical || root.Pos != 40 {
		t.Fatalf("root border = %+v", root)
	}
	if inner.Direction != Horizontal || inner.Pos != 12 {
		t.Fatalf("inner border = %+v", inner)
	}
	if len(inner.Path) != 1 || inner.Path[0] != true {
		t.Fatalf("inner path = %v, want [second]", inner.Path)
	}

	// Hit test accepts the divider columns on both panes' borders.
	if b := l.SplitBorderAt(40, 5, area()); b == nil || b.Pos != 40 {
		t.Fatalf("hit (40,5) = %+v", b)
	}
	if b := l.SplitBorderAt(39, 5, area()); b == nil || b.Pos != 40 {
		t.Fatalf("hit (39,5) = %+v", b)
	}
	if b := l.SplitBorderAt(41, 5, area()); b != nil {
		t.Fatalf("hit (41,5) should be pane content, got %+v", b)
	}
	if b := l.SplitBorderAt(60, 11, area()); b == nil || b.Pos != 12 {
		t.Fatalf("hit (60,11) = %+v", b)
	}

	// Dragging the root divider to x=30 sets ratio 30/80 = 0.375.
	if b := l.SplitBorderAt(40, 5, area()); b == nil {
		t.Fatal("no border at (40,5)")
	} else {
		r := float64(30-b.Area.X) / float64(b.Area.W)
		if !l.SetRatioAt(b.Path, r) {
			t.Fatal("SetRatioAt failed")
		}
	}
	got := paneMap(l, area())
	if got[1].W != 30 || got[2].W != 50 {
		t.Fatalf("after drag widths = %d/%d", got[1].W, got[2].W)
	}
}

// Ratio stays clamped to 0.1..0.9 while dragging.
func TestSetRatioClamp(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2)
	splits := l.Splits(area())
	if !l.SetRatioAt(splits[0].Path, 0.95) {
		t.Fatal("SetRatioAt failed")
	}
	got := paneMap(l, area())
	if got[1].W != 72 { // int(80 * 0.9)
		t.Fatalf("clamped width = %d, want 72", got[1].W)
	}
}
