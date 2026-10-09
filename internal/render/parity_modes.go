package render

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func drawModeHint(f *Frame, x, y, w int, hint string) {
	label := " NAVIGATE "
	labelBG := f.Palette.Accent
	tokens := []string{"esc", "up / down", "⇥", "g", "c", "v", "-", "x", "z", "r", "?", "s"}
	switch {
	case strings.HasPrefix(hint, " PREFIX "):
		label, tokens = " PREFIX ", []string{"esc", "ctrl+b", "w", "?"}
	case strings.HasPrefix(hint, " RESIZE "):
		label, tokens, labelBG = " RESIZE ", []string{"h/l", "j/k", "esc"}, f.Palette.Mauve
	case strings.HasPrefix(hint, label):
	default:
		drawCopyHint(f, x, y, w, hint)
		return
	}
	base := uv.Style{Bg: f.Palette.PopupBG}
	f.Fill(x, y, w, 1, " ", base)
	f.Text(x, y, hint, uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}, x+w)
	f.Text(x, y, label, uv.Style{Fg: f.Palette.PopupBG, Bg: labelBG, Attrs: uv.AttrBold}, x+w)
	rest := strings.TrimPrefix(hint, label)
	gap := len(rest) - len(strings.TrimLeft(rest, " "))
	f.Fill(x+ansi.StringWidth(label), y, min(gap, max(0, w-ansi.StringWidth(label))), 1, " ", base)
	for _, token := range tokens {
		// Match the key field, rather than letters in its description.
		at := strings.Index(hint, "  "+token+" ")
		if at >= 0 {
			f.Text(x+ansi.StringWidth(hint[:at+2]), y, token,
				uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, x+w)
		}
	}
}
