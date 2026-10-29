package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
	"image/color"
	"testing"
)

func TestMobileSwitcherUsesCommonBackgroundAndSelectionInk(t *testing.T) {
	for _, theme := range config.ThemeNames {
		for _, mode := range []string{"default", "opaque", "panel", "surface_dim", "matching-colors"} {
			t.Run(fmt.Sprint(theme, "/", mode), func(t *testing.T) {
				s := model.New(t.TempDir(), 60, 40)
				s.MobileWidthThreshold = 64
				s.Apply(model.CmdNewTab{Workspace: 0, NoFocus: true})
				s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir()})
				s.Apply(model.CmdSelectWorkspace{Index: 0})
				c := config.Config{}
				c.Theme.Name = theme
				var want color.Color
				switch mode {
				case "opaque":
					c.UI.OpaqueBackground = true
					want = ThemePalette(theme).PopupBG
					if want == nil {
						want = ThemePalette(theme).PanelBG
					}
				case "panel", "surface_dim", "matching-colors":
					key := "panel_bg"
					if mode == "surface_dim" {
						key = mode
					}
					c.Theme.Custom = map[string]string{key: "#234567"}
					if mode == "matching-colors" {
						c.Theme.Custom["surface_dim"] = "#234567"
					}
					want = color.RGBA{35, 69, 103, 255}
				}
				in := Input{State: s, SidebarConfig: c, ServerUI: true, MobileSwitcher: true}
				f := Compose(in).Frame
				for i, cell := range f.Cells {
					if cell.Style.Bg != want {
						t.Fatalf("cell %d,%d bg=%v want=%v", i%f.W, i/f.W, cell.Style.Bg, want)
					}
				}
				off := MobileOffset(s, nil, 0)
				for i, row := range MobileRows(s, nil) {
					if row.Kind != RowWorkspace && row.Kind != RowMobileTab {
						continue
					}
					y := MobileViewport(s).Y + i - off
					if y >= s.Rows {
						continue
					}
					selected := row.Kind == RowWorkspace && row.Ws == s.Active || row.Kind == RowMobileTab && row.Line == s.ActiveWorkspace().ActiveTab
					underlined := false
					for x := 3; x < f.W; x++ {
						cell := f.Cell(x, y)
						if cell.Content != " " && cell.Content != "" && cell.Style.Underline == uv.UnderlineSingle {
							underlined = true
						}
					}
					if underlined != selected {
						t.Fatalf("row %+v selected=%v underline=%v", row, selected, underlined)
					}
				}
			})
		}
	}
}
