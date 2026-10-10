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
}
