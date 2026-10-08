// Package layout implements the BSP tree used to tile panes inside a
// workspace. It is pure: no PTY, file, or OS access.
//
// Direction naming follows docs/spec/PLAN.md: Vertical arranges panes side
// by side (the divider is a vertical line, herdr's "split_vertical"),
// Horizontal stacks panes top and bottom.
package layout

import "math"

// Direction is the axis a split divides.
type Direction int

const (
	// Vertical places the two children left/right (divides the width).
	Vertical Direction = iota
	// Horizontal places the two children top/bottom (divides the height).
	Horizontal
)

// Rect is a rectangle in terminal cells. X/Y is the top-left corner, W/H the
// size. A Rect includes any border drawn inside it.
type Rect struct {
	X, Y, W, H int
}

// Contains reports whether the point (x, y) is inside the rect.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// NavDir is a cardinal direction used by FocusDir.
type NavDir int

const (
	NavLeft NavDir = iota
	NavRight
	NavUp
	NavDown
)

// Node is one BSP tree node: either a leaf holding a pane id, or a split.
// Ratio is the fraction of the area assigned to the first child (0.1..0.9).
type Node struct {
	Leaf      int // pane id when >= 0; -1 marks a split
	Direction Direction
	Ratio     float64
	First     *Node
	Second    *Node
}

// LeafNode builds a leaf node for pane id.
func LeafNode(id int) *Node { return &Node{Leaf: id} }

// SplitNode builds a split node.
func SplitNode(dir Direction, ratio float64, first, second *Node) *Node {
	return &Node{Leaf: -1, Direction: dir, Ratio: clampRatio(ratio), First: first, Second: second}
}

func clampRatio(r float64) float64 {
	if r < 0.1 {
		return 0.1
	}
	if r > 0.9 {
		return 0.9
	}
	return r
}

// Layout is the BSP tiling state of one workspace.
type Layout struct {
	root  *Node
	focus int
	order []int // pane ids in creation order
}

// New returns a layout holding a single pane.
func New(rootPane int) *Layout {
	return &Layout{root: LeafNode(rootPane), focus: rootPane, order: []int{rootPane}}
}

// Order returns pane ids in creation order.
func (l *Layout) Order() []int { return append([]int(nil), l.order...) }

// Restore rebuilds a Layout from persisted parts: an already-validated
// tree (positive, unique leaf ids), a focused pane id, and the creation
// order. Focus and order are sanitized against the tree's leaves, so a
// slightly stale snapshot still yields a usable layout.
func Restore(root *Node, focused int, order []int) *Layout {
	ids := collectIDs(root)
	seen := map[int]bool{}
	var ord []int
	for _, id := range order {
		if containsID(ids, id) && !seen[id] {
			ord = append(ord, id)
			seen[id] = true
		}
	}
	for _, id := range ids {
		if !seen[id] {
			ord = append(ord, id)
		}
	}
	if !containsID(ids, focused) {
		focused = firstLeaf(root)
	}
	return &Layout{root: root, focus: focused, order: ord}
}

// Focused returns the focused pane id.
func (l *Layout) Focused() int { return l.focus }

// PaneCount returns the number of leaf panes.
func (l *Layout) PaneCount() int { return countPanes(l.root) }

