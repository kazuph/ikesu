package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
	"image/color"
	"testing"
)

func TestDefaultBackgroundTransparentAcrossThemes(t *testing.T) {
	for _, theme := range config.ThemeNames {
		t.Run(theme, func(t *testing.T) {
			s := model.New(t.TempDir(), 120, 40)
			s.Apply(model.CmdNewTab{Workspace: 0, NoFocus: true})
			in := Input{State: s, ServerUI: true}
			in.SidebarConfig.Theme.Name = theme
			for _, width := range []int{120, 60} {
				s.Cols = width
				s.MobileWidthThreshold = (config.Config{}).MobileWidthThreshold()
				out := ComposeClient(in, nil, "test", false)
				f := out.Frame
				for i, c := range f.Cells {
					want, _ := tabBackgroundExpectation(out, s, i%f.W, i/f.W)
					if c.Style.Bg != want {
						t.Fatalf("theme=%s width=%d cell=%d,%d background=%v", theme, width, i%f.W, i/f.W, c.Style.Bg)
					}
				}
			}
		})
	}
}

func TestBackgroundSettingAppliesToAllPermanentRegions(t *testing.T) {
	for _, mode := range []string{"opaque", "panel", "surface_dim", "transparent-over-opaque"} {
		t.Run(mode, func(t *testing.T) {
			s := model.New(t.TempDir(), 120, 40)
			in := Input{State: s, ServerUI: true}
			in.SidebarConfig.UI.OpaqueBackground = true
			var want color.Color = ThemePalette("catppuccin").PopupBG
			switch mode {
			case "panel":
				in.SidebarConfig.Theme.Custom = map[string]string{"panel_bg": "#123456", "surface_dim": "#654321"}
				want = color.RGBA{18, 52, 86, 255}
			case "surface_dim":
				in.SidebarConfig.Theme.Custom = map[string]string{"surface_dim": "#654321"}
				want = color.RGBA{101, 67, 33, 255}
			case "transparent-over-opaque":
				in.SidebarConfig.Theme.Custom = map[string]string{"panel_bg": "transparent"}
				want = nil
			}
			out := ComposeClient(in, nil, "test", false)
			for i, c := range out.Frame.Cells {
				expected := want
				if tab, ok := tabBackgroundExpectation(out, s, i%s.Cols, i/s.Cols); ok {
					expected = tab
				}
				if c.Style.Bg != expected {
					t.Fatalf("%s x=%d y=%d bg=%v want=%v", mode, i%s.Cols, i/s.Cols, c.Style.Bg, expected)
				}
			}
		})
	}
}

func TestDefaultBackgroundKeepsApplicationColorsAndFloatingReadability(t *testing.T) {
	s := model.New(t.TempDir(), 120, 40)
	custom := color.RGBA{12, 34, 56, 255}
	in := Input{State: s, ServerUI: true, Panes: map[int]*PaneSnap{1: {Grid: [][]uv.Cell{{{Content: "A", Width: 1, Style: uv.Style{Bg: custom}}}}}}}
	f := Compose(in).Frame
	inner, _ := model.PaneContentRect(s.DisplayPaneRects()[0].Rect, s.PaneBorders)
	if f.Cell(inner.X, inner.Y).Style.Bg != custom {
		t.Fatal("application background was overwritten")
	}
	for _, theme := range config.ThemeNames {
		in.SidebarConfig.Theme.Name = theme
		in.SidebarConfig.Theme.Custom = map[string]string{"panel_bg": "transparent"}
		in.Menu = &Menu{X: 40, Y: 10, Title: "floating", Items: []MenuItem{{Label: "readable"}}, Sel: -1, Hover: -1}
		out := Compose(in)
		r := out.MenuRect
		if r.W == 0 {
			t.Fatal("menu was not rendered")
		}
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				if out.Frame.Cell(x, y).Style.Bg == nil {
					t.Fatalf("floating menu must cover underlying text: theme=%s x=%d y=%d", theme, x, y)
				}
			}
		}
	}
}

// Tab fills are the explicit exception to permanent-chrome transparency.
func tabBackgroundExpectation(out *Output, s *model.AppState, x, y int) (color.Color, bool) {
	for i, r := range out.TabHits {
		if y == r.Y && x >= r.X && x < r.X+r.W {
			if i == s.ActiveWorkspace().ActiveTab {
				return out.Frame.Palette.Accent, true
			}
			return out.Frame.Palette.Surface, true
		}
	}
	return nil, false
}
