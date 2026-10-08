package layout

import (
	"fmt"
	"testing"
)

// tree renders the layout tree like the observed herdr exports:
// (r0.50 a (d0.50 b c)) — direction initial, ratio, children.
func tree(l *Layout) string {
	var walk func(n *Node) string
	walk = func(n *Node) string {
		if n.Leaf >= 0 {
			return fmt.Sprintf("p%d", n.Leaf)
		}
		d := "r"
		if n.Direction == Horizontal {
			d = "d"
		}
		return fmt.Sprintf("(%s%.2f %s %s)", d, n.Ratio, walk(n.First), walk(n.Second))
	}
	return walk(l.root)
}

func build(ids []int, splits func(l *Layout)) *Layout {
	l := New(ids[0])
	for _, id := range ids[1:] {
		l.SplitFocused(Vertical, id)
	}
	if splits != nil {
		splits(l)
	}
	return l
}

// Implicit splits hit the traversal-last leaf and append to logical order
// (SPEC G1 + observed: pane.split without target splits the last leaf).
func TestSplitImplicitTargetsLastLeaf(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2) // [1|2], focused 2
	l.Focus(1)
	l.SplitFocused(Horizontal, 3) // [1/3 | 2], focused 3
	if got := l.LastLeaf(); got != 2 {
		t.Fatalf("LastLeaf = %d, want 2", got)
	}
	// focus is on 3; implicit split must still hit leaf 2.
	if !l.SplitImplicit(Vertical, 4, 0.5, false) {
		t.Fatal("SplitImplicit failed")
	}
	want := "(r0.50 (d0.50 p1 p3) (r0.50 p2 p4))"
	if got := tree(l); got != want {
		t.Fatalf("tree = %s, want %s", got, want)
	}
	// Logical order appends the new pane at the tail.
	ord := l.Order()
	if ord[len(ord)-1] != 4 {
		t.Fatalf("order tail = %d, want 4", ord[len(ord)-1])
	}
}

// Neighbor follows screen geometry, not tree order.
func TestNeighborAndEdges(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2)   // 1 left, 2 right
	l.SplitFocused(Horizontal, 3) // 2 top-right, 3 bottom-right
	a := Rect{X: 10, Y: 1, W: 54, H: 22}
	if got := l.Neighbor(1, NavRight, a); got != 2 && got != 3 {
		t.Fatalf("right of 1 = %d", got)
	}
	if got := l.Neighbor(3, NavUp, a); got != 2 {
		t.Fatalf("up of 3 = %d, want 2", got)
	}
	if got := l.Neighbor(1, NavLeft, a); got != -1 {
		t.Fatalf("left of 1 = %d, want none", got)
	}
	lt, rt, up, dn := l.Edges(1, a)
	if !(lt && !rt && up && dn) {
		t.Fatalf("edges(1) = %v %v %v %v, want l,u,d", lt, rt, up, dn)
	}
	lt, rt, up, dn = l.Edges(3, a)
	if lt || !rt || up || !dn {
		t.Fatalf("edges(3) = %v %v %v %v, want r,d", lt, rt, up, dn)
	}
}

// SwapLeaves exchanges slots; focus stays on the pane id.
func TestSwapLeaves(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2)
	l.SplitFocused(Horizontal, 3)
	l.Focus(1)
	if !l.SwapLeaves(1, 3) {
		t.Fatal("swap failed")
	}
	if got := tree(l); got != "(r0.50 p3 (d0.50 p2 p1))" {
		t.Fatalf("tree = %s", got)
	}
	if l.Focused() != 1 {
		t.Fatalf("focus = %d, want 1 (focus follows the pane)", l.Focused())
	}
	if l.SwapLeaves(1, 99) {
		t.Fatal("swap with missing pane must fail")
	}
}

// Equalize gives each leaf an equal area share: ratio = leaves(first)/total.
func TestEqualizeLeafProportional(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2)
	l.SplitFocused(Horizontal, 3) // [1 | (2/3)]
	l.SetRatioAt(nil, 0.7)
	l.Equalize()
	got := tree(l)
	want := "(r0.33 p1 (d0.50 p2 p3))"
	if got != want {
		t.Fatalf("equalized = %s, want %s", got, want)
	}
}

// Rotate cycles which subtree holds the extra leaf slots.
func TestRotate(t *testing.T) {
	l := New(1)
	for i := 2; i <= 4; i++ {
		l.SplitFocused(Vertical, i)
	}
	before := tree(l)
	l.Rotate(true)
	rot1 := tree(l)
	l.Rotate(false)
	if tree(l) != before {
		t.Fatalf("rotate back = %s, want %s", tree(l), before)
	}
	if rot1 == before {
		t.Fatal("rotate produced no change")
	}
}

// MoveToSide pulls a non-root-side pane out to the named root side at 0.5.
func TestMoveToSide(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2)
	l.SplitFocused(Horizontal, 3) // [1 | (2/3)] — 3 is already right-side
	if !l.MoveToSide(3, NavLeft) {
		t.Fatal("move_left failed")
	}
	// Observed: the moved pane takes share 1/N (0.33 for 3 panes).
	if got := tree(l); got != "(r0.33 p3 (r0.50 p1 p2))" {
		t.Fatalf("tree = %s, want (r0.33 p3 (r0.50 p1 p2))", got)
	}
	if l.Focused() != 3 {
		t.Fatalf("focus = %d, want moved pane focused", l.Focused())
	}
}

