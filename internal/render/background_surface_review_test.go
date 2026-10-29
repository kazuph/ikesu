package render

import (
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
	"image/color"
	"testing"
)

func TestExplicitSurfaceTransparencyOverridesCommonBackground(t *testing.T) {
	for _, theme := range []string{"catppuccin", "catppuccin-latte"} {
		for _, common := range []string{"opaque", "panel"} {
			for _, value := range []string{"transparent", "default", "reset", "none", "#abcdef"} {
				t.Run(theme+"/"+common+"/"+value, func(t *testing.T) {
					s := model.New(t.TempDir(), 120, 40)
					s.Apply(model.CmdNewTab{Workspace: 0, NoFocus: true})
					c := config.Config{}
					c.Theme.Name = theme
					c.UI.OpaqueBackground = true
					c.Theme.Custom = map[string]string{"surface0": "#abcdef"}
					base := ThemePalette(theme).PopupBG
					if common == "panel" {
						c.Theme.Custom["panel_bg"] = "#234567"
						base = color.RGBA{35, 69, 103, 255}
					}
					in := Input{State: s, SidebarConfig: c, ServerUI: true}
					reference := Compose(in).Frame
					c.Theme.Custom["surface0"] = value
					in.SidebarConfig = c
					f := Compose(in).Frame
					want, err := config.ParseThemeColor(value)
					if err != nil {
						t.Fatal(err)
					}
					count := 0
					for i, cell := range reference.Cells {
						expected := base
						if cell.Style.Bg == (color.RGBA{171, 205, 239, 255}) {
							expected = want
							count++
						}
						if f.Cells[i].Style.Bg != expected {
							t.Fatalf("cell %d,%d bg=%v want=%v surface=%s", i%f.W, i/f.W, f.Cells[i].Style.Bg, expected, value)
						}
					}
					if count == 0 {
						t.Fatal("fixture did not exercise explicit component surfaces")
					}
					inactive := Compose(in).TabHits[1]
					if f.Cell(inactive.X+2, inactive.Y).Style.Bg != want {
						t.Fatal("inactive tab did not retain explicit surface")
					}
				})
			}
		}
	}
}
