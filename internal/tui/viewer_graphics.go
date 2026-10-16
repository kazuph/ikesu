package tui

func (v *Viewer) GraphicsActive() bool { return !v.app.windowBlurred }

func (v *Viewer) GraphicsGeometry(a *App, pid int) (cols, rows, cellWidth, cellHeight int) {
	v.with(a, func(ui *App) {
		rect := ui.innerRect(pid)
		cols, rows, cellWidth, cellHeight = rect.W, rect.H, ui.cellW, ui.cellH
	})
	return
}
