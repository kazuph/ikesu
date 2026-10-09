package render

import (
	"bytes"
	"fmt"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Encoder diffs consecutive frames and emits synchronized terminal output.
// It keeps the last written frame and the currently emitted style so diffs
// are minimal: unchanged cells are never rewritten.
type Encoder struct {
	prev    *Frame
	curSty  uv.Style
	haveSty bool
}

const (
	syncStart  = "\x1b[?2026h"
	syncEnd    = "\x1b[?2026l"
	hideCursor = "\x1b[?25l"
	showCursor = "\x1b[?25h"
	clearAll   = "\x1b[2J"
)

// Frame encodes the transition from the previous frame to f inside a
// synchronized-output block, then parks the outer cursor at the focused
// pane's cursor (the IME anchor) with the right visibility — both inside
// and outside the sync region.
func (e *Encoder) Frame(f *Frame, anchorX, anchorY int, anchorVisible bool) []byte {
	var b bytes.Buffer
	b.WriteString(syncStart)
	b.WriteString(hideCursor)

	if e.prev == nil || e.prev.W != f.W || e.prev.H != f.H {
		e.full(f, &b)
	} else {
		e.diff(f, &b)
	}

	// Park the cursor at the anchor before ending the sync block.
	b.WriteString(cup(anchorY, anchorX))
	if anchorVisible {
		b.WriteString(showCursor)
	} else {
		b.WriteString(hideCursor)
	}
	b.WriteString(syncEnd)
	// Repeat the anchor placement outside the sync region: this is the
	// position IME candidate windows track.
	b.WriteString(cup(anchorY, anchorX))
	if anchorVisible {
		b.WriteString(showCursor)
	}

	e.prev = f
	return b.Bytes()
}

// Reset drops the remembered frame so the next Frame call repaints all.
func (e *Encoder) Reset() {
	e.prev = nil
}

func cup(row, col int) string {
	return fmt.Sprintf("\x1b[%d;%dH", row+1, col+1)
}

func (e *Encoder) full(f *Frame, b *bytes.Buffer) {
	b.WriteString(cup(0, 0))
	b.WriteString(clearAll)
	e.diffAll(f, b)
	e.prev = nil // will be set by caller
}

func (e *Encoder) diff(f *Frame, b *bytes.Buffer) {
	prev := e.prev
	unchanged := func(x, y int) bool {
		cell := f.Cell(x, y)
		if !prev.Cell(x, y).Equal(cell) {
			return false
		}
		// A changed trailing half also requires rewriting the wide head.
		return cell.Width != 2 || x+1 >= f.W || prev.Cell(x+1, y).Equal(f.Cell(x+1, y))
	}
	for y := 0; y < f.H; y++ {
		x := 0
		for x < f.W {
			if unchanged(x, y) || f.Cell(x, y).Width == 0 {
				x++
				continue
			}
			// Emit the run of changed cells.
			b.WriteString(cup(y, x))
			for x < f.W {
				nc := f.Cell(x, y)
				if unchanged(x, y) {
					break
				}
				if nc.Width == 0 {
					// Placeholder of a wide glyph: its head at x-1 was
					// emitted and already covers this cell.
					x++
					continue
				}
				e.emitCell(b, nc)
				x += max(nc.Width, 1)
			}
		}
	}
}

// diffAll writes every cell (used on the first frame or after resize).
func (e *Encoder) diffAll(f *Frame, b *bytes.Buffer) {
	for y := 0; y < f.H; y++ {
		b.WriteString(cup(y, 0))
		for x := 0; x < f.W; x++ {
			c := f.Cell(x, y)
			if c.Width == 0 {
				continue // covered by the wide cell before it
			}
			e.emitCell(b, c)
		}
	}
}

func (e *Encoder) emitCell(b *bytes.Buffer, c *uv.Cell) {
	if !e.haveSty || !e.curSty.Equal(&c.Style) {
		if c.Style.IsZero() {
			b.WriteString(ansi.ResetStyle)
		} else {
			b.WriteString(ansi.ResetStyle)
			b.WriteString(c.Style.String())
		}
		e.curSty = c.Style
		e.haveSty = true
	}
	content := c.Content
	if content == "" {
		content = " "
	}
	b.WriteString(content)
}
