package vt

import (
	"io"

	"github.com/charmbracelet/x/ansi"
)

// SetCellPixels supplies actual character geometry for in-band resize reports.
// Zero preserves unknown geometry rather than reporting guessed pixel sizes.
func (e *Emulator) SetCellPixels(width, height int) {
	e.cellWidth, e.cellHeight = max(0, width), max(0, height)
}

func (e *Emulator) reportResize() {
	_, _ = io.WriteString(e.pw, ansi.InBandResize(e.Height(), e.Width(),
		e.Height()*e.cellHeight, e.Width()*e.cellWidth))
}
