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
	return e.resizeWithReflowView(width, height, func(y int) ReflowRow {
		if y >= 0 && y < len(continued) {
			return continued[y]
		}
		return ReflowRow{}
	}, false).materialize()
}

// ResizeWithReflowView leaves history metadata indexed instead of allocating
// one result entry for each retained physical row. The caller preserves the
// continuation metadata of retained history; new output and main rows are
// supplied through continued. The slice API permits arbitrary replacement.
func (e *Emulator) ResizeWithReflowView(width, height int, continued func(int) ReflowRow) ReflowSnapshot {
	return e.resizeWithReflowView(width, height, continued, true)
}

func (e *Emulator) resizeWithReflowView(width, height int, continued func(int) ReflowRow, stableHistory bool) ReflowSnapshot {
	main := &e.scrs[0]
	oldWidth := main.Width()
	snapshot := main.reflowIndexed(width, height, continued, e.scr == main && e.atPhantom, stableHistory)
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
	return snapshot
}

func (s *Screen) reflow(width, height int, continued []ReflowRow, phantom bool) []ReflowRow {
	return s.reflowIndexed(width, height, func(y int) ReflowRow {
		if y >= 0 && y < len(continued) {
			return continued[y]
		}
		return ReflowRow{}
	}, phantom, false).materialize()
}
