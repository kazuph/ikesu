//go:build flatcells

package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"unsafe"
)

const ExperimentalFlatScreen = true

type screenBuffer = flatScreenBuffer

func newScreenBuffer(width, height int) *screenBuffer { return newFlatScreenBuffer(width, height) }

func flatIDArrayBytes[K comparable](p *flatIDs[K]) int {
	var value K
	return cap(p.values)*int(unsafe.Sizeof(value)) + cap(p.refs)*int(unsafe.Sizeof(int(0))) + cap(p.free)*int(unsafe.Sizeof(uint32(0)))
}

func (e *Emulator) ScreenStorage() ScreenStorageInfo {
	b := e.scr.buf
	t := &b.tables
	arrays := flatIDArrayBytes(&t.text) + flatIDArrayBytes(&t.styles) + flatIDArrayBytes(&t.links) + cap(t.decoded)*int(unsafe.Sizeof(uv.Style{})) + cap(t.colors)*int(unsafe.Sizeof(flatColorSlot{})) + cap(t.free)*int(unsafe.Sizeof(uint32(0)))
	return ScreenStorageInfo{CellBytes: int(unsafe.Sizeof(FlatCell{})), CellCapacity: cap(b.cells), IDArrayBytes: arrays, LiveIDs: t.text.live + t.styles.live + t.links.live + t.live, PointerFree: true}
}

func (s *Screen) writeScreenLine(y int, line uv.Line) { s.buf.putLine(y, line) }
func (s *Screen) pushScreenRows(y, n int) {
	for i := range min(n, s.buf.Height()-y) {
		if line := s.buf.Line(y + i); line != nil {
			s.scrollback.Push(line)
		}
	}
}
func (s *Screen) isScreenLineEmpty(y int) bool {
	for x := 0; x < s.buf.rowLength(y); x++ {
		c := s.buf.raw(x, y)
		if c.Text != 0 && c.Text != ' '+1 {
			return false
		}
	}
	return true
}
func (s *Screen) screenRowMatches(y int, want uv.Line) bool {
	for x := 0; x < s.buf.rowLength(y); x++ {
		c := s.buf.tables.pack(want[x])
		same := c == s.buf.raw(x, y)
		s.buf.tables.release(c)
		if !same {
			return false
		}
	}
	return true
}

func projectReflowBuffer(h *reflowHistory, width, height, cut int) (*screenBuffer, []ReflowRow) {
	buf := newScreenBuffer(width, height)
	main := make([]ReflowRow, height)
	row := make(uv.Line, width)
	for y := 0; y < min(height, h.view.rows); y++ {
		clear(row)
		h.copyRow(row, cut+y)
		buf.putLine(y, row)
		_, _, _, main[y], _ = h.view.row(cut + y)
	}
	return buf, main
}

// CopyScreenRow reads the canonical flat cells while the caller owns the emulator.
// The destination owns its decoded values and cannot retain process-local IDs.
func (e *Emulator) CopyScreenRow(dst []uv.Cell, x, y int) { e.scr.buf.copyRow(dst, x, y) }
