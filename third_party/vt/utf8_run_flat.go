//go:build flatcells

package vt

import uv "github.com/charmbracelet/ultraviolet"

func (b *flatScreenBuffer) writeASCII(x, y int, data []byte, pen uv.Style, link uv.Link) {
	cell := b.tables.pack(uv.Cell{Width: 1, Style: pen, Link: link})
	first, last := len(data), 0
	for i, value := range data {
		cell.Text = uint32(value) + 1
		old := b.raw(x+i, y)
		if old != cell {
			first = min(first, i)
			last = i + 1
			if cell.Style == 0 && cell.Link == 0 && old.Style == 0 && old.Link == 0 && old.Width == 1 {
				if old.Text&flatIndirect != 0 {
					b.tables.text.release(old.Text &^ flatIndirect)
				}
				b.cells[b.physical(y)*b.stride+x+i].Text = cell.Text
			} else {
				b.assign(x+i, y, cell)
			}
		}
	}
	if first < last {
		b.TouchLine(x+first, y, last-first)
	}
	cell.Text = 0
	b.tables.release(cell)
}

func (e *Emulator) printRun(run []byte) {
	if len(e.grapheme) == 1 {
		value := e.grapheme[0]
		e.grapheme = e.grapheme[:0]
		e.handleGrapheme(asciiContent[value], 1)
	}
	pending := run[len(run)-1]
	run = run[:len(run)-1]
	awm := e.autoWrap
	pen, link := e.scr.cursorPen(), e.scr.cursorLink()
	width := e.scr.buf.Width()
	finishRegular := func(i int) {
		for ; i < len(run); i++ {
			e.handlePrint(rune(run[i]))
		}
		e.handlePrint(rune(pending))
	}
	for i := 0; i < len(run); {
		x, y := e.scr.CursorPosition()
		if e.atPhantom {
			if awm {
				e.index()
				_, y = e.scr.CursorPosition()
				x = 0
			}
			e.atPhantom = false
		}
		n := min(len(run)-i, width-x)
		fast := e.scr.buf.rowLength(y) >= x+n && x >= 0
		for k := 0; fast && k < n; k++ {
			if e.scr.buf.raw(x+k, y).Width != 1 {
				fast = false
			}
		}
		if !fast || width <= 0 {
			finishRegular(i)
			return
		}
		e.scr.buf.writeASCII(x, y, run[i:i+n], pen, link)
		i += n
		x += n
		if x >= width {
			if awm {
				e.atPhantom = true
			} else if i < len(run) {
				e.scr.buf.writeASCII(x-1, y, run[len(run)-1:], pen, link)
				i = len(run)
			}
			x = width - 1
		}
		e.scr.setCursor(x, y, false)
	}
	e.grapheme = append(e.grapheme[:0], pending)
}
