package runtime

import "github.com/kazuph/goterrace/internal/config"

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
			p.read.historyCount = len(p.read.history)
			p.read.byteHistory = true
			p.emu.Scrollback().OnDrop = p.read.dropHistory
		}
		p.emu.Scrollback().SetMaxBytes(limit)
		p.mu.Unlock()
	}
}
func (r *readBuffer) historyLen() int {
	if r.byteHistory {
		return r.historyCount
	}
	return len(r.history)
}
func (r *readBuffer) historyRef(y int) rowRef {
	if y < 0 || y >= r.historyLen() {
		return rowRef{}
	}
	return r.history[(r.start+y)%len(r.history)]
}
func (r *readBuffer) dropHistory() {
	if r.historyCount > 0 {
		r.start = (r.start + 1) % len(r.history)
		r.historyCount--
	}
}
func (r *readBuffer) pushByteHistory(row rowRef) {
	if r.historyCount == len(r.history) {
		next := make([]rowRef, max(1, 2*len(r.history)))
		for i := 0; i < r.historyCount; i++ {
			next[i] = r.historyRef(i)
		}
		r.history, r.start = next, 0
	}
	r.history[(r.start+r.historyCount)%len(r.history)] = row
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
		r.historyCount--
	}
	for r.historyCount > retained {
		r.dropHistory()
	}
	r.main = next
}
