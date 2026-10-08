package layout

import "fmt"

// This file holds the layout operations beyond basic split/close: implicit
// append splits, geometric neighbours, swaps, rotation, equalization,
// root-side moves, preset cycling, directional resize, and edge queries.
// Everything is pure tree/geometry math on the existing Node structure.

// LeafIDs returns pane ids in tree traversal order (left-to-right,
// top-to-bottom).
func (l *Layout) LeafIDs() []int { return collectIDs(l.root) }

// HasPane reports whether pane is a leaf of this layout.
func (l *Layout) HasPane(pane int) bool {
	return pane >= 0 && containsID(collectIDs(l.root), pane)
}

// LastLeaf returns the last pane id in logical order. It is the implicit
// split target when no pane is named (SPEC G1: 非明示pane追加は既存順の末尾へ).
func (l *Layout) LastLeaf() int {
	ids := l.PaneIDs()
	if len(ids) == 0 {
		return -1
	}
	return ids[len(ids)-1]
}

// SplitImplicit splits the last leaf in traversal order and appends the new
// pane to the tail of the logical order.
func (l *Layout) SplitImplicit(dir Direction, newPane int, ratio float64, focus bool) bool {
	last := l.LastLeaf()
	if last < 0 {
		return false
	}
	if !l.splitTree(last, dir, newPane, ratio) {
		return false
	}
	l.order = append(l.order, newPane)
	if focus {
		l.focus = newPane
	}
	return true
}

// splitTree wraps target's leaf with a new split without touching order.
func (l *Layout) splitTree(target int, dir Direction, newID int, ratio float64) bool {
	if !containsID(collectIDs(l.root), target) {
		return false
	}
	l.root = splitAt(l.root, target, dir, newID, ratio)
	return true
}

// SetOrder replaces the logical pane order (used when a root-side move
// re-saves the post-restructure leaf order).
func (l *Layout) SetOrder(ids []int) {
	tree := collectIDs(l.root)
	var ord []int
	seen := map[int]bool{}
	for _, id := range ids {
		if containsID(tree, id) && !seen[id] {
			ord = append(ord, id)
			seen[id] = true
		}
	}
	for _, id := range tree {
		if !seen[id] {
			ord = append(ord, id)
		}
	}
	l.order = ord
}

