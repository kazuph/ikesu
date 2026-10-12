package vt

// Graphics placement follows kitty's cursor policy; movement beyond the
// scroll area uses the terminal's existing index operation.
func (e *Emulator) AdvanceImageCursor(cols, rows int) {
	x, y := e.scr.CursorPosition()
	for i := 0; i < rows; i++ {
		e.index()
	}
	_, y = e.scr.CursorPosition()
	e.setCursor(min(x+cols, e.Width()-1), y)
}
