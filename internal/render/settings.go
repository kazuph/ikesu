package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"image/color"
)

// Settings geometry and options follow the recorded distribution screens.
var ThemeNames = config.ThemeNames
var ToastNames = []string{"off", "gtx", "terminal", "system"}

type Settings struct {
	Section, Selected int
	Theme             string
	Sound             bool
	Toast             string
}

func SettingsRect(cols, rows int) layout.Rect {
	// Recorded narrow and short viewports retain two columns and one row per side.
	w, h := min(76, max(0, cols-4)), min(22, max(0, rows-2))
	return layout.Rect{X: (cols - w) / 2, Y: (rows - h) / 2, W: w, H: h}
}
func SettingsChoiceY(r layout.Rect, section, index int) int {
	if section == 0 {
		return r.Y + 5 + index
	}
	if section == 1 {
		return r.Y + 8 + index
	}
	return r.Y + 8 + 2*index
}
func drawSettings(f *Frame, s *Settings) {
	r := SettingsRect(f.W, f.H)
	f.Fill(r.X, r.Y, r.W, r.H, " ", uv.Style{Bg: f.Palette.PopupBG})
	f.Box(r.X, r.Y, r.W, r.H, uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG})
	f.Text(r.X+1, r.Y+1, " settings", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, r.X+r.W-1)
	f.Fill(r.X+1, r.Y+2, r.W-2, 1, " ", uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.PopupBG})
	x := r.X + 2
	for i, label := range []string{"theme", "sound", "toasts"} {
		style := uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.PopupBG}
		if s.Section == i {
			style.Fg = f.Palette.SurfDim
			style.Bg = f.Palette.Accent
			style.Attrs = uv.AttrBold
		}
		x = f.Text(x, r.Y+2, label, style, r.X+r.W-1) + 3
	}
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		f.Set(x, r.Y+3, "─", uv.Style{Fg: f.Palette.Surface, Bg: f.Palette.PopupBG})
	}
	names := ThemeNames
	current := s.Theme
	switch s.Section {
	case 1:
		f.Fill(r.X+1, r.Y+5, r.W-2, 2, " ", uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.PopupBG})
		f.Text(r.X+2, r.Y+5, "play sounds when agents change state in background", uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.PopupBG}, r.X+r.W-1)
		names = []string{"sound alerts: on", "sound alerts: off"}
		current = names[1]
		if s.Sound {
			current = names[0]
		}
	case 2:
		f.Fill(r.X+1, r.Y+5, r.W-2, 2, " ", uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.PopupBG})
		f.Text(r.X+2, r.Y+5, "choose where background popup notifications should appear", uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.PopupBG}, r.X+r.W-1)
		names = []string{"notification popups: off", "notification popups: inside gtx", "notification popups: via terminal", "notification popups: via system"}
		for i, n := range ToastNames {
			if s.Toast == n {
				current = names[i]
			}
		}
	}
	for i, name := range names {
		y := SettingsChoiceY(r, s.Section, i)
		if y >= r.Y+r.H-3 {
			break
		}
		label := name
		style := uv.Style{Fg: f.Palette.Subtle, Bg: f.Palette.PopupBG}
		if s.Section == 0 {
			label = "  " + label
		}
		if i == s.Selected {
			style.Fg = f.Palette.Text
			style.Bg = f.Palette.Surface
			style.Attrs = uv.AttrBold
			if s.Section == 0 {
				label = "▸ " + name
			}
		}
		if name == current {
			label += " ✓"
		}
		height := 1
		if s.Section == 2 {
			height = 2
		}
		f.Fill(r.X+1, y, r.W-2, height, " ", style)
		f.Text(r.X+2, y, label, style, r.X+r.W-1)
	}
	x = r.X + 2
	f.Set(r.X+1, r.Y+r.H-3, " ", uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG})
	for _, part := range []struct {
		text string
		fg   color.Color
	}{{"↑↓", f.Palette.Dim}, {" select  ", f.Palette.Overlay1}, {"tab", f.Palette.Dim}, {" section", f.Palette.Overlay1}} {
		x = f.Text(x, r.Y+r.H-3, part.text, uv.Style{Fg: part.fg, Bg: f.Palette.PopupBG}, r.X+r.W-1)
	}
	apply, close := SettingsButtons(r)
	f.Text(apply.X, apply.Y, " ↵ apply ", uv.Style{Fg: f.Palette.SurfDim, Bg: f.Palette.Accent, Attrs: uv.AttrBold}, apply.X+apply.W)
	f.Text(close.X, close.Y, " esc close ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface, Attrs: uv.AttrBold}, close.X+close.W)
}
func SettingsButtons(r layout.Rect) (apply, close layout.Rect) {
	aw, cw := len([]rune(" ↵ apply ")), len([]rune(" esc close "))
	x := r.X + (r.W-aw-cw-2)/2
	return layout.Rect{X: x, Y: r.Y + r.H - 2, W: aw, H: 1}, layout.Rect{X: x + aw + 2, Y: r.Y + r.H - 2, W: cw, H: 1}
}