// Neighbor returns the pane that shares the edge in direction dir, or -1.
func (l *Layout) Neighbor(pane int, dir NavDir, area Rect) int {
	panes := l.Panes(area)
	var fr Rect
	found := false
	for _, p := range panes {
		if p.ID == pane {
			fr, found = p.Rect, true
		}
	}
	if !found {
		return -1
	}
	best := -1
	bestDist := 0.0
	fcx, fcy := center(fr)
	for _, p := range panes {
		if p.ID == pane {
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
	return best
}

// Edges reports which sides of pane's rect coincide with the outer layout
// area boundary.
func (l *Layout) Edges(pane int, area Rect) (left, right, up, down bool) {
	for _, p := range l.Panes(area) {
		if p.ID != pane {
			continue
		}
		r := p.Rect
		return r.X == area.X,
			r.X+r.W == area.X+area.W,
			r.Y == area.Y,
			r.Y+r.H == area.Y+area.H
	}
	return false, false, false, false
}

// SwapLeaves exchanges the pane ids held by two leaf slots. Focus follows
// the pane id, so it stays on whichever pane was focused.
func (l *Layout) SwapLeaves(a, b int) bool {
	if a == b || !containsID(collectIDs(l.root), a) || !containsID(collectIDs(l.root), b) {
		return false
	}
	swapLeafIDs(l.root, a, b)
	return true
}

func swapLeafIDs(n *Node, a, b int) {
	if n == nil {
		return
	}
	if n.Leaf == a {
		n.Leaf = b
		return
	}
	if n.Leaf == b {
		n.Leaf = a
		return
	}
	swapLeafIDs(n.First, a, b)
	swapLeafIDs(n.Second, a, b)
}

// Rotate cycles pane ids through the leaf slots in traversal order without
// changing the tree shape or ratios. Forward moves each pane id one slot
// later (the last leaf's pane goes to the first slot); backward inverts.
func (l *Layout) Rotate(forward bool) {
	ids := collectIDs(l.root)
	if len(ids) <= 1 {
		return
	}
	var rot []int
	if forward {
		rot = append([]int{ids[len(ids)-1]}, ids[:len(ids)-1]...)
	} else {
		rot = append(ids[1:], ids[0])
	}
	i := 0
	assignLeaves(l.root, rot, &i)
}

func assignLeaves(n *Node, ids []int, i *int) {
	if n == nil {
		return
	}
	if n.Leaf >= 0 {
		n.Leaf = ids[*i]
		*i++
		return
	}
	assignLeaves(n.First, ids, i)
	assignLeaves(n.Second, ids, i)
}

// Equalize resets every split ratio to firstLeaves/totalLeaves, keeping the
// tree shape (SPEC G1: Equalize pane sizes).
func (l *Layout) Equalize() {
	equalizeNode(l.root)
}

func equalizeNode(n *Node) {
	if n == nil || n.Leaf >= 0 {
		return
	}
	total := countPanes(n.First) + countPanes(n.Second)
	if total > 0 {
		n.Ratio = clampRatio(float64(countPanes(n.First)) / float64(total))
	}
	equalizeNode(n.First)
	equalizeNode(n.Second)
}

// RemoveLeaf detaches pane's leaf and collapses its parent split into the
// sibling subtree. It returns the detached leaf count (1) when the pane
// existed and more than one pane remains. Focus moves to the first leaf of
// the sibling subtree when the removed pane held it.
func (l *Layout) RemoveLeaf(pane int) bool {
	return l.removeLeaf(pane, true)
}

func (l *Layout) removeLeaf(pane int, fixFocus bool) bool {
	if l.PaneCount() == 0 {
		return false
	}
	if l.root.Leaf >= 0 {
		// Removing the last pane empties the layout; the owning tab is
		// expected to be dissolved by the caller.
		if l.root.Leaf != pane {
			return false
		}
		l.root = nil
		l.order = removeID(l.order, pane)
		l.focus = 0
		return true
	}
	newRoot, ok, sibling := removePane(l.root, pane)
	if !ok || newRoot == nil {
		return false
	}
	l.root = newRoot
	l.order = removeID(l.order, pane)
	if fixFocus && (l.focus == pane || !containsID(collectIDs(l.root), l.focus)) {
		l.focus = firstLeaf(sibling)
	}
	return true
}

// MoveToSide pulls pane out of the tree and re-wraps the root so pane sits
// alone on the named side of the root split with share 1/N (N = pane count
// after the move, which equals the count before it). The surviving panes
// keep their relative order and inner structure. The logical order is
// re-saved as the post-restructure leaf order, and pane takes focus.
func (l *Layout) MoveToSide(pane int, side NavDir) bool {
	n := l.PaneCount()
	if n <= 1 || !containsID(collectIDs(l.root), pane) {
		return false
	}
	if !l.removeLeaf(pane, false) {
		return false
	}
	rest := l.root
	leaf := LeafNode(pane)
	var dir Direction
	var first, second *Node
	var ratio float64
	switch side {
	case NavLeft, NavUp:
		if side == NavLeft {
			dir = Vertical
		} else {
			dir = Horizontal
		}
		first, second = leaf, rest
		ratio = 1.0 / float64(n)
	case NavRight, NavDown:
		if side == NavRight {
			dir = Vertical
		} else {
			dir = Horizontal
		}
		first, second = rest, leaf
		ratio = float64(n-1) / float64(n)
	}
	l.root = SplitNode(dir, ratio, first, second)
	l.SetOrder(collectIDs(l.root))
	l.focus = pane
	return true
}

// ResizeDir adjusts the nearest split along dir's axis by amount (a ratio
// delta; default step 0.05 applied by callers when amount <= 0). Right/down
// grow the first-side share (+amount); left/up shrink it (-amount). The
// result is clamped to 0.1..0.9. It returns false when the pane has no
// same-axis ancestor split.
func (l *Layout) ResizeDir(pane int, dir NavDir, amount float64) bool {
	if amount <= 0 {
		amount = 0.05
	}
	var axis Direction
	delta := amount
	switch dir {
	case NavLeft:
		axis, delta = Vertical, -amount
	case NavRight:
		axis, delta = Vertical, amount
	case NavUp:
		axis, delta = Horizontal, -amount
	case NavDown:
		axis, delta = Horizontal, amount
	}
	target := nearestSplitOnAxis(l.root, pane, axis)
	if target == nil {
		return false
	}
	nr := clampRatio(target.Ratio + delta)
	if nr == target.Ratio {
		return false
	}
	target.Ratio = nr
	return true
}

// nearestSplitOnAxis finds the deepest ancestor split of pane whose split
// direction is axis.
func nearestSplitOnAxis(n *Node, pane int, axis Direction) *Node {
	if n == nil || n.Leaf >= 0 {
		return nil
	}
	if containsID(collectIDs(n.First), pane) {
		if s := nearestSplitOnAxis(n.First, pane, axis); s != nil {
			return s
		}
	} else if containsID(collectIDs(n.Second), pane) {
		if s := nearestSplitOnAxis(n.Second, pane, axis); s != nil {
			return s
		}
	} else {
		return nil
	}
	if n.Direction == axis {
		return n
	}
	return nil
}

// --- preset cycle ---

// CyclePresets advances the layout through the preset sequence (SPEC G1:
// レイアウト cycle). Panes keep their stored logical order and the focused
// pane keeps focus. With ≤1 pane nothing changes; with 2 panes a uniform
// side-by-side split toggles to a stacked split and anything else
// normalizes to a uniform side-by-side split.
func (l *Layout) CyclePresets() {
	ids := l.PaneIDs()
	n := len(ids)
	if n <= 1 {
		return
	}
	if n == 2 {
		// Toggle the split axis and normalize to a uniform split.
		r := l.root
		if r.Direction == Vertical && r.Ratio == 0.5 {
			l.root = SplitNode(Horizontal, 0.5, LeafNode(ids[0]), LeafNode(ids[1]))
		} else {
			l.root = SplitNode(Vertical, 0.5, LeafNode(ids[0]), LeafNode(ids[1]))
		}
		return
	}
	presets := buildPresets(ids)
	cur := -1
	for i, p := range presets {
		if sameShape(l.root, p) {
			cur = i
			break
		}
	}
	next := 0
	if cur >= 0 {
		next = (cur + 1) % len(presets)
	}
	l.root = cloneNode(presets[next])
}

// buildPresets returns the seven preset trees for the leaf order ids, in
// SPEC cycle order: row, column, even 2-row grid, left-main + right grid,
// right-main + left grid, top-main + bottom row, bottom-main + top row.
func buildPresets(ids []int) []*Node {
	row := chain(Vertical, ids)
	col := chain(Horizontal, ids)
	return []*Node{
		row,
		col,
		grid(ids),
		SplitNode(Vertical, 0.5, LeafNode(ids[0]), grid(ids[1:])),
		SplitNode(Vertical, 0.5, grid(ids[1:]), LeafNode(ids[0])),
		SplitNode(Horizontal, 0.5, LeafNode(ids[0]), chain(Vertical, ids[1:])),
		SplitNode(Horizontal, 0.5, chain(Vertical, ids[1:]), LeafNode(ids[0])),
	}
}

// chain builds a second-nested split chain over ids with equalized ratios:
// the level-k split gets ratio 1/(n-k).
func chain(dir Direction, ids []int) *Node {
	if len(ids) == 1 {
		return LeafNode(ids[0])
	}
	n := len(ids)
	return SplitNode(dir, 1.0/float64(n), LeafNode(ids[0]), chain(dir, ids[1:]))
}

// grid builds the even 2-row grid: top row holds ceil(n/2) leaves, bottom
// the rest; both rows are equalized right-chains under a 0.5 down split.
func grid(ids []int) *Node {
	top := (len(ids) + 1) / 2
	if len(ids) == 1 {
		return LeafNode(ids[0])
	}
	return SplitNode(Horizontal, 0.5,
		chain(Vertical, ids[:top]),
		chain(Vertical, ids[top:]))
}

// sameShape reports whether two trees have identical structure: same
// directions on every split and the same pane id in every leaf slot. Ratios
// are ignored so a manually resized preset still counts as that preset.
func sameShape(a, b *Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Leaf >= 0 || b.Leaf >= 0 {
		return a.Leaf >= 0 && b.Leaf >= 0 && a.Leaf == b.Leaf
	}
	if a.Direction != b.Direction {
		return false
	}
	return sameShape(a.First, b.First) && sameShape(a.Second, b.Second)
}

func cloneNode(n *Node) *Node {
	if n == nil {
		return nil
	}
	if n.Leaf >= 0 {
		return LeafNode(n.Leaf)
	}
	return SplitNode(n.Direction, n.Ratio, cloneNode(n.First), cloneNode(n.Second))
}

// Fingerprint renders the tree's structure, ratios, and focused pane as a
// compact string for cheap change detection.
func (l *Layout) Fingerprint() string {
	var sb []byte
	fpNode(l.root, &sb)
	return fmt.Sprintf("%s|f=%d", sb, l.focus)
}

func fpNode(n *Node, sb *[]byte) {
	if n == nil {
		return
	}
	if n.Leaf >= 0 {
		*sb = append(*sb, fmt.Sprintf("L%d", n.Leaf)...)
		return
	}
	*sb = append(*sb, fmt.Sprintf("(%v %.4f ", n.Direction, n.Ratio)...)
	fpNode(n.First, sb)
	*sb = append(*sb, ' ')
	fpNode(n.Second, sb)
	*sb = append(*sb, ')')
}

// HasSplitPath reports whether path names an internal split node (used by
// layout.set_split_ratio's split_not_found check).
func (l *Layout) HasSplitPath(path []bool) bool {
	n := l.root
	for _, second := range path {
		if n == nil || n.Leaf >= 0 {
			return false
		}
		if second {
			n = n.Second
		} else {
			n = n.First
		}
	}
	return n != nil && n.Leaf < 0
}