// ResizeDir nudges the nearest same-axis split; clamped edges report no
// change.
func TestResizeDir(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2)
	l.SplitFocused(Horizontal, 3) // [1 | (2/3)]
	if !l.ResizeDir(3, NavDown, 0) {
		t.Fatal("resize down failed")
	}
	if got := tree(l); got != "(r0.50 p1 (d0.55 p2 p3))" {
		t.Fatalf("tree = %s, want inner split 0.55", got)
	}
	if !l.ResizeDir(1, NavRight, 0) {
		t.Fatal("resize right failed")
	}
	if got := tree(l); got != "(r0.55 p1 (d0.55 p2 p3))" {
		t.Fatalf("tree = %s, want root 0.55", got)
	}
	// Clamped at 0.9: no change.
	l.SetRatioAt(nil, 0.9)
	if l.ResizeDir(1, NavRight, 0) {
		t.Fatal("resize past clamp must fail")
	}
	// Vertical resize on pane with no vertical-axis ancestor? pane 1's
	// ancestors are the root (vertical) → no horizontal split → up fails.
	if l.ResizeDir(1, NavUp, 0) {
		t.Fatal("resize up on pane without horizontal ancestor must fail")
	}
}

// CyclePresets for N=2 toggles the axis at uniform ratio.
func TestCycleTwoPane(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2)
	l.CyclePresets()
	if got := tree(l); got != "(d0.50 p1 p2)" {
		t.Fatalf("cycle1 = %s", got)
	}
	l.CyclePresets()
	if got := tree(l); got != "(r0.50 p1 p2)" {
		t.Fatalf("cycle2 = %s", got)
	}
	// Non-uniform split cycles to the other axis (observed: r0.70 → d0.50).
	l.SetRatioAt(nil, 0.7)
	l.CyclePresets()
	if got := tree(l); got != "(d0.50 p1 p2)" {
		t.Fatalf("cycle non-uniform = %s, want (d0.50 p1 p2)", got)
	}
}

// CyclePresets for N=4 follows the observed preset sequence:
// column, grid, left-main, right-main, top-main, bottom-main, row.
func TestCyclePresetsFour(t *testing.T) {
	l := New(1)
	for i := 2; i <= 4; i++ {
		l.SplitFocused(Vertical, i)
	}
	want := []string{
		"(d0.25 p1 (d0.33 p2 (d0.50 p3 p4)))", // column
		"(d0.50 (r0.50 p1 p2) (r0.50 p3 p4))", // grid
		"(r0.50 p1 (d0.50 (r0.50 p2 p3) p4))", // left-main
		"(r0.50 (d0.50 (r0.50 p2 p3) p4) p1)", // right-main
		"(d0.50 p1 (r0.33 p2 (r0.50 p3 p4)))", // top-main
		"(d0.50 (r0.33 p2 (r0.50 p3 p4)) p1)", // bottom-main
		"(r0.25 p1 (r0.33 p2 (r0.50 p3 p4)))", // row
		"(d0.25 p1 (d0.33 p2 (d0.50 p3 p4)))", // wraps to column
	}
	for i, w := range want {
		l.CyclePresets()
		if got := tree(l); got != w {
			t.Fatalf("cycle %d = %s, want %s", i, got, w)
		}
	}
	if l.Focused() == 0 {
		t.Fatal("focus lost")
	}
	ord := l.Order()
	if len(ord) != 4 || ord[0] != 1 || ord[3] != 4 {
		t.Fatalf("logical order changed: %v", ord)
	}
}

// RemoveLeaf drops a leaf without spawning effects; the sibling expands.
func TestRemoveLeaf(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2)
	l.SplitFocused(Horizontal, 3)
	if !l.RemoveLeaf(2) {
		t.Fatal("RemoveLeaf failed")
	}
	if got := tree(l); got != "(r0.50 p1 p3)" {
		t.Fatalf("tree = %s", got)
	}
	if l.RemoveLeaf(99) {
		t.Fatal("RemoveLeaf with missing id must fail")
	}
	if !l.RemoveLeaf(3) {
		t.Fatal("RemoveLeaf(3) failed")
	}
	// The last pane can be removed for pane.move; the tab is then empty.
	if !l.RemoveLeaf(1) {
		t.Fatal("RemoveLeaf of last pane must succeed (tab dissolves)")
	}
	if l.PaneCount() != 0 {
		t.Fatalf("PaneCount = %d, want 0", l.PaneCount())
	}
}

// Fingerprint changes on structure and focus changes.
func TestFingerprint(t *testing.T) {
	l := New(1)
	l.SplitFocused(Vertical, 2)
	fp1 := l.Fingerprint()
	l.Focus(1)
	if l.Fingerprint() == fp1 {
		t.Fatal("fingerprint unchanged on focus move")
	}
	l.Focus(2)
	l.SetRatioAt(nil, 0.6)
	if l.Fingerprint() == fp1 {
		t.Fatal("fingerprint unchanged on ratio change")
	}
}
