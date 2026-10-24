package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// ReflowRow records a physical row's logical continuation and meaningful width.
type ReflowRow struct {
	Continued bool
	Width     int
}

// ResizeWithReflow retains main-screen logical lines when their physical width
// changes. continued describes the caller's recorded automatic wraps across
// main history and visible rows; returned flags describe the new history/rows.
// The alternate screen keeps the clipping behavior of a full-screen terminal.
func (e *Emulator) ResizeWithReflow(width, height int, continued []ReflowRow) []ReflowRow {
	main := &e.scrs[0]
	oldWidth := main.Width()
	flags := main.reflow(width, height, continued, e.scr == main && e.atPhantom)
	pushed, pulled := e.scrs[1].resizeWithoutRestore(width, height)
	if e.cb.Resize != nil {
		e.cb.Resize(true, oldWidth, height, pushed, pulled)
	}
	if e.scr == main {
		e.atPhantom = false
	}
	e.tabstops = uv.DefaultTabStops(width)
	x, y := e.scr.CursorPosition()
	e.setCursor(min(x, width-1), min(y, height-1))
	if e.isModeSet(ansi.ModeInBandResize) {
		e.reportResize()
	}
	return flags
}

func (s *Screen) reflow(width, height int, continued []ReflowRow, phantom bool) []ReflowRow {
	history := s.scrollback.Len()
	cursorRow := history + s.cur.Y
	last := s.cur.Y
	for y := s.Height() - 1; y > last; y-- {
		if !s.isLineEmpty(s.buf.Line(y)) {
			last = y
			break
		}
	}
	previous := s.scrollback
	next := NewScrollback(len(previous.lines))
	if previous.byteLimited {
		next.SetMaxBytes(previous.maxBytes)
	}
	next.maxLines = previous.maxLines
	visible := make([]uv.Line, height)
	for y := range visible {
		visible[y] = make(uv.Line, width)
	}
	output := make(uv.Line, width)
	physical, visibleStart, visibleCount := 0, 0, 0
	emit := func() {
		physical++
		if visibleCount < height {
			idx := (visibleStart + visibleCount) % height
			output, visible[idx] = visible[idx], output
			visibleCount++
		} else {
			next.Push(visible[visibleStart])
			output, visible[visibleStart] = visible[visibleStart], output
			visibleStart = (visibleStart + 1) % height
		}
		clear(output)
	}
	var flags []ReflowRow
	var logical uv.Line
	cursorOffset := -1
	newCursor := uv.Position{}
	flush := func() {
		start := physical
		x := 0
		for i := 0; i < len(logical); {
			cell := logical[i]
			span := max(cell.Width, 1)
			if x+span > width {
				emit()
				flags = append(flags, ReflowRow{Continued: physical-1 > start, Width: x})
				x = 0
			}
			if cursorOffset >= i && cursorOffset < i+span {
				newCursor = uv.Position{X: x + cursorOffset - i, Y: physical}
			}
			copy(output[x:], logical[i:min(i+span, len(logical))])
			x += span
			i += span
		}
		emit()
		flags = append(flags, ReflowRow{Continued: physical-1 > start, Width: x})
		if cursorOffset == len(logical) {
			newCursor = uv.Position{X: x, Y: physical - 1}
			if x >= width {
				newCursor.X, newCursor.Y = 0, physical
				emit()
				flags = append(flags, ReflowRow{Continued: true})
			}
		}
		clear(logical)
		logical = logical[:0]
		cursorOffset = -1
	}
	var scratch uv.Line
	for y := 0; y <= history+last; y++ {
		var row uv.Line
		if y < history {
			scratch = s.scrollback.lineInto(y, scratch)
			row = scratch
		} else {
			row = s.buf.Line(y - history)
		}
		if y > 0 && (y >= len(continued) || !continued[y].Continued) {
			flush()
		}
		end := len(row)
		wraps := y+1 < len(continued) && continued[y+1].Continued
		if wraps {
			end = s.Width()
			if y < len(continued) && continued[y].Width > 0 {
				end = continued[y].Width
			}
		} else {
			for end > 0 && blankCell(&row[end-1]) {
				end--
			}
		}
		if y == cursorRow {
			col := s.cur.X
			if phantom {
				col++
			}
			cursorOffset = len(logical) + col
			end = max(end, col)
		}
		for x := 0; x < end; x++ {
			cell := uv.EmptyCell
			if x < len(row) && row[x].Width > 0 {
				cell = row[x]
			} else if x < len(row) && x > 0 && row[x-1].Width > 1 {
				cell = row[x]
			}
			logical = append(logical, cell)
		}
	}
	flush()
	cut := max(0, physical-height)
	// The old algorithm omitted rows beyond the line limit before applying
	// byte eviction. Retain that notification count while streaming all rows.
	skipped := max(0, cut-previous.MaxLines())
	drops := cut - next.Len() - skipped
	next.OnDrop = previous.OnDrop
	*s.scrollback = *next
	if next.ByteLimited() && next.OnDrop != nil {
		for i := 0; i < drops; i++ {
			next.OnDrop()
		}
	}
	s.scrollRows = nil
	s.buf = uv.NewRenderBuffer(width, height)
	for y := 0; y < visibleCount; y++ {
		copy(s.buf.Line(y), visible[(visibleStart+y)%height])
	}
	s.cur.X, s.cur.Y = newCursor.X, max(0, newCursor.Y-cut)
	s.scroll = s.buf.Bounds()
	dropped := cut - s.scrollback.Len()
	flags = flags[dropped:]
	for len(flags) < s.scrollback.Len()+height {
		flags = append(flags, ReflowRow{})
	}
	return flags
}
