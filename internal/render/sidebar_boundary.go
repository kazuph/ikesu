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
	for _, right := range []bool{false, true} {
		area, hover := s.SidebarArea(), in.SidebarHover
		x, adjacent := area.X+area.W-1, area.X+area.W
		if right {
			area, hover = s.RightSidebarArea(), in.RightSidebarHover
			x, adjacent = area.X, area.X-1
		}
		if area.W == 0 {
			continue
		}
		style := sidebarBoundaryStyle(f, in, hover)
		glyph := "│"
		if hover {
			glyph = "┃"
		}
		for y := area.Y; y < area.Y+area.H; y++ {
			f.Set(x, y, glyph, style)
			if !s.PaneBorders {
				continue
			}
			for _, pr := range s.DisplayPaneRects() {
				if pr.Rect.W <= 2 || pr.Rect.H <= 2 || y < pr.Rect.Y || y >= pr.Rect.Y+pr.Rect.H {
					continue
				}
				edge := pr.Rect.X
				if right {
					edge = pr.Rect.X + pr.Rect.W - 1
				}
				if edge != adjacent {
					continue
				}
				if cell := f.Cell(adjacent, y); cell != nil {
					switch cell.Content {
					case "│":
						f.Set(adjacent, y, " ", uv.Style{})
					}
				}
			}
		}
	}
}
