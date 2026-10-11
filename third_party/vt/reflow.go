package vt

import (
	"io"

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
		_, _ = io.WriteString(e.pw, ansi.InBandResize(height, width, 0, 0))
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
	var physical []uv.Line
	var flags []ReflowRow
	var logical uv.Line
	cursorOffset := -1
	newCursor := uv.Position{}
	flush := func() {
		start := len(physical)
		row := make(uv.Line, width)
		x := 0
		for i := 0; i < len(logical); {
			cell := logical[i]
			span := max(cell.Width, 1)
			if x+span > width {
				physical = append(physical, row)
				flags = append(flags, ReflowRow{Continued: len(physical)-1 > start, Width: x})
				row = make(uv.Line, width)
				x = 0
			}
			if cursorOffset >= i && cursorOffset < i+span {
				newCursor = uv.Position{X: x + cursorOffset - i, Y: len(physical)}
			}
			copy(row[x:], logical[i:min(i+span, len(logical))])
			x += span
			i += span
		}
		physical = append(physical, row)
		flags = append(flags, ReflowRow{Continued: len(physical)-1 > start, Width: x})
		if cursorOffset == len(logical) {
			newCursor = uv.Position{X: x, Y: len(physical) - 1}
			if x >= width {
				newCursor.X, newCursor.Y = 0, len(physical)
				physical = append(physical, make(uv.Line, width))
				flags = append(flags, ReflowRow{Continued: true})
			}
		}
		logical = nil
		cursorOffset = -1
	}
	for y := 0; y <= history+last; y++ {
		var row uv.Line
		if y < history {
			row = s.scrollback.Line(y)
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
	cut := max(0, len(physical)-height)
	limit := s.scrollback.MaxLines()
	dropped := max(0, cut-limit)
	s.scrollback.Clear()
	for _, row := range physical[dropped:cut] {
		s.scrollback.Push(row)
	}
	s.buf = uv.NewRenderBuffer(width, height)
	for y, row := range physical[cut:] {
		copy(s.buf.Line(y), row)
	}
	s.cur.X, s.cur.Y = newCursor.X, max(0, newCursor.Y-cut)
	s.scroll = s.buf.Bounds()
	dropped = cut - s.scrollback.Len()
	flags = flags[dropped:]
	for len(flags) < s.scrollback.Len()+height {
		flags = append(flags, ReflowRow{})
	}
	return flags
}
