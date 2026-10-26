//go:build !flatcells

package vt

import uv "github.com/charmbracelet/ultraviolet"

// rotateScrollRows preserves cell storage while advancing a full-screen row view.
// Header-changing operations invalidate the view but retain its allocation.
func (s *Screen) rotateScrollRows(y int, r uv.Rectangle, line uv.Line) {
	lines := s.buf.Lines
	h := len(lines)
	if y != 0 || r.Max.Y != h || r.Min.X != 0 || r.Max.X != s.buf.Width() {
		s.scrollRows = s.scrollRows[:0]
		copy(lines[y:r.Max.Y-1], lines[y+1:r.Max.Y])
		lines[r.Max.Y-1] = line
		return
	}
	if len(s.scrollRows) != 2*h || &lines[0] != &s.scrollRows[s.scrollRowHead] {
		rows := s.scrollRows
		if cap(rows) != 2*h {
			rows = make(uv.Lines, 2*h)
		} else {
			rows = rows[:2*h]
		}
		copy(rows, lines)
		// lines may overlap rows, so duplicate the already copied first half.
		copy(rows[h:], rows[:h])
		s.scrollRows = rows
		s.scrollRowHead = 0
	}
	s.scrollRowHead++
	if s.scrollRowHead == h {
		s.scrollRowHead = 0
	}
	head := s.scrollRowHead
	s.buf.Lines = s.scrollRows[head : head+h : head+h]
}
