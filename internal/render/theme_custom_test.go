package render

import (
	"image/color"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
)

func TestCustomThemeAllBaseThemesAndReset(t *testing.T) {
	for _, name := range config.ThemeNames {
		original := ThemePalette(name)
		p := CustomPalette(name, map[string]string{"panel_bg": "transparent", "accent": "#123456"})
		if p.PopupBG != nil || p.Accent != (color.RGBA{18, 52, 86, 255}) || p.Text != original.Text || ThemePalette(name).Accent != original.Accent {
			t.Fatalf("%s: custom override changed unrelated or shared colors", name)
		}
	}
}

func TestEndpointActiveTabUsesPanelColorWithTransparentFallback(t *testing.T) {
	for _, custom := range []map[string]string{
		{"panel_bg": "transparent"},
		{"panel_bg": "#123456", "surface_dim": "#654321"},
		{"panel_bg": "transparent", "surface_dim": "#654321"},
	} {
		for _, serverUI := range []bool{false, true} {
			s := model.New(t.TempDir(), 160, 45)
			in := Input{State: s, ServerUI: serverUI}
			in.SidebarConfig.Theme.Custom = custom
			out := Compose(in)
			want := out.Frame.Palette.Accent
			for x := out.TabHits[0].X; x < out.TabHits[0].X+out.TabHits[0].W; x++ {
				cell := out.Frame.Cell(x, s.TabBarArea().Y)
				fg := want
				if x == out.TabHits[0].X+1 {
					fg = out.Frame.ThemeColor(Dim)
				}
				if cell.Style.Fg != fg || cell.Style.Bg != persistentBackground(in.SidebarConfig) {
					t.Fatalf("server=%v custom=%v cell=%d: %#v", serverUI, custom, x, cell.Style)
				}
			}
		}
	}
}

func TestTransparentSidebarCardsKeepDefaultBackgroundOnlyOnCards(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	s.Apply(model.CmdNewWorkspace{NoFocus: true})
	in := Input{State: s, ServerUI: true}
	in.SidebarConfig.Theme.Custom = map[string]string{"surface0": "#123456"}
	painted := Compose(in).Frame
	in.SidebarConfig.UI.TransparentSidebarCards = true
	clear := Compose(in).Frame
	view := SidebarViewForInput(in)
	cards := 0
	for _, pos := range view.Rows {
		switch pos.Row.Kind {
		case RowWorkspace, RowGit, RowCardBlank:
			cards++
			for x := 0; x < s.SidebarArea().W-1; x++ {
				if clear.Cell(x, pos.Y).Style.Bg != nil {
					t.Fatalf("card bg at %d,%d", x, pos.Y)
				}
			}
			if pos.Row.Ws != s.Active && painted.Cell(3, pos.Y).Style.Bg == nil {
				t.Fatal("explicit component background was lost")
			}
			if pos.Row.Ws == s.Active && pos.Row.Kind == RowWorkspace && clear.Cell(0, pos.Y).Content != "▌" {
				t.Fatal("active indicator lost")
			}
		case RowTitle:
			if clear.Cell(3, pos.Y).Style != painted.Cell(3, pos.Y).Style {
				t.Fatal("card flag changed section header")
			}
		}
	}
	if cards == 0 {
		t.Fatal("no cards exercised")
	}
	in.ServerUI = false
	in.SidebarConfig.UI.TransparentSidebarCards = false
	standalone := Compose(in).Frame
	in.SidebarConfig.UI.TransparentSidebarCards = true
	standaloneWithFlag := Compose(in).Frame
	for i, cell := range standalone.Cells {
		if cell.Style != standaloneWithFlag.Cells[i].Style {
			t.Fatal("standalone distribution keeps painted cards when the endpoint flag is enabled")
		}
	}
}

func TestCollapsedEndpointStatusKeepsActiveIndicator(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir()})
	s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir()})
	s.SidebarVisible = false
	in := Input{State: s, ServerUI: true}
	for _, transparent := range []bool{false, true} {
		in.SidebarConfig.UI.TransparentSidebarCards = transparent
		f := Compose(in).Frame
		for y := 0; y < len(s.Workspaces); y++ {
			c := f.Cell(0, 4+y*3)
			if c.Content != "·" || c.Style.Fg != f.Palette.Dim {
				t.Fatal("status cell missing", c)
			}
			if c.Style.Bg != nil {
				t.Fatal("collapsed status must use terminal background", c)
			}
			if (c.Style.Underline != uv.UnderlineNone) != (y == s.Active) {
				t.Fatal("collapsed active indicator lost", c)
			}
			if f.Cell(1, 4+y*3).Content != "│" {
				t.Fatal("status divider missing")
			}
		}
	}
}

func TestCustomPanelBackgroundControlsMobileHeaderAndSwitcherGround(t *testing.T) {
	for _, transparentCards := range []bool{false, true} {
		s := model.New(t.TempDir(), 60, 25)
		s.MobileWidthThreshold = (config.Config{}).MobileWidthThreshold()
		in := Input{State: s, ServerUI: true}
		in.SidebarConfig.Theme.Custom = map[string]string{"panel_bg": "transparent", "surface_dim": "#16161e", "surface0": "#24283c"}
		in.SidebarConfig.UI.TransparentSidebarCards = transparentCards
		for _, switcher := range []bool{false, true} {
			in.MobileSwitcher = switcher
			f := Compose(in).Frame
			for y := 0; y < 2; y++ {
				for x := 0; x < MobileSwitchRect(s).X; x++ {
					if f.Cell(x, y).Style.Bg != nil {
						t.Fatalf("mobile header painted at %d,%d (cards=%v switcher=%v)", x, y, transparentCards, switcher)
					}
				}
			}
			if f.Cell(s.Cols-1, 1).Style.Bg != (color.RGBA{36, 40, 60, 255}) {
				t.Fatal("switch button must keep surface0 background")
			}
			if switcher && f.Cell(s.Cols-1, s.Rows-1).Style.Bg != nil {
				t.Fatal("unused switcher ground must keep the terminal background")
			}
		}
	}
}

func TestCopyCursorAndStatusUseObservedEndpointContrastWithoutChangingLocal(t *testing.T) {
	for _, tc := range []struct {
		custom        map[string]string
		endpointColor color.RGBA
	}{
		{map[string]string{"panel_bg": "transparent"}, color.RGBA{30, 30, 46, 255}},
		{map[string]string{"panel_bg": "#123456", "surface_dim": "#654321"}, color.RGBA{18, 52, 86, 255}},
	} {
		for _, endpoint := range []bool{false, true} {
			f := NewFrame(40, 2)
			f.Palette = CustomPalette("catppuccin", tc.custom)
			f.Text(0, 0, "source", uv.Style{})
			drawCopyCursor(f, 0, 0, endpoint)
			drawCopyStatus(f, 0, 1, 40, " COPY  h/j/k/l w/b/e { } move", endpoint)
			want := f.Palette.SurfDim
			if endpoint {
				want = tc.endpointColor
			}
			for _, point := range [][2]int{{0, 0}, {0, 1}, {1, 1}, {2, 1}, {3, 1}, {4, 1}, {5, 1}} {
				cell := f.Cell(point[0], point[1])
				if cell.Style.Fg != want || cell.Style.Bg != f.Palette.Accent || cell.Style.Attrs != uv.AttrBold {
					t.Fatalf("endpoint=%v config=%v point=%v style=%+v want=%v", endpoint, tc.custom, point, cell.Style, want)
				}
			}
		}
	}
}
