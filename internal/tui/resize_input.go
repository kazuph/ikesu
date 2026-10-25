package tui

import "github.com/kazuph/ikesu/internal/input"

func (a *App) resizeDragActive() bool {
	return a.navigator == nil && a.settings == nil && a.help == nil && a.dialog == nil && a.menu == nil && a.cm == nil && a.sidebarBar == nil && a.tabGesture == nil &&
		(a.sep != nil || a.sidebarGesture != nil && a.sidebarGesture.divider)
}

func (a *App) resizeMotion(ev input.Event) bool {
	if ev.Kind != input.KindMouseMotion || !a.resizeDragActive() {
		return false
	}
	x, y := ev.Mouse.X, ev.Mouse.Y
	if a.hostPixelMouse {
		x /= a.cellW
		y /= a.cellH
	}
	return ev.Mouse.X >= 0 && ev.Mouse.Y >= 0 && x < a.st.Cols && y < a.st.Rows
}

// Only already-decoded consecutive motions of our active resize gesture can
// replace each other. Every other event remains an ordering boundary.
func (a *App) dispatchInputEvents(events []input.Event, deliver func(input.Event)) {
	for i := 0; i < len(events); i++ {
		ev := events[i]
		if a.resizeMotion(ev) {
			for i+1 < len(events) && a.resizeMotion(events[i+1]) {
				if g := a.sidebarGesture; g != nil && g.divider {
					x, y := ev.Mouse.X, ev.Mouse.Y
					if a.hostPixelMouse {
						x /= a.cellW
						y /= a.cellH
					}
					g.moved = g.moved || x != g.x || y != g.y
				}
				i++
				ev = events[i]
			}
		}
		deliver(ev)
	}
}

// Size ownership and the resulting drag position are applied together. The
// event is processed immediately; this scope creates no timer or queue.
func (a *App) resizeInput(ev input.Event, deliver func()) {
	if !a.resizeDragActive() || ev.Kind != input.KindMouseMotion && ev.Kind != input.KindMouseRelease {
		deliver()
		return
	}
	a.deferResize = true
	defer func() {
		a.deferResize = false
		if a.resizePending {
			a.resizePending = false
			a.syncLayout()
		}
	}()
	deliver()
}
