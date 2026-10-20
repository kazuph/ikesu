package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/layout"
)

// paneMouseCoord converts a host cell coordinate to a pane-inner
// coordinate. In pixel mode the result is the center pixel of the target
// cell, sized from the known cell geometry. Unknown geometry uses cell coordinates.
func paneMouseCoord(inner layout.Rect, pixel bool, cw, ch int, m uv.Mouse) uv.Mouse {
	x, y := m.X-inner.X, m.Y-inner.Y
	if pixel {
		x = x*cw + cw/2
		y = y*ch + ch/2
	}
	return uv.Mouse{X: x, Y: y, Button: m.Button, Mod: m.Mod}
}