// PaneIDs returns live pane ids in creation order.
func (l *Layout) PaneIDs() []int {
	tree := collectIDs(l.root)
	out := make([]int, 0, len(tree))
	seen := map[int]bool{}
	for _, id := range l.order {
		if containsID(tree, id) && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	for _, id := range tree {
		if !seen[id] {
			out = append(out, id)
		}
	}
	return out
}

// SplitFocused splits the focused pane in direction dir at ratio 0.5, puts
// newPane as the second child and moves focus to it.
func (l *Layout) SplitFocused(dir Direction, newPane int) {
	l.SplitAt(l.focus, dir, newPane, 0.5, true)
}

// SplitAt splits an arbitrary pane in direction dir at ratio, puts newPane
// as the second child, and optionally moves focus to it. Returns false if
// pane does not exist.
func (l *Layout) SplitAt(pane int, dir Direction, newPane int, ratio float64, focus bool) bool {
	if !containsID(collectIDs(l.root), pane) {
		return false
	}
	l.root = splitAt(l.root, pane, dir, newPane, ratio)
	at := indexOf(l.order, pane)
	if at < 0 {
		l.order = append(l.order, newPane)
	} else {
		l.order = append(l.order[:at+1], append([]int{newPane}, l.order[at+1:]...)...)
	}
	if focus {
		l.focus = newPane
	}
	return true
}

// Close removes a leaf pane and promotes its sibling into the parent slot.
// It refuses to remove the last pane and returns false. When the closed pane
// held focus, focus moves to the first leaf of the sibling subtree.
func (l *Layout) Close(pane int) bool {
	if l.PaneCount() <= 1 {
		return false
	}
	newRoot, ok, _ := removePane(l.root, pane)
	if !ok || newRoot == nil {
		return false
	}
	l.root = newRoot
	// SPEC G1: focus moves to the next pane in logical order, or the
	// previous one when the closed pane was the tail.
	idx := indexOf(l.order, pane)
	l.order = removeID(l.order, pane)
	if l.focus == pane || !containsID(collectIDs(l.root), l.focus) {
		switch {
		case idx >= 0 && idx < len(l.order):
			l.focus = l.order[idx]
		case len(l.order) > 0:
			l.focus = l.order[len(l.order)-1]
		}
	}
	return true
}

// Focus makes pane the focused pane if it exists.
func (l *Layout) Focus(pane int) {
	if containsID(collectIDs(l.root), pane) {
		l.focus = pane
	}
}

// Cycle moves focus to the next (or previous) pane in creation order.
func (l *Layout) Cycle(next bool) {
	ids := l.PaneIDs()
	if len(ids) == 0 {
		return
	}
	at := indexOf(ids, l.focus)
	if at < 0 {
		l.focus = ids[0]
		return
	}
	if next {
		l.focus = ids[(at+1)%len(ids)]
	} else {
		l.focus = ids[(at-1+len(ids))%len(ids)]
	}
}

// Panes assigns an outer rect (including its one-cell border) to every pane.
// The fractional remainder goes to the second child.
func (l *Layout) Panes(area Rect) []PaneRect {
	var out []PaneRect
	collectPanes(l.root, area, l.focus, &out)
	return out
}

// PaneRect pairs a pane id with its outer rect.
type PaneRect struct {
	ID        int
	Rect      Rect
	IsFocused bool
}

// FocusDir moves focus to the neighbouring pane in direction dir: the
// candidate must share the edge with the focused pane and overlap on the
// orthogonal axis; among those the pane whose centre is closest to the
// focused pane's centre wins.
func (l *Layout) FocusDir(dir NavDir, area Rect) {
	panes := l.Panes(area)
	var fr Rect
	for _, p := range panes {
		if p.ID == l.focus {
			fr = p.Rect
		}
	}
	best := -1
	bestDist := 0.0
	fcx, fcy := center(fr)
	for _, p := range panes {
		if p.ID == l.focus {
			continue
		}
		r := p.Rect
		var ok bool
		switch dir {
		case NavLeft:
			ok = r.X+r.W == fr.X && rangesOverlap(r.Y, r.H, fr.Y, fr.H)
		case NavRight:
			ok = fr.X+fr.W == r.X && rangesOverlap(r.Y, r.H, fr.Y, fr.H)
		case NavUp:
			ok = r.Y+r.H == fr.Y && rangesOverlap(r.X, r.W, fr.X, fr.W)
		case NavDown:
			ok = fr.Y+fr.H == r.Y && rangesOverlap(r.X, r.W, fr.X, fr.W)
		}
		if !ok {
			continue
		}
		cx, cy := center(r)
		d := (cx-fcx)*(cx-fcx) + (cy-fcy)*(cy-fcy)
		if best < 0 || d < bestDist {
			best, bestDist = p.ID, d
		}
	}
	if best >= 0 {
		l.focus = best
	}
}

// SplitBorder describes the draggable divider produced by one split node.
type SplitBorder struct {
	// Pos is the divider coordinate: the x of the second child's left edge
	// for a Vertical split, the y of the second child's top edge for a
	// Horizontal split. Because every pane draws its own border, the
	// clickable divider is two cells wide/tall: Pos-1 and Pos.
	Pos       int
	Direction Direction
	Ratio     float64
	Area      Rect
	// Path locates the split node from the root: false = first, true = second.
	Path []bool
}

// Splits lists all split borders in the layout for the given area.
func (l *Layout) Splits(area Rect) []SplitBorder {
	var out []SplitBorder
	collectSplits(l.root, area, nil, &out)
	return out
}

// SplitBorderAt returns the split whose divider line covers (x, y).
// Each divider accepts both its cell columns/rows (the right/bottom border of
// the first child and the left/top border of the second child).
func (l *Layout) SplitBorderAt(x, y int, area Rect) *SplitBorder {
	splits := l.Splits(area)
	for i := range splits {
		b := &splits[i]
		switch b.Direction {
		case Vertical:
			if y >= b.Area.Y && y < b.Area.Y+b.Area.H && (x == b.Pos || x == b.Pos-1) && b.Pos-1 >= b.Area.X {
				return b
			}
		case Horizontal:
			if x >= b.Area.X && x < b.Area.X+b.Area.W && (y == b.Pos || y == b.Pos-1) && b.Pos-1 >= b.Area.Y {
				return b
			}
		}
	}
	return nil
}

// SetRatioAt updates the ratio of the split node addressed by path.
func (l *Layout) SetRatioAt(path []bool, ratio float64) bool {
	return setRatioAt(l.root, path, clampRatio(ratio))
}

// Root exposes the tree for tests and snapshots.
func (l *Layout) Root() *Node { return l.root }

// --- tree helpers ---

func countPanes(n *Node) int {
	if n == nil {
		return 0
	}
	if n.Leaf >= 0 {
		return 1
	}
	return countPanes(n.First) + countPanes(n.Second)
}

func collectIDs(n *Node) []int {
	if n == nil {
		return nil
	}
	if n.Leaf >= 0 {
		return []int{n.Leaf}
	}
	return append(collectIDs(n.First), collectIDs(n.Second)...)
}

func firstLeaf(n *Node) int {
	if n == nil {
		return -1
	}
	if n.Leaf >= 0 {
		return n.Leaf
	}
	if f := firstLeaf(n.First); f >= 0 {
		return f
	}
	return firstLeaf(n.Second)
}

func splitAt(n *Node, target int, dir Direction, newID int, ratio float64) *Node {
	if n.Leaf == target {
		return SplitNode(dir, ratio, LeafNode(target), LeafNode(newID))
	}
	if n.Leaf >= 0 {
		return n
	}
	return &Node{
		Leaf:      -1,
		Direction: n.Direction,
		Ratio:     n.Ratio,
		First:     splitAt(n.First, target, dir, newID, ratio),
		Second:    splitAt(n.Second, target, dir, newID, ratio),
	}
}

// removePane drops target. It returns the replacement subtree, whether the
// pane was found, and the sibling subtree that shared target's parent split
// (used to move focus).
func removePane(n *Node, target int) (*Node, bool, *Node) {
	if n == nil {
		return nil, false, nil
	}
	if n.Leaf == target {
		return nil, true, nil
	}
	if n.Leaf >= 0 {
		return n, false, nil
	}
	if nf, ok, sib := removePane(n.First, target); ok {
		if nf == nil {
			return n.Second, true, n.Second
		}
		return &Node{Leaf: -1, Direction: n.Direction, Ratio: n.Ratio, First: nf, Second: n.Second}, true, sib
	}
	ns, ok, sib := removePane(n.Second, target)
	if !ok {
		return n, false, nil
	}
	if ns == nil {
		return n.First, true, n.First
	}
	return &Node{Leaf: -1, Direction: n.Direction, Ratio: n.Ratio, First: n.First, Second: ns}, true, sib
}

func setRatioAt(n *Node, path []bool, ratio float64) bool {
	if n == nil || n.Leaf >= 0 {
		return false
	}
	if len(path) == 0 {
		n.Ratio = ratio
		return true
	}
	if path[0] {
		return setRatioAt(n.Second, path[1:], ratio)
	}
	return setRatioAt(n.First, path[1:], ratio)
}

func collectPanes(n *Node, area Rect, focus int, out *[]PaneRect) {
	if n == nil {
		return
	}
	if n.Leaf >= 0 {
		*out = append(*out, PaneRect{ID: n.Leaf, Rect: area, IsFocused: n.Leaf == focus})
		return
	}
	a, b := splitRect(area, n.Direction, n.Ratio)
	collectPanes(n.First, a, focus, out)
	collectPanes(n.Second, b, focus, out)
}

func collectSplits(n *Node, area Rect, path []bool, out *[]SplitBorder) {
	if n == nil || n.Leaf >= 0 {
		return
	}
	a, b := splitRect(area, n.Direction, n.Ratio)
	pos := 0
	if n.Direction == Vertical {
		pos = a.X + a.W
	} else {
		pos = a.Y + a.H
	}
	*out = append(*out, SplitBorder{
		Pos:       pos,
		Direction: n.Direction,
		Ratio:     n.Ratio,
		Area:      area,
		Path:      append([]bool(nil), path...),
	})
	collectSplits(n.First, a, append(append([]bool(nil), path...), false), out)
	collectSplits(n.Second, b, append(append([]bool(nil), path...), true), out)
}

func splitRect(area Rect, dir Direction, ratio float64) (Rect, Rect) {
	// SPEC G1: cells = axis_len × ratio computed in f32; an exact .5 rounds
	// up to the first side and the second side owns the remainder.
	round := func(n int, r float64) int {
		return int(math.Round(float64(float32(n) * float32(r))))
	}
	switch dir {
	case Vertical:
		w1 := round(area.W, ratio)
		return Rect{area.X, area.Y, w1, area.H},
			Rect{area.X + w1, area.Y, area.W - w1, area.H}
	default:
		h1 := round(area.H, ratio)
		return Rect{area.X, area.Y, area.W, h1},
			Rect{area.X, area.Y + h1, area.W, area.H - h1}
	}
}

func rangesOverlap(s1, l1, s2, l2 int) bool {
	return s1 < s2+l2 && s2 < s1+l1
}

func center(r Rect) (float64, float64) {
	return float64(r.X) + float64(r.W)/2, float64(r.Y) + float64(r.H)/2
}

func indexOf(s []int, v int) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func containsID(s []int, v int) bool { return indexOf(s, v) >= 0 }

func removeID(s []int, v int) []int {
	out := s[:0]
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
