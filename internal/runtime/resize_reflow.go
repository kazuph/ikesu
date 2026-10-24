package runtime

import "github.com/charmbracelet/x/vt"

func (p *Pane) resizeTerminal(width, height int) {
	p.emu.SetCellPixels(p.cellWidth, p.cellHeight)
	if p.read != nil && width != p.emu.Width() {
		p.resizeReflow(width, height)
	} else {
		p.emu.Resize(width, height)
	}
	pos := p.emu.CursorPosition()
	p.cursor.X, p.cursor.Y = pos.X, pos.Y
}

// resizeReflow gives the terminal cells their recorded logical boundaries;
// styles and hyperlinks remain attached to cells during width changes.
func (p *Pane) resizeReflow(width, height int) {
	r := p.read
	oldHistory := r.historyLen()
	snapshot := p.emu.ResizeWithReflowView(width, height, func(y int) vt.ReflowRow {
		var ref rowRef
		if y < oldHistory {
			ref = r.historyRef(y)
		} else if y-oldHistory < len(r.main) {
			ref = r.main[y-oldHistory]
		}
		return vt.ReflowRow{Continued: ref.continued, Width: ref.width}
	})
	r.history, r.start = nil, 0
	r.historyIndexed = true
	r.historyBase = &snapshot
	r.baseFirst, r.baseCount = 0, snapshot.HistoryLen()
	r.historyCount = r.baseCount
	if r.baseCount == 0 {
		r.historyBase = nil
	}
	r.main = make([]rowRef, height)
	for y := range r.main {
		ref := snapshot.MainRow(y)
		r.main[y] = rowRef{continued: ref.Continued, width: ref.Width}
	}
}
