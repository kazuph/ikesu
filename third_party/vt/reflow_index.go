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
	return sumReflowEventsInto(make([]reflowCostEvent, 0, len(a)+len(b)), a, b)
}

func sumReflowEventsInto(out, a, b []reflowCostEvent) []reflowCostEvent {
	out = out[:0]
	if cap(out) == 0 {
		out = make([]reflowCostEvent, 0, len(a)+len(b))
	}
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
	n.refreshOwned(false)
}

func (n *reflowIndex) refreshOwned(owned bool) {
	if n.left == nil {
		n.groups, n.height = len(n.shapes), 1
		same := len(n.shapes) > 0
		for _, shape := range n.shapes {
			same = same && shape == n.shapes[0]
		}
		if same {
			var reuse []reflowCostEvent
			if owned {
				reuse = n.events[:0]
			}
			n.events = append(reuse, n.shapes[0].events...)
			for i, event := range n.shapes[0].events {
				n.events[i] = reflowCostEvent{event.width, reflowCost{event.cost.rows * n.groups, event.cost.bytes * n.groups}}
			}
			return
		}
		n.events = nil
		for _, shape := range n.shapes {
			n.events = sumReflowEvents(n.events, shape.events)
		}
		return
	}
	n.groups = n.left.groups + n.right.groups
	n.height = max(n.left.height, n.right.height) + 1
	if owned {
		n.events = sumReflowEventsInto(n.events, n.left.events, n.right.events)
	} else {
		n.events = sumReflowEvents(n.left.events, n.right.events)
	}
}

func reflowIndexBatch(shapes []*reflowShape) *reflowIndex {
	return reflowIndexBatchRecycled(shapes, nil)
}

func reflowIndexBatchRecycled(shapes []*reflowShape, free *[]*reflowIndex) *reflowIndex {
	if len(shapes) == 0 {
		return nil
	}
	var n *reflowIndex
	if free != nil && len(*free) > 0 {
		count := len(*free)
		n = (*free)[count-1]
		(*free)[count-1] = nil
		*free = (*free)[:count-1]
	} else {
		n = &reflowIndex{}
	}
	if len(shapes) <= bits.UintSize {
		n.shapes = append(n.shapes[:0], shapes...)
	} else {
		middle := len(shapes) / 2
		n.left = reflowIndexBatchRecycled(shapes[:middle], free)
		n.right = reflowIndexBatchRecycled(shapes[middle:], free)
	}
	n.refreshOwned(true)
	return n
}

// Collect only nodes that dropGroupsOwned discards completely. A partially
// retained parent is reused by joinReflowIndexOwned and cannot enter the pool.
func (n *reflowIndex) collectDropped(count int, free *[]*reflowIndex) {
	if n == nil || count <= 0 {
		return
	}
	if count >= n.groups {
		n.left.collectDropped(count, free)
		n.right.collectDropped(count, free)
		*free = append(*free, n)
		return
	}
	if n.left == nil {
		return
	}
	n.left.collectDropped(count, free)
	if count >= n.left.groups {
		n.right.collectDropped(count-n.left.groups, free)
		*free = append(*free, n)
	}
}

// owned is permitted only while the preparation tree has never been exposed
// through a ReflowSnapshot. Published trees always use persistent updates.
func copyReflowNode(n *reflowIndex, owned bool) *reflowIndex {
	if owned {
		return n
	}
	copy := *n
	return &copy
}

func (n *reflowIndex) rotateLeftOwned(owned bool) *reflowIndex {
	n = copyReflowNode(n, owned)
	r := copyReflowNode(n.right, owned)
	n.right, r.left = r.left, n
	n.refreshOwned(owned)
	r.refreshOwned(owned)
	return r
}

func (n *reflowIndex) rotateRightOwned(owned bool) *reflowIndex {
	n = copyReflowNode(n, owned)
	l := copyReflowNode(n.left, owned)
	n.left, l.right = l.right, n
	n.refreshOwned(owned)
	l.refreshOwned(owned)
	return l
}

func (n *reflowIndex) balance() *reflowIndex {
	return n.balanceOwned(false)
}

func (n *reflowIndex) balanceOwned(owned bool) *reflowIndex {
	n.refreshOwned(owned)
	if n.left == nil {
		return n
	}
	if n.left.height > n.right.height+1 {
		if n.left.right.height > n.left.left.height {
			n.left = n.left.rotateLeftOwned(owned)
		}
		return n.rotateRightOwned(owned)
	}
	if n.right.height > n.left.height+1 {
		if n.right.left.height > n.right.right.height {
			n.right = n.right.rotateRightOwned(owned)
		}
		return n.rotateLeftOwned(owned)
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
	return n.dropGroupsOwned(count, false)
}

func (n *reflowIndex) dropGroupsOwned(count int, owned bool) *reflowIndex {
	if n == nil || count >= n.groups {
		return nil
	}
	if count <= 0 {
		return n
	}
	if n.left == nil {
		if owned {
			clear(n.shapes[:count])
			n.shapes = n.shapes[count:]
			return n.balanceOwned(true)
		}
		leaf := &reflowIndex{shapes: append([]*reflowShape(nil), n.shapes[count:]...)}
		return leaf.balance()
	}
	if count >= n.left.groups {
		return n.right.dropGroupsOwned(count-n.left.groups, owned)
	}
	return joinReflowIndexOwned(n.left.dropGroupsOwned(count, owned), n.right, n, owned)
}

func joinReflowIndex(left, right *reflowIndex) *reflowIndex {
	return joinReflowIndexOwned(left, right, nil, false)
}

func joinReflowIndexOwned(left, right, spare *reflowIndex, owned bool) *reflowIndex {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	if left.height > right.height+1 {
		left = copyReflowNode(left, owned)
		left.right = joinReflowIndexOwned(left.right, right, spare, owned)
		return left.balanceOwned(owned)
	}
	if right.height > left.height+1 {
		right = copyReflowNode(right, owned)
		right.left = joinReflowIndexOwned(left, right.left, spare, owned)
		return right.balanceOwned(owned)
	}
	n := spare
	if !owned || n == nil {
		n = &reflowIndex{}
	}
	n.left, n.right, n.shapes = left, right, nil
	n.refreshOwned(owned)
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
	return n.takeGroupsOwned(groups, false)
}

func (n *reflowIndex) takeGroupsOwned(groups int, owned bool) *reflowIndex {
	if n == nil || groups <= 0 {
		return nil
	}
	if groups >= n.groups {
		return n
	}
	if n.left == nil {
		if owned {
			clear(n.shapes[groups:])
			n.shapes = n.shapes[:groups]
			return n.balanceOwned(true)
		}
		leaf := &reflowIndex{shapes: append([]*reflowShape(nil), n.shapes[:groups]...)}
		return leaf.balance()
	}
	if groups <= n.left.groups {
		return n.left.takeGroupsOwned(groups, owned)
	}
	return joinReflowIndexOwned(n.left, n.right.takeGroupsOwned(groups-n.left.groups, owned), n, owned)
}
