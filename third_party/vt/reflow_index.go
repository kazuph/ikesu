package vt

import (
	"math/bits"
	"sort"
)

// Each leaf contains at most one machine word's worth of logical lines. Width
// events summarize a subtree, so a resize query never walks all its lines.
type reflowIndex struct {
	left, right *reflowIndex
	shapes      []*reflowShape
	events      []reflowCostEvent
	groups      int
	height      int
}

func (n *reflowIndex) cost(width int) reflowCost {
	if n == nil {
		return reflowCost{}
	}
	i := sort.Search(len(n.events), func(i int) bool { return n.events[i].width > width }) - 1
	return n.events[max(0, i)].cost
}

func sumReflowEvents(a, b []reflowCostEvent) []reflowCostEvent {
	out := make([]reflowCostEvent, 0, len(a)+len(b))
	var x, y reflowCost
	for i, j := 0, 0; i < len(a) || j < len(b); {
		width := 0
		if j == len(b) || i < len(a) && a[i].width < b[j].width {
			width = a[i].width
		} else {
			width = b[j].width
		}
		if i < len(a) && a[i].width == width {
			x = a[i].cost
			i++
		}
		if j < len(b) && b[j].width == width {
			y = b[j].cost
			j++
		}
		cost := reflowCost{x.rows + y.rows, x.bytes + y.bytes}
		if len(out) == 0 || out[len(out)-1].cost != cost {
			out = append(out, reflowCostEvent{width, cost})
		}
	}
	return out
}

func (n *reflowIndex) refresh() {
	if n.left == nil {
		n.groups, n.height = len(n.shapes), 1
		n.events = nil
		for _, shape := range n.shapes {
			n.events = sumReflowEvents(n.events, shape.events)
		}
		return
	}
	n.groups = n.left.groups + n.right.groups
	n.height = max(n.left.height, n.right.height) + 1
	n.events = sumReflowEvents(n.left.events, n.right.events)
}

func (n *reflowIndex) rotateLeft() *reflowIndex {
	copyNode, copyRight := *n, *n.right
	n, r := &copyNode, &copyRight
	n.right, r.left = r.left, n
	n.refresh()
	r.refresh()
	return r
}

func (n *reflowIndex) rotateRight() *reflowIndex {
	copyNode, copyLeft := *n, *n.left
	n, l := &copyNode, &copyLeft
	n.left, l.right = l.right, n
	n.refresh()
	l.refresh()
	return l
}

func (n *reflowIndex) balance() *reflowIndex {
	n.refresh()
	if n.left == nil {
		return n
	}
	if n.left.height > n.right.height+1 {
		if n.left.right.height > n.left.left.height {
			n.left = n.left.rotateLeft()
		}
		return n.rotateRight()
	}
	if n.right.height > n.left.height+1 {
		if n.right.left.height > n.right.right.height {
			n.right = n.right.rotateRight()
		}
		return n.rotateLeft()
	}
	return n
}

func (n *reflowIndex) append(shape *reflowShape) *reflowIndex {
	if n == nil {
		n = &reflowIndex{shapes: []*reflowShape{shape}}
		n.refresh()
		return n
	}
	copyNode := *n
	n = &copyNode
	if n.left == nil {
		if len(n.shapes) < bits.UintSize {
			n.shapes = append(append([]*reflowShape(nil), n.shapes...), shape)
			n.refresh()
			return n
		}
		n = &reflowIndex{left: n, right: (*reflowIndex)(nil).append(shape)}
		return n.balance()
	}
	n.right = n.right.append(shape)
	return n.balance()
}

func (n *reflowIndex) dropGroups(count int) *reflowIndex {
	if n == nil || count >= n.groups {
		return nil
	}
	if count <= 0 {
		return n
	}
	if n.left == nil {
		leaf := &reflowIndex{shapes: append([]*reflowShape(nil), n.shapes[count:]...)}
		return leaf.balance()
	}
	if count >= n.left.groups {
		return n.right.dropGroups(count - n.left.groups)
	}
	return joinReflowIndex(n.left.dropGroups(count), n.right)
}

func joinReflowIndex(left, right *reflowIndex) *reflowIndex {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	if left.height > right.height+1 {
		copyNode := *left
		left = &copyNode
		left.right = joinReflowIndex(left.right, right)
		return left.balance()
	}
	if right.height > left.height+1 {
		copyNode := *right
		right = &copyNode
		right.left = joinReflowIndex(left, right.left)
		return right.balance()
	}
	n := &reflowIndex{left: left, right: right}
	n.refresh()
	return n
}

func (n *reflowIndex) selectRow(width, row int) (group, start, end int, ok bool) {
	if n == nil || row < 0 || row >= n.cost(width).rows {
		return 0, 0, 0, false
	}
	if n.left != nil {
		left := n.left.cost(width)
		if row < left.rows {
			return n.left.selectRow(width, row)
		}
		group, start, end, ok = n.right.selectRow(width, row-left.rows)
		return group + n.left.groups, start, end, ok
	}
	for group, shape := range n.shapes {
		cost := shape.cost(width)
		if row < cost.rows {
			start, end, ok = shape.bounds(width, row)
			return group, start, end, ok
		}
		row -= cost.rows
	}
	return 0, 0, 0, false
}

func (n *reflowIndex) prefixBytes(width, rows int) int {
	if n == nil || rows <= 0 {
		return 0
	}
	if cost := n.cost(width); rows >= cost.rows {
		return cost.bytes
	}
	if n.left != nil {
		left := n.left.cost(width)
		if rows <= left.rows {
			return n.left.prefixBytes(width, rows)
		}
		return left.bytes + n.right.prefixBytes(width, rows-left.rows)
	}
	charged := 0
	for _, shape := range n.shapes {
		cost := shape.cost(width)
		if rows <= cost.rows {
			return charged + shape.prefixBytes(width, rows)
		}
		charged += cost.bytes
		rows -= cost.rows
	}
	return charged
}

func (n *reflowIndex) prefixGroups(width, groups int) reflowCost {
	if n == nil || groups <= 0 {
		return reflowCost{}
	}
	if groups >= n.groups {
		return n.cost(width)
	}
	if n.left != nil {
		if groups <= n.left.groups {
			return n.left.prefixGroups(width, groups)
		}
		a, b := n.left.cost(width), n.right.prefixGroups(width, groups-n.left.groups)
		return reflowCost{a.rows + b.rows, a.bytes + b.bytes}
	}
	var total reflowCost
	for _, shape := range n.shapes[:groups] {
		c := shape.cost(width)
		total.rows += c.rows
		total.bytes += c.bytes
	}
	return total
}

func (n *reflowIndex) takeGroups(groups int) *reflowIndex {
	if n == nil || groups <= 0 {
		return nil
	}
	if groups >= n.groups {
		return n
	}
	if n.left == nil {
		leaf := &reflowIndex{shapes: append([]*reflowShape(nil), n.shapes[:groups]...)}
		return leaf.balance()
	}
	if groups <= n.left.groups {
		return n.left.takeGroups(groups)
	}
	return joinReflowIndex(n.left, n.right.takeGroups(groups-n.left.groups))
}
