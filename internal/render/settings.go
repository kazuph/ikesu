package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/layout"
	"image/color"
	"strings"
)

// Settings geometry and options follow the recorded distribution screens.
var ThemeNames = config.ThemeNames
var ToastNames = []string{"off", "ikesu", "terminal", "system"}

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
		return r.Y + min(8, max(5, r.H-6)) + index
	}
	return r.Y + min(8, max(5, r.H-6)) + 2*index
}
func SettingsTabsY(r layout.Rect) int {
	if r.H < 6 || r.H == 8 {
		return -1
	}
	if r.H < 8 {
		return r.Y + 1
	}
	return r.Y + 2
}
func SettingsChoiceVisible(r layout.Rect, section, index int) bool {
	if r.H < 6 {
		return false
	}
	y := SettingsChoiceY(r, section, index)
	if section == 2 {
		return y+2 <= r.Y+r.H-3
	}
	return y < r.Y+r.H-4
}
func SettingsChoiceOffset(r layout.Rect, s *Settings) int {
	if s.Section != 0 {
		return 0
	}
	visible := 0
	for visible < len(ThemeNames) && SettingsChoiceVisible(r, s.Section, visible) {
		visible++
	}
	if visible == 0 {
		return 0
	}
	return max(0, min(s.Selected-visible+1, len(ThemeNames)-visible))
}
func drawSettings(f *Frame, s *Settings) {
	r := SettingsRect(f.W, f.H)
	f.Fill(r.X, r.Y, r.W, r.H, " ", uv.Style{Bg: f.Palette.PopupBG})
	f.Box(r.X, r.Y, r.W, r.H, uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG})
	// Height cases follow settings-height-sections.steps: short boxes reserve the footer first.
	if r.H < 6 {
		return
	}
	if r.H >= 8 {
		f.Text(r.X+1, r.Y+1, " settings", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, r.X+r.W-1)
	}
	tabsY := SettingsTabsY(r)
	if tabsY >= 0 {
		f.Fill(r.X+1, tabsY, r.W-2, 1, " ", uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.PopupBG})
	}
	x := r.X + 2
	for i, label := range []string{"theme", "sound", "toasts"} {
		if tabsY < 0 {
			break
		}
		style := uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.PopupBG}
		if s.Section == i {
			style.Fg = f.Palette.SurfDim
			style.Bg = f.Palette.Accent
			style.Attrs = uv.AttrBold
		}
		x = f.Text(x, tabsY, label, style, r.X+r.W-1) + 3
	}
	if r.H >= 8 {
		ruleY := r.Y + 3
		if r.H == 8 {
			ruleY--
		}
		for x := r.X + 1; x < r.X+r.W-1; x++ {
			f.Set(x, ruleY, "─", uv.Style{Fg: f.Palette.Surface, Bg: f.Palette.PopupBG})
		}
	}
	names := ThemeNames
	current := s.Theme
	description := ""
	switch s.Section {
	case 1:
		description = "play sounds when agents change state in background"
		names = []string{"sound alerts: on", "sound alerts: off"}
		current = names[1]
		if s.Sound {
			current = names[0]
		}
	case 2:
		description = "choose where background popup notifications should appear"
		names = []string{"notification popups: off", "notification popups: inside ikesu", "notification popups: via terminal", "notification popups: via system"}
		for i, n := range ToastNames {
			if s.Toast == n {
				current = names[i]
			}
		}
	}
	if description != "" {
		height := min(2, max(0, (r.H-10)/2))
		style := uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.PopupBG}
		f.Fill(r.X+1, r.Y+5, r.W-2, height, " ", style)
		var lines []string
		for _, word := range strings.Fields(description) {
			if len(lines) == 0 || len(lines[len(lines)-1])+1+len(word) > r.W-4 {
				lines = append(lines, word)
			} else {
				lines[len(lines)-1] += " " + word
			}
		}
		for i := 0; i < min(height, len(lines)); i++ {
			f.Fill(r.X+1, r.Y+5+i, r.W-2, 1, " ", style)
			indent := 0
			if i == 0 {
				indent = 1
			}
			f.Text(r.X+1+indent, r.Y+5+i, lines[i], style, r.X+r.W-2)
		}
	}
	offset := SettingsChoiceOffset(r, s)
	for i := offset; i < len(names); i++ {
		name := names[i]
		y := SettingsChoiceY(r, s.Section, i-offset)
		if !SettingsChoiceVisible(r, s.Section, i-offset) {
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
		if s.Section == 2 && y+1 < r.Y+r.H-4 {
			height = 2
		}
		f.Fill(r.X+1, y, r.W-2, height, " ", style)
		f.Text(r.X+2, y, label, style, r.X+r.W-1)
	}
	x = r.X + 2
	apply, close := SettingsButtons(r)
	f.Text(apply.X, apply.Y, " ↵ apply ", uv.Style{Fg: f.Palette.SurfDim, Bg: f.Palette.Accent, Attrs: uv.AttrBold}, min(apply.X+apply.W, r.X+r.W-1))
	f.Text(close.X, close.Y, " esc close ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface, Attrs: uv.AttrBold}, min(close.X+close.W, r.X+r.W-1))
	hintY := r.Y + r.H - 3
	if r.H == 6 {
		hintY++
	}
	f.Set(r.X+1, hintY, " ", uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG})
	for _, part := range []struct {
		text string
		fg   color.Color
	}{{"↑↓", f.Palette.Dim}, {" select  ", f.Palette.Overlay1}, {"tab", f.Palette.Dim}, {" section", f.Palette.Overlay1}} {
		x = f.Text(x, hintY, part.text, uv.Style{Fg: part.fg, Bg: f.Palette.PopupBG}, r.X+r.W-1)
	}
	if hintY == apply.Y {
		for xx := max(apply.X, r.X+1); xx < min(x, apply.X+apply.W); xx++ {
			cell := f.Cell(xx, hintY)
			cell.Style.Bg = f.Palette.Accent
			cell.Style.Attrs |= uv.AttrBold
		}
	}
}
func SettingsButtons(r layout.Rect) (apply, close layout.Rect) {
	if r.H < 6 {
		return layout.Rect{}, layout.Rect{}
	}
	aw, cw := len([]rune(" ↵ apply ")), len([]rune(" esc close "))
	x := r.X + 1 + max(0, (r.W-2-aw-cw-2)/2)
	return layout.Rect{X: x, Y: r.Y + r.H - 2, W: aw, H: 1}, layout.Rect{X: x + aw + 2, Y: r.Y + r.H - 2, W: cw, H: 1}
}
