//go:build !flatcells

package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"unsafe"
)

const ExperimentalFlatScreen = false

type screenBuffer = uv.RenderBuffer

func newScreenBuffer(width, height int) *screenBuffer { return uv.NewRenderBuffer(width, height) }

func (e *Emulator) ScreenStorage() ScreenStorageInfo {
	n := 0
	for _, row := range e.scr.buf.Lines {
		n += cap(row)
	}
	return ScreenStorageInfo{CellBytes: int(unsafe.Sizeof(uv.Cell{})), CellCapacity: n}
}

func (s *Screen) writeScreenLine(y int, line uv.Line) { copy(s.buf.Line(y), line) }
func (s *Screen) pushScreenRows(y, n int)             { s.scrollback.PushN(s.buf, y, n) }
func (s *Screen) isScreenLineEmpty(y int) bool        { return s.isLineEmpty(s.buf.Line(y)) }
func (s *Screen) screenRowMatches(y int, want uv.Line) bool {
	for x, cell := range s.buf.Line(y) {
		if cell != want[x] {
			return false
		}
	}
	return true
}

func projectReflowBuffer(h *reflowHistory, width, height, cut int) (*screenBuffer, []ReflowRow) {
	buf := newScreenBuffer(width, height)
	main := make([]ReflowRow, height)
	for y := 0; y < min(height, h.view.rows); y++ {
		clear(buf.Line(y))
		h.copyRow(buf.Line(y), cut+y)
		_, _, _, main[y], _ = h.view.row(cut + y)
	}
	return buf, main
}

// CopyScreenRow is the compatibility entry; paneGrid uses it only with flatcells.
func (e *Emulator) CopyScreenRow(dst []uv.Cell, x, y int) {
	for i := range dst {
		if c := e.CellAt(x+i, y); c != nil {
			dst[i] = *c
		} else {
			dst[i] = uv.Cell{Content: " ", Width: 1}
		}
	}
}
