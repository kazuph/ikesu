package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"image/color"
	"strings"
	"testing"
)

func TestCopyStatusBackgroundWithBordersAndSplitNeighbors(t *testing.T) {
	for _, theme := range config.ThemeNames {
		for _, borders := range []bool{false, true} {
			for _, direction := range []layout.Direction{layout.Vertical, layout.Horizontal} {
				for _, mode := range []string{"default", "panel", "surface_dim"} {
					t.Run(fmt.Sprintf("%s/borders=%v/%v/%s", theme, borders, direction, mode), func(t *testing.T) {
						s := model.New(t.TempDir(), 120, 40)
						s.PaneBorders = borders
						s.Apply(model.CmdSplitFocused{Dir: direction})
						s.ActiveWorkspace().TabRef().Layout.Focus(1)
						c := config.Config{}
						c.Theme.Name = theme
						var common color.Color
						if mode != "default" {
							key := mode
							if key == "panel" {
								key = "panel_bg"
							}
							c.Theme.Custom = map[string]string{key: "#234567"}
							common = color.RGBA{35, 69, 103, 255}
						}
						app := color.RGBA{12, 34, 56, 255}
						snaps := map[int]*PaneSnap{}
						for _, pr := range s.DisplayPaneRects() {
							inner, _ := model.PaneContentRect(pr.Rect, borders)
							grid := make([][]uv.Cell, inner.H)
							for y := range grid {
								grid[y] = make([]uv.Cell, inner.W)
								for x := range grid[y] {
									grid[y][x] = uv.Cell{Content: "a", Width: 1, Style: uv.Style{Bg: app}}
								}
							}
							snaps[pr.ID] = &PaneSnap{Grid: grid}
						}
						in := Input{State: s, SidebarConfig: c, Panes: snaps, Copy: &CopyMark{Pane: 1}, CopyHint: " COPY  /find█"}
						f := Compose(in).Frame
						for _, pr := range s.DisplayPaneRects() {
							inner, _ := model.PaneContentRect(pr.Rect, borders)
							if pr.ID != 1 {
								for y := inner.Y; y < inner.Y+inner.H; y++ {
									if cell := f.Cell(inner.X+1, y); cell.Style.Bg != app {
										t.Fatalf("neighbor app changed at %d,%d: %v", inner.X+1, y, cell.Style.Bg)
									}
								}
								continue
							}
							y := pr.Rect.Y + pr.Rect.H - 1
							text := ""
							for x := pr.Rect.X; x < pr.Rect.X+pr.Rect.W; x++ {
								cell := f.Cell(x, y)
								text += cell.Content
								if cell.Style.Bg != common {
									t.Fatalf("copy status %d,%d bg=%v want=%v", x, y, cell.Style.Bg, common)
								}
							}
							if !strings.Contains(text, " COPY  /find█") {
								t.Fatalf("search text or cursor missing: %q", text)
							}
							cell := f.Cell(pr.Rect.X+1, y)
							if cell.Style.Fg != f.Palette.Accent || cell.Style.Attrs&uv.AttrBold == 0 || cell.Style.Underline != uv.UnderlineSingle {
								t.Fatal("copy label lost selection ink")
							}
						}
					})
				}
			}
		}
	}
}
