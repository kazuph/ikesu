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
	lines    []uv.Line // ring buffer, capacity maxLines
	maxLines int
	start    int // index of the oldest line
	count    int // number of stored lines
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
	if c.Content != "" && c.Content != " " {
		return false
	}
	st := &c.Style
	if st.Fg != nil || st.Bg != nil || st.UnderlineColor != nil ||
		st.Underline != uv.UnderlineNone || st.Attrs != 0 {
		return false
	}
	if c.Link.URL != "" || c.Link.Params != "" {
		return false
	}
	if c.Content == "" {
		return c.Width == 0
	}
	return c.Width == 1
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

	if s.count < s.maxLines {
		s.lines[s.wrap(s.start+s.count)] = slices.Clone(line[:end])
		s.count++
		return
	}

	// The buffer is full: reuse the evicted line's backing array for the new
	// copy instead of allocating a fresh one for every pushed line.
	idx := s.start
	if dst := s.lines[idx]; cap(dst) >= end {
		s.lines[idx] = append(dst[:0], line[:end]...)
	} else {
		s.lines[idx] = slices.Clone(line[:end])
	}
	s.start = s.wrap(s.start + 1)
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

// Len returns the number of lines in the scrollback buffer.
func (s *Scrollback) Len() int {
	if s == nil {
		return 0
	}
	return s.count
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

	if s.count > maxLines {
		s.start = s.wrap(s.start + s.count - maxLines)
		s.count = maxLines
	}
	s.maxLines = maxLines
	if len(s.lines) != maxLines {
		// Rebuild the ring at the new capacity, unwrapping into order.
		lines := make([]uv.Line, maxLines)
		for i := 0; i < s.count; i++ {
			lines[i] = s.lines[s.wrap(s.start+i)]
		}
		s.lines = lines
		s.start = 0
	}
}

// Line returns the line at the given index.
// Index 0 is the oldest line, Len()-1 is the most recent.
// Returns nil if index is out of bounds.
func (s *Scrollback) Line(index int) uv.Line {
	if s == nil || index < 0 || index >= s.count {
		return nil
	}
	return s.lines[s.wrap(s.start+index)]
}

// Lines returns all lines in the scrollback buffer.
// Index 0 is the oldest line.
func (s *Scrollback) Lines() []uv.Line {
	if s == nil {
		return nil
	}
	if s.count == 0 {
		return nil
	}
	if s.start == 0 {
		return s.lines[:s.count]
	}
	// The ring is wrapped; return the lines in order on a fresh slice.
	out := make([]uv.Line, s.count)
	n := copy(out, s.lines[s.start:])
	copy(out[n:], s.lines[:s.count-n])
	return out
}

// Clear removes all lines from the scrollback buffer.
func (s *Scrollback) Clear() {
	if s == nil {
		return
	}
	s.start = 0
	s.count = 0
}

// CellAt returns the cell at the given position in the scrollback buffer.
// x is the column, y is the line index (0 = oldest).
// Returns nil if position is out of bounds.
func (s *Scrollback) CellAt(x, y int) *uv.Cell {
	line := s.Line(y)
	if line == nil || x < 0 || x >= len(line) {
		return nil
	}
	return &line[x]
}
