package runtime

import (
	"fmt"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// AgentAttachFrame renders only this terminal, including styles and cursor.
// Scroll offset belongs to the direct client and never changes session UI state.
func (p *Pane) AgentAttachFrame(offset, cols, rows int) (string, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	history := p.emu.ScrollbackLen()
	if p.altScreen {
		history = 0
	}
	offset = max(0, min(offset, history))
	start := history - offset
	var b strings.Builder
	b.WriteString("\x1b[?25l\x1b[H")
	for y := 0; y < rows; y++ {
		row := make(uv.Line, cols)
		for x := range row {
			var c *uv.Cell
			at := start + y
			if at < history {
				c = p.emu.ScrollbackCellAt(x, at)
			} else if at-history < p.emu.Height() && x < p.emu.Width() {
				c = p.emu.CellAt(x, at-history)
			}
			if c != nil {
				row[x] = *c
			} else {
				row[x] = uv.Cell{Content: " ", Width: 1}
			}
		}
		fmt.Fprintf(&b, "\x1b[%d;1H%s\x1b[0m\x1b[K", y+1, renderReadRow(row, false, false, cols))
	}
	if offset == 0 {
		cur := p.cursor
		fmt.Fprintf(&b, "\x1b[%d;%dH", cur.Y+1, cur.X+1)
		if p.cursor.Visible {
			b.WriteString("\x1b[?25h")
		}
	}
	return b.String(), offset
}
