package runtime

import "github.com/kazuph/ikesu/internal/config"

// Configuration applies between output batches, on the app loop.
func (s *Supervisor) SetScrollbackBytes(c config.Config) {
	limit := config.DefaultScrollbackLimitBytes
	if c.Advanced.ScrollbackLimitBytes != nil {
		limit = *c.Advanced.ScrollbackLimitBytes
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scrollbackBytes = &limit
	for _, p := range s.panes {
		p.mu.Lock()
		if p.read != nil && !p.read.byteHistory {
			if !p.read.historyIndexed {
				p.read.historyCount = len(p.read.history)
			}
			p.read.byteHistory = true
			p.emu.Scrollback().OnDrop = p.read.dropHistory
		}
		p.emu.Scrollback().SetMaxBytes(limit)
		p.mu.Unlock()
	}
}
func (r *readBuffer) historyLen() int {
	if r.byteHistory || r.historyIndexed {
		return r.historyCount
	}
	return len(r.history)
}
func (r *readBuffer) historyRef(y int) rowRef {
	if y < 0 || y >= r.historyLen() {
		return rowRef{}
	}
	if y < r.baseCount {
		ref := r.historyBase.HistoryRow(r.baseFirst + y)
		return rowRef{continued: ref.Continued, width: ref.Width}
	}
	y -= r.baseCount
	return r.history[(r.start+y)%len(r.history)]
}
func (r *readBuffer) dropHistory() {
	if r.historyCount > 0 {
		if r.baseCount > 0 {
			r.baseFirst++
			r.baseCount--
			if r.baseCount == 0 {
				r.historyBase = nil
			}
		} else {
			r.start = (r.start + 1) % len(r.history)
		}
		r.historyCount--
	}
}
func (r *readBuffer) pushByteHistory(row rowRef) {
	count := r.historyCount - r.baseCount
	if count == len(r.history) {
		next := make([]rowRef, max(1, 2*len(r.history)))
		for i := 0; i < count; i++ {
			next[i] = r.historyRef(r.baseCount + i)
		}
		r.history, r.start = next, 0
	}
	r.history[(r.start+count)%len(r.history)] = row
	r.historyCount++
}

func (r *readBuffer) resizeByteRows(oldWidth, height, pushed, pulled, retained int) {
	rows := r.main
	for _, row := range rows[:pushed] {
		if row.width == 0 {
			row.width = oldWidth
		}
		r.pushByteHistory(row)
	}
	next := make([]rowRef, height)
	copy(next[pulled:], rows[pushed:])
	for y := pulled - 1; y >= 0; y-- {
		if r.historyCount == 0 {
			break
		}
		next[y] = r.historyRef(r.historyCount - 1)
		if r.historyCount == r.baseCount {
			r.baseCount--
			if r.baseCount == 0 {
				r.historyBase = nil
			}
		}
		r.historyCount--
	}
	for r.historyCount > retained {
		r.dropHistory()
	}
	r.main = next
}
