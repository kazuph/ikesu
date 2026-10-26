package vt

import "sync"

type reflowPosition struct {
	group, start, end int
	flag              ReflowRow
}

// A read cursor shares only immutable shape metadata. Sequential read and copy
// operations advance through a leaf instead of selecting every row anew.
type reflowCursor struct {
	mu                                           sync.Mutex
	index                                        *reflowIndex
	width, row, local, shapeRows, step, leafBase int
	leaf                                         *reflowIndex
	shape                                        *reflowShape
	position                                     reflowPosition
}

func (n *reflowIndex) leafAt(group int) (*reflowIndex, int) {
	base := 0
	for n.left != nil {
		if group < base+n.left.groups {
			n = n.left
		} else {
			base += n.left.groups
			n = n.right
		}
	}
	return n, base
}

func (c *reflowCursor) locate(index *reflowIndex, width, row int) reflowPosition {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.index == index && c.width == width && c.row == row {
		return c.position
	}
	if c.index == index && c.width == width && c.row+1 == row {
		if c.local+1 < c.shapeRows {
			c.local++
			if c.shape.plain {
				c.step += width
			} else {
				c.step, _ = c.shape.next(c.step, width)
			}
		} else {
			c.position.group++
			if c.position.group >= c.leafBase+c.leaf.groups {
				c.leaf, c.leafBase = index.leafAt(c.position.group)
			}
			c.shape = c.leaf.shapes[c.position.group-c.leafBase]
			c.local, c.shapeRows, c.step = 0, c.shape.cost(width).rows, 0
			if !c.shape.plain {
				c.step = c.shape.first(width)
			}
		}
	} else {
		group, start, _, _ := index.selectRow(width, row)
		c.position.group = group
		c.leaf, c.leafBase = index.leafAt(group)
		c.shape = c.leaf.shapes[group-c.leafBase]
		c.local = row - index.prefixGroups(width, group).rows
		c.shapeRows, c.step = c.shape.cost(width).rows, start
		if c.local == 0 && !c.shape.plain {
			c.step = c.shape.first(width)
		}
	}
	c.index, c.width, c.row = index, width, row
	start, consumed, painted := c.step, 0, 0
	if c.shape.plain {
		consumed = min(start+width, c.shape.length)
		painted = consumed - start
	} else {
		consumed, painted = c.shape.next(start, width)
	}
	c.position.start, c.position.end = max(0, start), max(0, start)+painted
	c.position.flag = ReflowRow{Continued: c.local > 0}
	if start >= 0 {
		c.position.flag.Width = consumed - start
	}
	if c.shape.endMarker && c.local == c.shapeRows-1 {
		c.position.flag.Width--
	}
	return c.position
}
