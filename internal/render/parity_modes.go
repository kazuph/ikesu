package render

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func drawModeHint(f *Frame, x, y, w int, hint string) {
	if !strings.HasPrefix(hint, " NAVIGATE ") {
		drawCopyHint(f, x, y, w, hint)
		return
	}
	base := uv.Style{Bg: f.Palette.PopupBG}
	f.Fill(x, y, w, 1, " ", base)
	f.Text(x, y, hint, uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}, x+w)
	f.Text(x, y, " NAVIGATE ", uv.Style{Fg: f.Palette.PopupBG, Bg: f.Palette.Accent, Attrs: uv.AttrBold}, x+w)
	f.Set(x+ansi.StringWidth(" NAVIGATE "), y, " ", base)
	for _, token := range []string{"esc", "up / down", "⇥", "g", "c", "v", "-", "x", "z", "r", "?", "s"} {
		// Match the key field, rather than letters in its description.
		at := strings.Index(hint, "  "+token+" ")
		if at >= 0 {
			f.Text(x+ansi.StringWidth(hint[:at+2]), y, token,
				uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, x+w)
		}
	}
}
