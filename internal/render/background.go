package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"image/color"
)

func persistentBackground(c config.Config) color.Color {
	for _, key := range []string{"panel_bg", "surface_dim"} {
		if value, ok := c.Theme.Custom[key]; ok {
			if parsed, err := config.ParseThemeColor(value); err == nil {
				return parsed
			}
		}
	}
	if !c.UI.OpaqueBackground {
		return nil
	}
	p := ThemePalette(c.ThemeName())
	if p.PopupBG != nil {
		return p.PopupBG
	}
	return p.PanelBG
}

// Apply the same terminal-default background policy to permanent chrome and
// unpainted pane cells. Application-supplied colors and floating layers retain
// their own backgrounds; floating layers are drawn after this pass.
func applyPersistentBackground(f *Frame, in Input) {
	bg := persistentBackground(in.SidebarConfig)
	s := in.State
	var content []layout.Rect
	if in.Copy != nil && in.Copy.Fullscreen {
		content = []layout.Rect{s.PanesArea()}
	} else {
		for _, pane := range s.DisplayPaneRects() {
			inner, _ := model.PaneContentRect(pane.Rect, s.PaneBorders)
			content = append(content, inner)
		}
	}
	_, customSurface := in.SidebarConfig.Theme.Custom["surface0"]
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			cell := f.Cell(x, y)
			application := false
			for _, r := range content {
				if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
					application = true
					break
				}
			}
			if application {
				if cell.Style.Bg == nil {
					cell.Style.Bg = bg
				}
				continue
			}
			old := cell.Style.Bg
			// Explicit component colors continue to override the common ground.
			if customSurface && old != nil && old == f.Palette.Surface {
				continue
			}
			cell.Style.Bg = bg
			if old != nil && old == f.Palette.PanelBG && cell.Content != " " && cell.Content != "" {
				cell.Style.Underline = uv.UnderlineSingle
			}
			if old != nil && old == f.Palette.Accent && cell.Style.Fg != nil && (cell.Style.Fg == f.Palette.PanelBG || cell.Style.Fg == f.Palette.PopupBG || cell.Style.Fg == f.Palette.SurfDim) {
				cell.Style.Fg = f.Palette.Accent
				cell.Style.Attrs |= uv.AttrBold
				cell.Style.Underline = uv.UnderlineSingle
			}
			if cell.Style.Attrs&uv.AttrReverse != 0 {
				cell.Style.Attrs &^= uv.AttrReverse
				cell.Style.Attrs |= uv.AttrBold
				cell.Style.Underline = uv.UnderlineSingle
			}
		}
	}
}
