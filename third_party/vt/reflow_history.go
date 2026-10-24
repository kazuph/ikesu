package vt

import uv "github.com/charmbracelet/ultraviolet"

// Cells and their cache ownership live here, separately from the immutable
// metadata tree. The ring releases evicted cell references immediately.
type reflowHistory struct {
	view                     reflowProjection
	pool                     reflowShapePool
	lines                    []*reflowLogicalLine
	start, count             int
	cachedRow                int
	cachedLine               *reflowLogicalLine
	cachedStart, cachedWidth int
	cachedValid              bool
	clipping                 reflowClipping
	maxSpan                  int
}

func (h *reflowHistory) at(group int) *reflowLogicalLine {
	return h.lines[(h.start+group)%len(h.lines)]
}

func (h *reflowHistory) append(cells uv.Line) {
	line := newReflowLogicalLine(cells, &h.pool)
	h.appendLine(line)
}

func (h *reflowHistory) grow() {
	if h.count == len(h.lines) {
		lines := make([]*reflowLogicalLine, max(1, 2*len(h.lines)))
		for group := 0; group < h.count; group++ {
			lines[group] = h.at(group)
		}
		h.lines, h.start = lines, 0
	}
}

func (h *reflowHistory) appendLine(line *reflowLogicalLine) {
	h.attach(line)
	h.grow()
	h.lines[(h.start+h.count)%len(h.lines)] = line
	h.count++
	h.view.index = h.view.index.append(line.shape)
	h.cachedLine, h.cachedValid = nil, false
	h.view.cursor = nil
}

func (h *reflowHistory) appendPreparedBatch(lines []*reflowLogicalLine) {
	shapes := make([]*reflowShape, len(lines))
	for i, line := range lines {
		h.attach(line)
		h.grow()
		h.lines[(h.start+h.count)%len(h.lines)] = line
		h.count++
		shapes[i] = line.shape
	}
	h.view.index = joinReflowIndex(h.view.index, reflowIndexBatch(shapes))
	h.cachedLine, h.cachedValid = nil, false
	h.view.cursor = nil
}

func (h *reflowHistory) prependLine(line *reflowLogicalLine) {
	h.attach(line)
	h.grow()
	h.start = (h.start + len(h.lines) - 1) % len(h.lines)
	h.lines[h.start] = line
	h.count++
	h.view.index = joinReflowIndex((*reflowIndex)(nil).append(line.shape), h.view.index)
	h.cachedLine, h.cachedValid = nil, false
	h.view.cursor = nil
}

func (h *reflowHistory) attach(line *reflowLogicalLine) {
	h.clipping.serial++
	line.clipping, line.serial = &h.clipping, h.clipping.serial
	h.maxSpan = max(h.maxSpan, line.shape.maxSpan)
}

func (h *reflowHistory) project(width int) {
	if width < h.maxSpan {
		if h.clipping.through == nil {
			h.clipping.through = make(map[int]uint64)
		}
		h.clipping.through[width] = h.clipping.serial
	}
}

func (h *reflowHistory) dropGroups(groups int) {
	for i := 0; i < groups; i++ {
		line := h.lines[h.start]
		h.pool.release(line.ownedShape())
		h.lines[h.start] = nil
		h.start = (h.start + 1) % len(h.lines)
		h.count--
	}
	h.view.index = h.view.index.dropGroups(groups)
	h.cachedLine, h.cachedValid = nil, false
	h.view.cursor = nil
}

func (h *reflowHistory) takeGroups(groups int) {
	for h.count > groups {
		at := (h.start + h.count - 1) % len(h.lines)
		h.pool.release(h.lines[at].ownedShape())
		h.lines[at] = nil
		h.count--
	}
	h.view.index = h.view.index.takeGroups(groups)
	h.cachedLine, h.cachedValid = nil, false
	h.view.cursor = nil
}

func (h *reflowHistory) line(row int) uv.Line {
	return h.lineInto(row, nil)
}

func (h *reflowHistory) copyRow(dst uv.Line, row int) {
	group, start, end, _, ok := h.view.row(row)
	if ok {
		h.at(group).copyRange(dst, start, end)
	}
}

func (h *reflowHistory) lineInto(row int, dst uv.Line) uv.Line {
	group, start, end, _, ok := h.view.row(row)
	if !ok {
		return nil
	}
	shape := h.at(group).shape
	width := end - start
	if !shape.plain && width > 0 {
		width = max(0, shape.last[end]-start+1)
	}
	line := dst
	if cap(line) < width {
		line = make(uv.Line, width)
	} else {
		clear(line)
		line = line[:width]
	}
	h.at(group).copyRange(line, start, min(start+width, end))
	return line
}

func (h *reflowHistory) cellAt(x, row int) *uv.Cell {
	if x < 0 || row < 0 || row >= h.view.rows {
		return nil
	}
	if !h.cachedValid || h.cachedRow != row {
		group, start, end, _, _ := h.view.row(row)
		h.cachedLine, h.cachedStart, h.cachedWidth = h.at(group), start, end-start
		if !h.cachedLine.shape.plain && h.cachedWidth > 0 {
			h.cachedWidth = max(0, h.cachedLine.shape.last[end]-start+1)
		}
		h.cachedRow = row
		h.cachedValid = true
	}
	if x >= h.cachedWidth {
		return nil
	}
	var cell [1]uv.Cell
	h.cachedLine.copyRange(cell[:], h.cachedStart+x, h.cachedStart+x+1)
	return &cell[0]
}

func (h *reflowHistory) dropRows(rows int) int {
	before := h.view.bytes()
	h.view.first += rows
	h.view.rows -= rows
	if h.view.rows == 0 {
		h.dropGroups(h.count)
		h.view.first = 0
		return before
	}
	group, _, _, _, _ := h.view.row(0)
	if group > 0 {
		first := h.view.index.prefixGroups(h.view.width, group).rows
		h.dropGroups(group)
		h.view.first -= first
	}
	h.cachedLine, h.cachedValid = nil, false
	h.view.cursor = nil
	return before - h.view.bytes()
}

func (h *reflowHistory) popLast() (uv.Line, int) {
	row := h.line(h.view.rows - 1)
	before := h.view.bytes()
	h.view.rows--
	if h.view.rows == 0 {
		h.dropGroups(h.count)
		h.view.first = 0
		return row, before
	}
	h.cachedLine, h.cachedValid = nil, false
	h.view.cursor = nil
	return row, before - h.view.bytes()
}
