package vt

import uv "github.com/charmbracelet/ultraviolet"

// scrollOneReusingBlanks shares the history trim scan with row recycling.
// Only an already identical EmptyCell suffix can avoid being filled again;
// zero-width blank cells still need to become one-column spaces.
func (s *Screen) scrollOneReusingBlanks() bool {
	r := s.scroll
	y := s.cur.Y
	if s.scrollback == nil || s.scrollback.maxLines <= 0 || y != r.Min.Y || r.Min.X != 0 || r.Max.X != s.buf.Width() || r.Max.Y > len(s.buf.Lines) || s.cur.Pen.Bg != nil {
		return false
	}
	if uv.EmptyCell.Content != " " || uv.EmptyCell.Width != 1 || !blankCell(&uv.EmptyCell) {
		return false
	}
	line := s.buf.Lines[y]
	last := -1
	clearEnd := 0
	scanEnd := len(line)
	if s.runtimeASCII && len(line) > 0 {
		scanEnd = s.runtimeBlankBounds[s.runtimeRowHead].end
	}
	for i := scanEnd - 1; i >= 0; i-- {
		if !blankCell(&line[i]) {
			last = i
			break
		}
		if line[i].Width == 0 && clearEnd == 0 {
			clearEnd = i + 1
		}
	}
	end := last + 1
	if last >= 0 {
		end = min(last+max(line[last].Width, 1), len(line))
	}
	clearEnd = max(clearEnd, end)
	dropped := false
	if line != nil {
		if s.runtimeASCII && s.scrollback.byteLimited && len(line) > 0 {
			dropped = s.scrollback.pushBytesWithPlain(line[:end], s.runtimeBlankBounds[s.runtimeRowHead].plain)
		} else {
			dropped = s.scrollback.pushTrimmed(line[:end])
		}
	}
	if dropped {
		s.scrollRows = s.scrollRows[:0]
		if s.cur.Pen.Bg == nil && uv.EmptyCell == (uv.Cell{Content: " ", Width: 1}) && r.Max.Y <= len(s.buf.Lines) && y < len(s.buf.Lines) && r.Max.X >= s.buf.Width() {
			line = s.buf.Lines[y]
			if len(s.blankLine) != len(line) {
				s.blankLine = make(uv.Line, len(line))
				for x := range s.blankLine {
					s.blankLine[x] = uv.Cell{Content: " ", Width: 1}
				}
			}
			s.rotateScrollRows(y, r, line)
			copy(line, s.blankLine)
		} else {
			s.buf.Buffer.DeleteLineArea(y, 1, s.blankCell(), r)
		}
	} else {
		s.rotateScrollRows(y, r, line)
		if clearEnd > 0 && s.runtimeASCII && s.runtimeBlankBounds[s.runtimeRowHead].plain {
			for x := range line[:clearEnd] {
				line[x].Content = " "
			}
		} else if clearEnd > 0 {
			if len(s.blankLine) != len(line) {
				s.blankLine = make(uv.Line, len(line))
				for x := range s.blankLine {
					s.blankLine[x] = uv.Cell{Content: " ", Width: 1}
				}
			}
			copy(line[:clearEnd], s.blankLine[:clearEnd])
		}
	}
	if s.runtimeASCII && len(line) > 0 {
		s.runtimeBlankBounds[s.runtimeRowHead] = runtimeRowState{plain: true}
		s.runtimeRowHead++
		if s.runtimeRowHead == len(s.runtimeBlankBounds) {
			s.runtimeRowHead = 0
		}
	}
	// The first touch grows the bookkeeping for every row once.
	s.buf.TouchLine(r.Min.X, r.Min.Y, r.Max.X-r.Min.X)
	start, endRow := r.Min.Y+1, min(r.Max.Y+1, len(s.buf.Lines))
	if start < endRow {
		for offset, ch := range s.buf.Touched[start:endRow] {
			row := start + offset
			if ch == nil {
				s.buf.Touched[row] = &uv.LineData{FirstCell: r.Min.X, LastCell: r.Max.X}
			} else {
				ch.FirstCell = min(ch.FirstCell, r.Min.X)
				ch.LastCell = max(ch.LastCell, r.Max.X)
			}
		}
	}
	if s.cb.Scroll != nil {
		s.cb.Scroll(r.Min.X, y, r.Max.X, r.Max.Y, -1)
	}
	return true
}
