package vt

import (
	"slices"

	uv "github.com/charmbracelet/ultraviolet"
)

// DefaultScrollbackSize is the default maximum number of lines in the scrollback buffer.
const DefaultScrollbackSize = 10000

// Scrollback represents a scrollback buffer that stores lines scrolled off the screen.
//
// The lines are kept in a ring buffer: appending a line is O(1) even when the
// buffer is full, where the previous slice-of-lines design shifted every
// stored line on each push.
type Scrollback struct {
	lines           []uv.Line // ring buffer, capacity maxLines
	maxLines        int
	start           int // index of the oldest line
	count           int // number of stored lines
	byteLimited     bool
	maxBytes, bytes int
	rowBytes        []int
	packed          [][]byte
	styled          []*styledRow
	indexed         *reflowHistory
	prepared        *reflowPrepared
	OnDrop          func()
}

// NewScrollback creates a new scrollback buffer with the given maximum number of lines.
func NewScrollback(maxLines int) *Scrollback {
	if maxLines <= 0 {
		maxLines = DefaultScrollbackSize
	}
	return &Scrollback{
		lines:    make([]uv.Line, maxLines),
		maxLines: maxLines,
	}
}

// wrap maps a logical position onto the ring's backing array. Positions never
// exceed start+count < 2*maxLines, so one subtraction is enough.
func (s *Scrollback) wrap(i int) int {
	if i >= len(s.lines) {
		i -= len(s.lines)
	}
	return i
}

// blankCell reports whether c is a plain blank: the zero cell or
// [uv.EmptyCell] (" ", width 1, no style, no link). It is the field-by-field
// equivalent of `*c == uv.Cell{} || *c == uv.EmptyCell`, written out because
// the trailing-space scan in [Scrollback.Push] hits it for almost every cell
// of every scrolled line and a whole-struct comparison is measurably slower.
func blankCell(c *uv.Cell) bool {
	if !((c.Width == 1 && c.Content == " ") || (c.Width == 0 && c.Content == "")) {
		return false
	}
	st := &c.Style
	return int(st.Attrs)|int(st.Underline)|len(c.Link.URL)|len(c.Link.Params) == 0 && st.Fg == nil && st.Bg == nil && st.UnderlineColor == nil
}

// Push adds a line to the scrollback buffer.
// If the buffer is full, the oldest line is removed.
func (s *Scrollback) Push(line uv.Line) {
	if s == nil || s.maxLines <= 0 {
		return
	}

	// Find last non-empty cell to trim trailing empty cells.
	// This helps with wrapping and window resizing.
	lastNonEmpty := -1
	for i := len(line) - 1; i >= 0; i-- {
		if !blankCell(&line[i]) {
			lastNonEmpty = i
			break
		}
	}

	// Keep the line content up to and including the last non-empty cell.
	end := lastNonEmpty + 1
	if lastNonEmpty >= 0 {
		// Keep a wide glyph's continuation cells so a later blank cannot
		// overwrite its head when the saved row is rendered again.
		end = min(lastNonEmpty+max(line[lastNonEmpty].Width, 1), len(line))
	}

	s.pushTrimmed(line[:end])
}

// pushTrimmed reports whether an eviction callback ran.
func (s *Scrollback) pushTrimmed(line uv.Line) bool {
	end := len(line)
	if s.byteLimited {
		return s.pushBytes(line[:end])
	}
	if s.indexedLen() > 0 && s.Len() >= s.maxLines {
		callback := s.OnDrop
		s.OnDrop = nil
		s.dropOldest()
		s.OnDrop = callback
	}
	if s.count < s.maxLines {
		s.lines[s.wrap(s.start+s.count)] = slices.Clone(line[:end])
		s.count++
		return false
	}

	// The buffer is full: reuse the evicted line's backing array for the new
	// copy instead of allocating a fresh one for every pushed line.
	if s.prepared != nil {
		s.prepared.dropFirst()
	}
	idx := s.start
	if dst := s.lines[idx]; cap(dst) >= end {
		s.lines[idx] = append(dst[:0], line[:end]...)
	} else {
		s.lines[idx] = slices.Clone(line[:end])
	}
	s.start = s.wrap(s.start + 1)
	return false
}

// PushN adds n lines from the buffer starting at line y to the scrollback.
func (s *Scrollback) PushN(buf *uv.RenderBuffer, y, n int) {
	if s == nil || buf == nil || n <= 0 {
		return
	}

	for i := range min(n, buf.Height()-y) {
		if line := buf.Line(y + i); line != nil {
			s.Push(line)
		}
	}
}

// popLast transfers the newest saved row back to a growing screen.
func (s *Scrollback) popLast() uv.Line {
	if s == nil || s.Len() == 0 {
		return nil
	}
	if s.count == 0 {
		line, charge := s.indexed.popLast()
		if s.byteLimited {
			s.bytes -= charge
		}
		return line
	}
	if s.prepared != nil {
		s.prepared.popLast(s.count)
	}
	idx := s.wrap(s.start + s.count - 1)
	line := s.lines[idx]
	if len(s.styled) > idx && s.styled[idx] != nil {
		line = s.styled[idx].line()
		s.styled[idx] = nil
	}
	if len(s.packed) > idx && s.packed[idx] != nil {
		line = unpackRow(s.packed[idx])
		s.packed[idx] = nil
	}
	s.lines[idx] = nil
	s.count--
	if s.byteLimited {
		s.bytes -= s.rowBytes[idx]
		s.rowBytes[idx] = 0
	}
	return line
}

// Len returns the number of lines in the scrollback buffer.
func (s *Scrollback) Len() int {
	if s == nil {
		return 0
	}
	return s.count + s.indexedLen()
}

