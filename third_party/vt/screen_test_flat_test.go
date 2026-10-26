//go:build flatcells

package vt

import uv "github.com/charmbracelet/ultraviolet"

func screenTestLines(b *screenBuffer) []uv.Line {
	lines := make([]uv.Line, b.Height())
	for y := range lines {
		lines[y] = b.Line(y)
	}
	return lines
}

func screenTestRaw(s *Screen, x, y int, c uv.Cell) { s.buf.setRaw(x, y, c) }
func screenTestNilRow(s *Screen, y int) {
	b := s.buf
	physical := b.physical(y)
	for x := 0; x < b.rowLength(y); x++ {
		i := physical*b.stride + x
		b.tables.release(b.cells[i])
		b.cells[i] = FlatCell{}
	}
	b.lengths[physical] = -1
}
