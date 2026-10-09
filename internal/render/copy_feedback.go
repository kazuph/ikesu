package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/layout"
)

// Recorded feedback has one text row, a dot and gap, and a border on each side.
func drawCopyFeedback(f *Frame, area layout.Rect, text string) {
	width := min(area.W, ansi.StringWidth(text)+4)
	if width < 4 || area.H < 3 {
		return
	}
	x, y := area.X+(area.W-width)/2, area.Y+area.H-3
	border := uv.Style{Fg: f.Palette.Green, Bg: f.Palette.PopupBG}
	ix, iy, iw, _ := f.Box(x, y, width, 3, border)
	f.Fill(ix, iy, iw, 1, " ", uv.Style{Bg: f.Palette.PopupBG})
	f.Set(ix, iy, "●", border)
	f.Text(ix+2, iy, text, uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, ix+iw)
}
