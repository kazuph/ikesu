package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
)

func sidebarBoundaryStyle(f *Frame, in Input, hover bool) uv.Style {
	style := uv.Style{Fg: f.Palette.Subtle}
	if value, ok := in.SidebarConfig.Theme.Custom["sidebar_border"]; ok {
		style.Fg, _ = config.ParseThemeColor(value)
	}
	if in.ServerUI {
		style.Bg = f.Palette.PopupBG
	}
	if hover {
		style.Fg = f.Palette.Accent
		if value, ok := in.SidebarConfig.Theme.Custom["sidebar_border_hover"]; ok {
			style.Fg, _ = config.ParseThemeColor(value)
		}
		style.Attrs |= uv.AttrBold
	}
	return style
}

// The pane's adjacent outside edge and sidebar boundary share one visible line.
// Hit areas stay unchanged, including both columns of the left drag handle.
func drawSidebarBoundaries(f *Frame, s *model.AppState, in Input) {
	panes := s.DisplayPaneRects()
	for _, right := range []bool{false, true} {
		area, hover := s.SidebarArea(), in.SidebarHover
		x := area.X + area.W - 1
		if right {
			area, hover = s.RightSidebarArea(), in.RightSidebarHover
			x = area.X
		}
		if area.W == 0 {
			continue
		}
		style := sidebarBoundaryStyle(f, in, hover)
		glyph := "│"
		if hover {
			glyph = "┃"
		}
		joins := map[int]bool{}
		if s.PaneBorders && (in.Copy == nil || !in.Copy.Fullscreen) {
			for _, pane := range panes {
				r := pane.Rect
				if r.W > 2 && r.H > 2 && (!right && r.X == x+1 || right && r.X+r.W == x) {
					joins[r.Y], joins[r.Y+r.H-1] = true, true
				}
			}
		}
		for y := area.Y; y < area.Y+area.H; y++ {
			line := glyph
			if joins[y] {
				line = "├"
				if right {
					line = "┤"
				}
				if hover {
					line = "┣"
					if right {
						line = "┫"
					}
				}
			}
			f.Set(x, y, line, style)
		}
	}
}
