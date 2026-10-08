package render

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
	"image/color"
	"testing"
)

func TestG2SettingsThemeAppliesPerFramePreservesExplicitTokenColor(t *testing.T) {
	s := model.New(t.TempDir(), 120, 50)
	c := config.Config{}
	c.Theme.Name = "dracula"
	f := Compose(Input{State: s, SidebarConfig: c, Settings: &Settings{Theme: "dracula"}}).Frame
	if f.Cell(22, 14).Style.Fg != (color.RGBA{R: 0xbd, G: 0x93, B: 0xf9, A: 0xff}) {
		t.Fatal("theme border", f.Cell(22, 14))
	}
	original := Compose(Input{State: s, Settings: &Settings{Theme: "catppuccin"}}).Frame
	if original.Cell(22, 14).Style.Fg != Accent {
		t.Fatal("another frame theme changed")
	}
	for _, name := range ThemeNames {
		if _, ok := palettes[name]; !ok {
			t.Fatal("theme missing", name)
		}
	}
	fg := "#89b4fa"
	c.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "$custom", Fg: &fg}}}
	// Explicit colors pass through tokenStyle after the theme's base color.
	style := tokenStyle(c.UI.Sidebar.Agents.Rows[0][0], f.Cell(22, 14).Style)
	if style.Fg != Accent {
		t.Fatal("explicit token color changed", style)
	}
}
