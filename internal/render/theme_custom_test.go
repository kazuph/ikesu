package render

import (
	"image/color"
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
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

func TestTransparentSidebarCardsKeepDefaultBackgroundOnlyOnCards(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	in := Input{State: s, ServerUI: true}
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
			if painted.Cell(3, pos.Y).Style.Bg == nil || clear.Cell(0, pos.Y).Content != "▌" {
				t.Fatal("painted default or active indicator lost")
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

func TestCollapsedEndpointCardsKeepNumbersReadableAndRespectTransparency(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir()})
	s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir()})
	s.SidebarVisible = false
	in := Input{State: s, ServerUI: true}
	for _, transparent := range []bool{false, true} {
		in.SidebarConfig.UI.TransparentSidebarCards = transparent
		f := Compose(in).Frame
		for y := 0; y < len(s.Workspaces); y++ {
			bg := f.Palette.Surface
			if y == s.Active {
				bg = f.Palette.PanelBG
			}
			if transparent {
				bg = nil
			}
			for x := 0; x < 3; x++ {
				cell := f.Cell(x, y)
				fg := f.Palette.Text
				if x == 2 {
					fg = f.Palette.Dim
				}
				if cell.Style.Fg != fg || cell.Style.Bg != bg {
					t.Fatalf("collapsed card style at %d,%d (transparent=%v): %+v", x, y, transparent, cell.Style)
				}
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
