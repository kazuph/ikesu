//go:build flatcells

package vt

import (
	"bytes"
	uv "github.com/charmbracelet/ultraviolet"
)

func flatBlank(c FlatCell) bool {
	return c.Style == 0 && c.Link == 0 && (c.Width == 1 && c.Text == ' '+1 || c.Width == 0 && c.Text == 0)
}

func (s *Screen) scrollOneReusingBlanks() bool {
	r, y := s.scroll, s.cur.Y
	if s.scrollback == nil || s.scrollback.maxLines <= 0 || y != r.Min.Y || r.Min.X != 0 || r.Max.X != s.buf.Width() || r.Max.Y > s.buf.Height() || s.cur.Pen.Bg != nil {
		return false
	}
	if uv.EmptyCell != (uv.Cell{Content: " ", Width: 1}) {
		return false
	}
	length := s.buf.rowLength(y)
	scanEnd := max(length, 0)
	if s.runtimeASCII && length > 0 {
		scanEnd = s.runtimeBlankBounds[s.runtimeRowHead].end
	}
	last := -1
	var plainText []byte
	if s.runtimeASCII && length > 0 {
		state := s.runtimeBlankBounds[s.runtimeRowHead]
		if state.plain && state.text != nil && len(state.text) == scanEnd {
			plainText = bytes.TrimRight(state.text, " ")
		}
	}
	if plainText != nil {
		last = len(plainText) - 1
	} else {
		for x := scanEnd - 1; x >= 0; x-- {
			if !flatBlank(s.buf.raw(x, y)) {
				last = x
				break
			}
		}
	}
	end := last + 1
	if last >= 0 {
		end = min(last+max(s.buf.raw(last, y).Width, 1), max(length, 0))
	}
	dropped := false
	if length >= 0 {
		line := make(uv.Line, end)
		s.buf.copyRow(line, 0, y)
		if s.runtimeASCII && s.scrollback.byteLimited && length > 0 {
			dropped = s.scrollback.pushBytesWithPlainText(line, s.runtimeBlankBounds[s.runtimeRowHead].plain, plainText)
		} else {
			dropped = s.scrollback.pushTrimmed(line)
		}
	}
	if dropped && !(s.cur.Pen.Bg == nil && uv.EmptyCell == (uv.Cell{Content: " ", Width: 1}) && r.Max.Y <= s.buf.Height() && y < s.buf.Height() && r.Max.X >= s.buf.Width()) {
		s.buf.deleteLines(y, 1, s.blankCell(), r)
	} else {
		s.buf.rotateRows(y, r.Max.Y, 1)
		s.buf.fillRows(r.Max.Y-1, r.Max.Y, nil)
	}
	if s.runtimeASCII && length > 0 {
		s.runtimeBlankBounds[s.runtimeRowHead] = runtimeRowState{plain: true}
		s.runtimeRowHead = (s.runtimeRowHead + 1) % len(s.runtimeBlankBounds)
	}
	if !s.runtimeASCII || !s.runtimeScrollTouched {
		for row := r.Min.Y; row < min(r.Max.Y+1, s.buf.Height()); row++ {
			s.buf.TouchLine(r.Min.X, row, r.Dx())
		}
		if s.runtimeASCII {
			s.runtimeScrollTouched = true
		}
	}
	if s.cb.Scroll != nil {
		s.cb.Scroll(r.Min.X, y, r.Max.X, r.Max.Y, -1)
	}
	return true
}
