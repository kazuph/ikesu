package vt

import uv "github.com/charmbracelet/ultraviolet"

// Cells and their cache ownership live here, separately from the immutable
// metadata tree. The ring releases evicted cell references immediately.
type reflowHistory struct {
	view         reflowProjection
	pool         reflowShapePool
	lines        []*reflowLogicalLine
	start, count int
	cachedRow    int
	cached       uv.Line
}

func (h *reflowHistory) at(group int) *reflowLogicalLine {
	return h.lines[(h.start+group)%len(h.lines)]
}

func (h *reflowHistory) append(cells uv.Line) {
	line := newReflowLogicalLine(cells, &h.pool)
	if h.count == len(h.lines) {
		lines := make([]*reflowLogicalLine, max(1, 2*len(h.lines)))
		for group := 0; group < h.count; group++ {
			lines[group] = h.at(group)
		}
		h.lines, h.start = lines, 0
	}
	h.lines[(h.start+h.count)%len(h.lines)] = line
	h.count++
	h.view.index = h.view.index.append(line.shape)
	h.cached = nil
}

func (h *reflowHistory) dropGroups(groups int) {
	for i := 0; i < groups; i++ {
		line := h.lines[h.start]
		h.pool.release(line.shape)
		h.lines[h.start] = nil
		h.start = (h.start + 1) % len(h.lines)
		h.count--
	}
	h.view.index = h.view.index.dropGroups(groups)
	h.cached = nil
}

func (h *reflowHistory) takeGroups(groups int) {
	for h.count > groups {
		at := (h.start + h.count - 1) % len(h.lines)
		h.pool.release(h.lines[at].shape)
		h.lines[at] = nil
		h.count--
	}
	h.view.index = h.view.index.takeGroups(groups)
	h.cached = nil
}

func (h *reflowHistory) line(row int) uv.Line {
	return h.lineInto(row, nil)
}

func (h *reflowHistory) lineInto(row int, dst uv.Line) uv.Line {
	group, start, end, _, ok := h.view.row(row)
	if !ok {
		return nil
	}
	physical := h.view.first + row
	local := physical - h.view.index.prefixGroups(h.view.width, group).rows
	shape := h.at(group).shape
	charge := shape.prefixBytes(h.view.width, local+1) - shape.prefixBytes(h.view.width, local)
	width := (charge - scrollbackRowBytes) / scrollbackCellBytes
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
	if h.cached == nil || h.cachedRow != row {
		h.cached, h.cachedRow = h.line(row), row
	}
	if x >= len(h.cached) {
		return nil
	}
	cell := h.cached[x]
	return &cell
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
	h.cached = nil
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
	h.cached = nil
	return row, before - h.view.bytes()
}
