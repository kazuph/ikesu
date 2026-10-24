package vt

import (
	"sort"

	uv "github.com/charmbracelet/ultraviolet"
)

// ReflowSnapshot retains row metadata independently of mutable terminal cells.
// Runtime can consume its old snapshot during resize callbacks.
type ReflowSnapshot struct {
	history reflowProjection
	main    []ReflowRow
}

func (r ReflowSnapshot) HistoryLen() int { return r.history.rows }
func (r ReflowSnapshot) HistoryRow(y int) ReflowRow {
	_, _, _, flag, _ := r.history.row(y)
	return flag
}
func (r ReflowSnapshot) MainRow(y int) ReflowRow { return r.main[y] }

func (r ReflowSnapshot) materialize() []ReflowRow {
	rows := make([]ReflowRow, r.HistoryLen()+len(r.main))
	for y := 0; y < r.HistoryLen(); y++ {
		rows[y] = r.HistoryRow(y)
	}
	copy(rows[r.HistoryLen():], r.main)
	return rows
}

// reusableHistory separates complete immutable logical lines from the mutable
// boundary line. Only the boundaries and new output need reconstruction.
func (s *Screen) reusableHistory(rowRef func(int) ReflowRow) (h *reflowHistory, first *reflowLogicalLine, from, groups, source int) {
	h = s.scrollback.indexed
	if h == nil || h.view.rows == 0 {
		return s.reusablePrepared(rowRef)
	}
	p := h.view
	firstGroup, firstCell, _, _, _ := p.row(0)
	lastGroup, _, _, _, _ := p.row(p.rows - 1)
	from, end := firstGroup, lastGroup
	if p.first != p.index.prefixGroups(p.width, firstGroup).rows {
		from++
	}
	if p.first+p.rows == p.index.prefixGroups(p.width, lastGroup+1).rows {
		end++
	}
	if end <= from {
		return &reflowHistory{}, nil, 0, 0, 0
	}
	if from > firstGroup {
		old := h.at(firstGroup)
		cells := make(uv.Line, old.shape.length-firstCell)
		old.copyRange(cells, firstCell, old.shape.length)
		first = newReflowLogicalLine(cells, &h.pool)
	}
	return h, first, from, end - from, p.index.prefixGroups(p.width, end).rows - p.first
}

func (s *Screen) reflowIndexed(width, height int, rowRef func(int) ReflowRow, phantom, stableHistory bool) ReflowSnapshot {
	history := s.scrollback.Len()
	cursorRow := history + s.cur.Y
	last := s.cur.Y
	for y := s.Height() - 1; y > last; y-- {
		if !s.isLineEmpty(s.buf.Line(y)) {
			last = y
			break
		}
	}
	h, first, from, groups, source := &reflowHistory{}, (*reflowLogicalLine)(nil), 0, 0, 0
	if stableHistory {
		h, first, from, groups, source = s.reusableHistory(rowRef)
	}
	var tail []*reflowLogicalLine
	var logical, scratch uv.Line
	cursorOffset, cursorGroup, cursorCell := -1, -1, 0
	flush := func() {
		marker := cursorOffset == len(logical)
		if marker {
			logical = append(logical, uv.Cell{})
		}
		line := newReflowLogicalLine(logical, &h.pool)
		if marker {
			shape := *line.shape
			shape.endMarker = true
			line.pooled, line.shape = line.shape, &shape
		}
		if cursorOffset >= 0 {
			cursorGroup, cursorCell = len(tail), cursorOffset
			if marker {
				cursorCell = line.shape.length - 1
			}
		}
		tail = append(tail, line)
		clear(logical)
		logical = logical[:0]
		cursorOffset = -1
	}
	for y := source; y <= history+last; y++ {
		var row uv.Line
		if y < history {
			scratch = s.scrollback.lineInto(y, scratch)
			row = scratch
		} else {
			row = s.buf.Line(y - history)
		}
		if y > source && !rowRef(y).Continued {
			flush()
		}
		end := len(row)
		if rowRef(y + 1).Continued {
			end = s.Width()
			if ref := rowRef(y); ref.Width > 0 {
				end = ref.Width
			}
		} else {
			for end > 0 && blankCell(&row[end-1]) {
				end--
			}
		}
		if y == cursorRow {
			col := s.cur.X
			if phantom {
				col++
			}
			cursorOffset = len(logical) + col
			end = max(end, col)
		}
		for x := 0; x < end; x++ {
			cell := uv.EmptyCell
			if x < len(row) && (row[x].Width > 0 || x > 0 && row[x-1].Width > 1) {
				cell = row[x]
			}
			logical = append(logical, cell)
		}
	}
	flush()
	h.takeGroups(from + groups)
	h.dropGroups(from)
	if first != nil {
		h.prependLine(first)
	}
	cursorGroup += h.count
	h.appendPreparedBatch(tail)
	h.view.width, h.view.first = width, 0
	h.view.rows = h.view.index.cost(width).rows
	h.project(width)
	shape := h.at(cursorGroup).shape
	row := sort.Search(shape.cost(width).rows, func(row int) bool {
		start, end, _ := shape.bounds(width, row)
		if start != end && !shape.plain {
			end, _ = shape.next(start, width)
		}
		return end > cursorCell
	})
	start, _, _ := shape.bounds(width, row)
	newCursor := uv.Position{X: cursorCell - start, Y: h.view.index.prefixGroups(width, cursorGroup).rows + row}
	cut := max(0, h.view.rows-height)
	buf := uv.NewRenderBuffer(width, height)
	main := make([]ReflowRow, height)
	for y := 0; y < min(height, h.view.rows); y++ {
		clear(buf.Line(y))
		h.copyRow(buf.Line(y), cut+y)
		_, _, _, main[y], _ = h.view.row(cut + y)
	}
	h.view.rows = cut
	skipped := max(0, cut-s.scrollback.maxLines)
	dropped := skipped
	if s.scrollback.byteLimited {
		need := h.view.bytes() - s.scrollback.maxBytes
		dropped = max(dropped, sort.Search(cut+1, func(rows int) bool {
			return h.view.index.prefixBytes(width, rows) >= need
		}))
	}
	if cut > 0 && dropped > 0 {
		h.dropRows(dropped)
	}
	s.scrollback.Clear()
	if h.view.rows > 0 {
		s.scrollback.indexed = h
		if s.scrollback.byteLimited {
			s.scrollback.bytes = h.view.bytes()
		}
	}
	if s.scrollback.byteLimited && s.scrollback.OnDrop != nil {
		for i := skipped; i < dropped; i++ {
			s.scrollback.OnDrop()
		}
	}
	s.buf, s.scrollRows = buf, nil
	s.cur.X, s.cur.Y = newCursor.X, max(0, newCursor.Y-cut)
	s.scroll = s.buf.Bounds()
	if h.view.cursor == nil {
		h.view.cursor = &reflowCursor{}
	}
	return ReflowSnapshot{history: h.view, main: main}
}
