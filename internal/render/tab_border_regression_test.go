package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"reflect"
	"testing"
)

func TestTabSelectionKeepsFilledBackground(t *testing.T) {
	for _, theme := range []string{"tokyo-night", "tokyo-night-day", "catppuccin", "catppuccin-latte"} {
		for _, custom := range []bool{false, true} {
			for _, server := range []bool{false, true} {
				t.Run(fmt.Sprint(theme, "/custom=", custom, "/server=", server), func(t *testing.T) {
					s := testState(160, 40)
					s.Apply(model.CmdNewTab{Workspace: 0, NoFocus: true})
					ws := s.ActiveWorkspace()
					ws.Tabs[0].Name = "boss"
					ws.Tabs[1].Name = "impl"
					c := config.Config{}
					c.Theme.Name = theme
					if custom {
						c.Theme.Custom = map[string]string{"panel_bg": "transparent", "surface_dim": "#16161e", "surface0": "#24283c", "overlay0": "#737aa2", "overlay1": "#a9b1d6"}
					}
					for active := range ws.Tabs {
						ws.ActiveTab = active
						out := Compose(Input{State: s, SidebarConfig: c, ServerUI: server})
						for i, hit := range out.TabHits {
							want := out.Frame.Palette.Surface
							if i == active {
								want = out.Frame.Palette.Accent
							}
							for x := hit.X; x < hit.X+hit.W; x++ {
								cell := out.Frame.Cell(x, hit.Y)
								if !reflect.DeepEqual(cell.Style.Bg, want) || cell.Style.Underline != uv.UnderlineNone {
									t.Fatalf("tab=%d active=%d cell=%d,%d background=%v want=%v underline=%v", i, active, x, hit.Y, cell.Style.Bg, want, cell.Style.Underline)
								}
							}
						}
						if out.Frame.Cell(0, 10).Style.Bg != nil {
							t.Fatal("sidebar transparency changed")
						}
					}
				})
			}
		}
	}
}

func TestPaneFramesJoinSingleSidebarBoundaryOnEveryRow(t *testing.T) {
	for _, theme := range []string{"tokyo-night", "tokyo-night-day", "catppuccin", "catppuccin-latte"} {
		for _, server := range []bool{false, true} {
			for _, sidebars := range []bool{false, true} {
				t.Run(fmt.Sprint(theme, "/server=", server, "/sidebars=", sidebars), func(t *testing.T) {
					s := testState(160, 40)
					s.SidebarVisible = sidebars
					if !sidebars {
						s.SidebarHidden = true
						s.RightSidebarMode = "hidden"
					}
					s.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
					s.Apply(model.CmdSplitFocused{Dir: layout.Horizontal})
					s.Apply(model.CmdFocusPane{Pane: 1})
					s.Apply(model.CmdSplitFocused{Dir: layout.Horizontal})
					c := config.Config{}
					c.Theme.Name = theme
					out := Compose(Input{State: s, SidebarConfig: c, ServerUI: server})
					area := s.PanesArea()
					for _, pane := range s.DisplayPaneRects() {
						r := pane.Rect
						left, right := r.X, r.X+r.W-1
						if sidebars && left == area.X {
							left--
						}
						if sidebars && right == area.X+area.W-1 {
							right++
						}
						for _, x := range []int{left, right} {
							for y := r.Y + 1; y < r.Y+r.H-1; y++ {
								if got := out.Frame.Cell(x, y).Content; got != "│" {
									t.Fatalf("pane=%d edge=%d row=%d glyph=%q", pane.ID, x, y, got)
								}
							}
						}
						// The top and bottom strokes must reach the same column as the vertical edge.
						// A corner left behind at the old pane column is a visible broken frame.
						if left != r.X {
							for _, y := range []int{r.Y, r.Y + r.H - 1} {
								if got := out.Frame.Cell(r.X, y).Content; got != "─" {
									t.Fatalf("pane=%d left join row=%d glyph=%q", pane.ID, y, got)
								}
							}
						}
						if right != r.X+r.W-1 {
							for _, y := range []int{r.Y, r.Y + r.H - 1} {
								if got := out.Frame.Cell(r.X+r.W-1, y).Content; got != "─" {
									t.Fatalf("pane=%d right join row=%d glyph=%q", pane.ID, y, got)
								}
							}
						}
					}
				})
			}
		}
	}
}
