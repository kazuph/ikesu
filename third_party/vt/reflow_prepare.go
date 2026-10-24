package vt

import uv "github.com/charmbracelet/ultraviolet"

// Preparation never changes the physical history presented to callers. A
// recorded logical line can be cached once its following boundary is known.
type reflowPrepared struct {
	history                *reflowHistory
	rows, skipped, scanned int
	pendingGroups          int
}

func appendReflowCells(logical uv.Line, row uv.Line, ref ReflowRow, wraps bool, width int) uv.Line {
	end := len(row)
	if wraps {
		end = width
		if ref.Width > 0 {
			end = ref.Width
		}
	} else {
		for end > 0 && blankCell(&row[end-1]) {
			end--
		}
	}
	for x := 0; x < end; x++ {
		cell := uv.EmptyCell
		if x < len(row) && (row[x].Width > 0 || x > 0 && row[x-1].Width > 1) {
			cell = row[x]
		}
		logical = append(logical, cell)
	}
	return logical
}

// PrepareReflowHistory consumes recorded boundaries after an output batch.
// The callback includes the first main-screen row at ScrollbackLen().
func (e *Emulator) PrepareReflowHistory(rowRef func(int) ReflowRow) {
	e.scrs[0].prepareReflowHistory(rowRef)
}

func (s *Screen) prepareReflowHistory(rowRef func(int) ReflowRow) {
	sb := s.scrollback
	if sb.indexedLen() > 0 {
		return
	}
	if sb.prepared != nil {
		sb.prepared.applyDrops()
	}
	if sb.count == 0 {
		sb.prepared = nil
		return
	}
	if sb.prepared == nil {
		sb.prepared = &reflowPrepared{history: &reflowHistory{}}
	}
	p := sb.prepared
	closed := p.rows
	for y := max(p.rows, p.scanned-1); y < sb.count; y++ {
		if !rowRef(y + 1).Continued {
			closed = y + 1
		}
	}
	p.scanned = sb.count
	if closed == p.rows {
		return
	}
	var logical, scratch uv.Line
	physical := 0
	var lines []*reflowLogicalLine
	flush := func() {
		line := newReflowLogicalLine(logical, &p.history.pool)
		line.sourceRows = physical
		lines = append(lines, line)
		clear(logical)
		logical = logical[:0]
		physical = 0
	}
	for y := p.rows; y < closed; y++ {
		if y > p.rows && !rowRef(y).Continued {
			flush()
		}
		scratch = sb.lineInto(y, scratch)
		logical = appendReflowCells(logical, scratch, rowRef(y), rowRef(y+1).Continued, s.Width())
		physical++
	}
	flush()
	p.history.appendPreparedBatch(lines)
	p.rows = closed
}

func (p *reflowPrepared) dropFirst() {
	p.scanned = max(0, p.scanned-1)
	if p.rows == 0 {
		return
	}
	p.rows--
	p.skipped++
	if p.skipped == p.history.at(p.pendingGroups).sourceRows {
		p.pendingGroups++
		p.skipped = 0
	}
}

func (p *reflowPrepared) popLast(oldRows int) {
	p.applyDrops()
	p.scanned = min(p.scanned, oldRows-1)
	if oldRows > p.rows || p.history.count == 0 {
		return
	}
	if p.history.count == 1 {
		p.rows, p.skipped = 0, 0
	} else {
		p.rows -= p.history.at(p.history.count - 1).sourceRows
	}
	p.history.takeGroups(p.history.count - 1)
}

func (p *reflowPrepared) applyDrops() {
	if p.pendingGroups > 0 {
		p.history.dropGroups(p.pendingGroups)
		p.pendingGroups = 0
	}
}

func (s *Screen) reusablePrepared(rowRef func(int) ReflowRow) (h *reflowHistory, first *reflowLogicalLine, from, groups, source int) {
	p := s.scrollback.prepared
	if p != nil {
		p.applyDrops()
	}
	if p == nil || p.history.count == 0 {
		return &reflowHistory{}, nil, 0, 0, 0
	}
	if p.skipped > 0 {
		from = 1
	}
	groups = p.history.count - from
	if groups == 0 {
		return &reflowHistory{}, nil, 0, 0, 0
	}
	h = p.history
	if from > 0 {
		var cells, scratch uv.Line
		remaining := h.at(0).sourceRows - p.skipped
		for y := 0; y < remaining; y++ {
			scratch = s.scrollback.lineInto(y, scratch)
			cells = appendReflowCells(cells, scratch, rowRef(y), rowRef(y+1).Continued, s.Width())
		}
		first = newReflowLogicalLine(cells, &h.pool)
	}
	return h, first, from, groups, p.rows
}