func (s *Scrollback) indexedLen() int {
	if s.indexed == nil {
		return 0
	}
	return s.indexed.view.rows
}

// MaxLines returns the maximum number of lines the scrollback buffer can hold.
func (s *Scrollback) MaxLines() int {
	if s == nil {
		return 0
	}
	return s.maxLines
}

// SetMaxLines sets the maximum number of lines in the scrollback buffer.
// If the current number of lines exceeds the new maximum, oldest lines are removed.
func (s *Scrollback) SetMaxLines(maxLines int) {
	if s == nil || maxLines <= 0 {
		return
	}

	callback := s.OnDrop
	if !s.byteLimited {
		s.OnDrop = nil
	}
	for s.Len() > maxLines {
		s.dropOldest()
	}
	if s.prepared != nil {
		s.prepared.applyDrops()
	}
	s.OnDrop = callback
	s.maxLines = maxLines
	if len(s.lines) != maxLines {
		// Rebuild the ring at the new capacity, unwrapping into order.
		lines := make([]uv.Line, maxLines)
		var packed [][]byte
		var styled []*styledRow
		var sizes []int
		if s.byteLimited {
			packed = make([][]byte, maxLines)
			styled = make([]*styledRow, maxLines)
			sizes = make([]int, maxLines)
		}
		for i := 0; i < s.count; i++ {
			idx := s.wrap(s.start + i)
			lines[i] = s.lines[idx]
			if s.byteLimited {
				packed[i], sizes[i], styled[i] = s.packed[idx], s.rowBytes[idx], s.styled[idx]
			}
		}
		s.lines = lines
		if s.byteLimited {
			s.packed, s.rowBytes, s.styled = packed, sizes, styled
		}
		s.start = 0
	}
}

// Line returns the line at the given index.
// Index 0 is the oldest line, Len()-1 is the most recent.
// Returns nil if index is out of bounds.
func (s *Scrollback) Line(index int) uv.Line {
	if s == nil || index < 0 || index >= s.Len() {
		return nil
	}
	if index < s.indexedLen() {
		line := s.indexed.line(index)
		if len(line) == 0 && !s.byteLimited {
			return uv.Line{}
		}
		return line
	}
	index -= s.indexedLen()
	idx := s.wrap(s.start + index)
	if len(s.styled) > idx && s.styled[idx] != nil {
		return s.styled[idx].line()
	}
	if len(s.packed) > idx && s.packed[idx] != nil {
		return unpackRow(s.packed[idx])
	}
	return s.lines[idx]
}

// lineInto reconstructs a saved row without allocating a fresh expanded row
// for each history entry. The caller owns dst; saved storage stays immutable.
func (s *Scrollback) lineInto(index int, dst uv.Line) uv.Line {
	if index < s.indexedLen() {
		return s.indexed.lineInto(index, dst)
	}
	index -= s.indexedLen()
	idx := s.wrap(s.start + index)
	var packed []byte
	var styled *styledRow
	width := len(s.lines[idx])
	if len(s.packed) > idx && s.packed[idx] != nil {
		packed = s.packed[idx]
		width = len(packed)
	}
	if len(s.styled) > idx && s.styled[idx] != nil {
		styled = s.styled[idx]
		width = len(styled.glyphs)
	}
	if cap(dst) < width {
		dst = make(uv.Line, width)
	} else {
		clear(dst)
		dst = dst[:width]
	}
	switch {
	case packed != nil:
		for x, b := range packed {
			dst[x] = uv.Cell{Content: asciiContent[b], Width: 1}
		}
	case styled != nil:
		start := 0
		for _, run := range styled.runs {
			for x := start; x < run.end; x++ {
				g := styled.glyphs[x]
				dst[x] = uv.Cell{Content: g.content, Width: g.width, Style: run.style, Link: run.link}
			}
			start = run.end
		}
	default:
		copy(dst, s.lines[idx])
	}
	return dst
}

// Lines returns all lines in the scrollback buffer.
// Index 0 is the oldest line.
func (s *Scrollback) Lines() []uv.Line {
	if s == nil {
		return nil
	}
	if s.Len() == 0 {
		return nil
	}
	if !s.byteLimited && s.start == 0 && s.indexedLen() == 0 {
		return s.lines[:s.count]
	}
	// The ring is wrapped; return the lines in order on a fresh slice.
	out := make([]uv.Line, s.Len())
	for i := range out {
		out[i] = s.Line(i)
	}
	return out
}

// Clear removes all lines from the scrollback buffer.
func (s *Scrollback) Clear() {
	if s == nil {
		return
	}
	clear(s.lines)
	clear(s.rowBytes)
	clear(s.packed)
	clear(s.styled)
	s.start = 0
	s.count = 0
	s.bytes = 0
	s.indexed = nil
	s.prepared = nil
}

// CellAt returns the cell at the given position in the scrollback buffer.
// x is the column, y is the line index (0 = oldest).
// Returns nil if position is out of bounds.
func (s *Scrollback) CellAt(x, y int) *uv.Cell {
	if s == nil || y < 0 || y >= s.Len() {
		return nil
	}
	if y < s.indexedLen() {
		return s.indexed.cellAt(x, y)
	}
	y -= s.indexedLen()
	idx := s.wrap(s.start + y)
	if len(s.styled) > idx && s.styled[idx] != nil {
		return s.styled[idx].cellAt(x)
	}
	if len(s.packed) > idx && s.packed[idx] != nil {
		if x < 0 || x >= len(s.packed[idx]) {
			return nil
		}
		return &uv.Cell{Content: string(s.packed[idx][x]), Width: 1}
	}
	line := s.lines[idx]
	if line == nil || x < 0 || x >= len(line) {
		return nil
	}
	return &line[x]
}
