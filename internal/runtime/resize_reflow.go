package runtime

import "github.com/charmbracelet/x/vt"

func (p *Pane) resizeTerminal(width, height int) {
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
	continued := make([]vt.ReflowRow, r.historyLen()+len(r.main))
	for y := 0; y < r.historyLen(); y++ {
		ref := r.historyRef(y)
		continued[y] = vt.ReflowRow{Continued: ref.continued, Width: ref.width}
	}
	for y, ref := range r.main {
		continued[r.historyLen()+y] = vt.ReflowRow{Continued: ref.continued, Width: ref.width}
	}
	continued = p.emu.ResizeWithReflow(width, height, continued)
	n := p.emu.ScrollbackLen()
	r.history = make([]rowRef, n)
	r.start = 0
	r.historyCount = n
	for y := range r.history {
		r.history[y] = rowRef{continued: continued[y].Continued, width: continued[y].Width}
	}
	r.main = make([]rowRef, height)
	for y := range r.main {
		r.main[y] = rowRef{continued: continued[n+y].Continued, width: continued[n+y].Width}
	}
}
