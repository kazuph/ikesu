package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"image/color"
)

// Retain the surface role until the common-background pass, including when
// its explicit color is transparent. The marker never reaches terminal output.
type transparentSurfaceBackground struct{}

func (transparentSurfaceBackground) RGBA() (r, g, b, a uint32) { return }

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
func applyPersistentBackground(f *Frame, in Input, tabs []layout.Rect) {
	bg := persistentBackground(in.SidebarConfig)
	s := in.State
	var content []layout.Rect
	if s.Mobile() && in.MobileSwitcher {
		// The switcher replaces the application area with ikesu-owned rows.
	} else if in.Copy != nil && in.Copy.Fullscreen {
		content = []layout.Rect{s.PanesArea()}
	} else {
		for _, pane := range s.DisplayPaneRects() {
			inner, _ := model.PaneContentRect(pane.Rect, s.PaneBorders)
			content = append(content, inner)
		}
	}
	area := s.PanesArea()
	var statusRows []layout.Rect
	if in.ModeHint != "" && !(in.ServerUI && in.Prefix) {
		statusRows = append(statusRows, layout.Rect{X: area.X, Y: area.Y + area.H - 1, W: area.W, H: 1})
	}
	if in.Copy != nil && !in.ServerUI && in.CopyHint != "" {
		if in.Copy.Fullscreen {
			statusRows = append(statusRows, layout.Rect{X: area.X, Y: area.Y + area.H - 1, W: area.W, H: 1})
		} else {
			for _, pane := range s.DisplayPaneRects() {
				if pane.ID == in.Copy.Pane {
					statusRows = append(statusRows, layout.Rect{X: pane.Rect.X, Y: pane.Rect.Y + pane.Rect.H - 1, W: pane.Rect.W, H: 1})
				}
			}
		}
	}
	_, customSurface := in.SidebarConfig.Theme.Custom["surface0"]
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			cell := f.Cell(x, y)
			if _, explicit := cell.Style.Bg.(transparentSurfaceBackground); explicit {
				cell.Style.Bg = nil
				continue
			}
			// Tab chips keep their semantic fill, including the selected accent.
			// The rest of the strip still uses the common background.
			tab := false
			if len(tabs) > 0 && y == tabs[0].Y {
				for _, r := range tabs {
					if x >= r.X && x < r.X+r.W {
						tab = true
						break
					}
				}
			}
			if tab {
				continue
			}
			application := false
			for _, r := range content {
				if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
					application = true
					break
				}
			}
			for _, row := range statusRows {
				if x >= row.X && x < row.X+row.W && y == row.Y {
					application = false
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
			if !(s.Mobile() && in.MobileSwitcher) && old != nil && old == f.Palette.PanelBG && cell.Content != " " && cell.Content != "" {
				cell.Style.Underline = uv.UnderlineSingle
			}
			if old != nil && (old == f.Palette.Accent || old == f.Palette.Mauve) && cell.Style.Fg != nil && (cell.Style.Fg == f.Palette.PanelBG || cell.Style.Fg == f.Palette.PopupBG || cell.Style.Fg == f.Palette.SurfDim) {
				cell.Style.Fg = old
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
