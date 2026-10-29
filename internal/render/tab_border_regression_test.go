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
			for _, gaps := range []bool{false, true} {
				for _, view := range []string{"normal", "narrow", "zoom"} {
					for _, sidebars := range []bool{false, true} {
						t.Run(fmt.Sprint(theme, "/server=", server, "/sidebars=", sidebars, "/gaps=", gaps, "/view=", view), func(t *testing.T) {
							s := testState(160, 40)
							s.PaneGaps = gaps
							if view == "narrow" {
								s.Cols = 64
								s.Rows = 20
							}
							s.SidebarVisible = sidebars
							if !sidebars {
								s.SidebarHidden = true
								s.RightSidebarMode = "hidden"
							}
							s.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
							s.Apply(model.CmdSplitFocused{Dir: layout.Horizontal})
							s.Apply(model.CmdFocusPane{Pane: 1})
							s.Apply(model.CmdSplitFocused{Dir: layout.Horizontal})
							if view == "narrow" {
								for {
									narrowWidth := 0
									for _, pane := range s.DisplayPaneRects() {
										if pane.ID == 1 {
											narrowWidth = pane.Rect.W
										}
									}
									if narrowWidth <= 6 {
										break
									}
									s.Apply(model.CmdFocusPane{Pane: 1})
									s.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
								}
							}
							if view == "zoom" {
								s.Apply(model.CmdZoomPane{Pane: s.FocusedPane(), Mode: "on"})
							}
							c := config.Config{}
							c.Theme.Name = theme
							out := Compose(Input{State: s, SidebarConfig: c, ServerUI: server})
							area := s.PanesArea()
							for _, pane := range s.DisplayPaneRects() {
								r := pane.Rect
								if r.W <= 2 || r.H <= 2 {
									continue
								}
								left, right := r.X, r.X+r.W-1
								if s.SidebarArea().W > 0 && left == area.X {
									left--
								}
								if s.RightSidebarArea().W > 0 && right == area.X+area.W-1 {
									right++
								}
								for _, x := range []int{left, right} {
									for y := r.Y + 1; y < r.Y+r.H-1; y++ {
										if got := out.Frame.Cell(x, y).Content; got != "│" {
											t.Fatalf("pane=%d edge=%d row=%d glyph=%q", pane.ID, x, y, got)
										}
									}
								}
								// Every visible side reaches its top and bottom junction, including
								// shared rows containing a neighboring pane's title.
								vertical := map[string]int{"│": 12, "┌": 8, "┐": 8, "└": 4, "┘": 4, "├": 12, "┤": 12, "┬": 8, "┴": 4, "┼": 12}
								for _, x := range []int{left, right} {
									if vertical[out.Frame.Cell(x, r.Y).Content]&8 == 0 || vertical[out.Frame.Cell(x, r.Y+r.H-1).Content]&4 == 0 {
										t.Fatalf("pane=%d side=%d disconnected endpoint: %q/%q", pane.ID, x, out.Frame.Cell(x, r.Y).Content, out.Frame.Cell(x, r.Y+r.H-1).Content)
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
	}
}

func TestPaneFrameAndScrollbarWidthBoundaries(t *testing.T) {
	for _, gaps := range []bool{false, true} {
		for _, width := range []int{2, 3, 6, 7} {
			for _, height := range []int{2, 3} {
				t.Run(fmt.Sprintf("gaps=%v/%dx%d", gaps, width, height), func(t *testing.T) {
					s := testState(width, height+2)
					s.SidebarVisible = false
					s.SidebarHidden = true
					s.RightSidebarMode = "hidden"
					s.PaneGaps = gaps
					r := s.DisplayPaneRects()[0].Rect
					inner, gutter := model.PaneContentRect(r, true)
					grid := make([][]uv.Cell, inner.H)
					for y := range grid {
						grid[y] = make([]uv.Cell, inner.W)
						for x := range grid[y] {
							grid[y][x] = uv.Cell{Content: "a", Width: 1}
						}
					}
					out := Compose(Input{State: s, Panes: map[int]*PaneSnap{1: {Grid: grid}}, ServerUI: true})
					framed := width > 2 && height > 2
					for y := r.Y; y < r.Y+r.H; y++ {
						for x := r.X; x < r.X+r.W; x++ {
							cell := out.Frame.Cell(x, y)
							if !framed {
								if cell.Content != "a" {
									t.Fatalf("unframed cell %d,%d=%q", x, y, cell.Content)
								}
								continue
							}
							if x == r.X || x == r.X+r.W-1 {
								if y > r.Y && y < r.Y+r.H-1 && cell.Content != "│" {
									t.Fatalf("frame side %d,%d=%q", x, y, cell.Content)
								}
							}
						}
					}
					if framed {
						for _, corner := range []struct {
							x, y  int
							glyph string
						}{{r.X, r.Y, "┌"}, {r.X + r.W - 1, r.Y, "┐"}, {r.X, r.Y + r.H - 1, "└"}, {r.X + r.W - 1, r.Y + r.H - 1, "┘"}} {
							if out.Frame.Cell(corner.x, corner.y).Content != corner.glyph {
								t.Fatal("corner", corner)
							}
						}
					}
					if framed && width < 7 && gutter != 0 {
						t.Fatal("scrollbar column in a pane narrower than its minimum", width, gutter)
					}
				})
			}
		}
	}
}

// A six-cell upper split meets the lower header immediately after its prefix.
// The next wide character must yield to that structural cell.
func TestWidePaneTitlesPreserveFrameJunctions(t *testing.T) {
	for _, title := range []string{"日本語の作業タイトル", "🐱☕作業中", "日A🐱本B語", "👩‍💻日本語"} {
		for _, theme := range []string{"tokyo-night", "tokyo-night-day", "catppuccin", "catppuccin-latte"} {
			for _, gaps := range []bool{false, true} {
				for _, view := range []string{"split", "narrow", "zoom"} {
					t.Run(fmt.Sprintf("%s/%s/gaps=%v/%s", title, theme, gaps, view), func(t *testing.T) {
						s := testState(60, 22)
						s.SidebarVisible = false
						s.SidebarHidden = true
						s.RightSidebarMode = "hidden"
						s.PaneGaps = gaps
						l := s.ActiveWorkspace().ActiveLayout()
						l.SplitAt(1, layout.Horizontal, 2, .5, false)
						l.SplitAt(1, layout.Vertical, 3, .1, false)
						if view == "narrow" {
							l.SplitAt(2, layout.Vertical, 4, .2, false)
						}
						if view == "zoom" {
							s.Apply(model.CmdZoomPane{Pane: 2, Mode: "on"})
						}
						c := config.Config{}
						c.Theme.Name = theme
						base := Compose(Input{State: s, SidebarConfig: c, ServerUI: true}).Frame
						snaps := map[int]*PaneSnap{}
						for _, p := range s.DisplayPaneRects() {
							snaps[p.ID] = &PaneSnap{Label: title}
						}
						got := Compose(Input{State: s, SidebarConfig: c, ServerUI: true, Panes: snaps}).Frame
						masks := map[string]int{"│": 12, "┌": 10, "┐": 9, "└": 6, "┘": 5, "├": 14, "┤": 13, "┬": 11, "┴": 7, "┼": 15}
						for y := 0; y < base.H; y++ {
							for x := 0; x < base.W; x++ {
								want := masks[base.Cell(x, y).Content]
								if want&12 != 0 && masks[got.Cell(x, y).Content]&want != want {
									t.Errorf("title=%q structural cell %d,%d got=%q width=%d want=%q", title, x, y, got.Cell(x, y).Content, got.Cell(x, y).Width, base.Cell(x, y).Content)
								}
							}
						}
						if !gaps && view == "split" {
							var lower model.Rect
							for _, p := range s.DisplayPaneRects() {
								if p.ID == 2 {
									lower = p.Rect
								}
							}
							if got.Cell(lower.X+5, lower.Y).Content != "…" {
								t.Errorf("wide character at junction must become ellipsis: %q", got.Cell(lower.X+5, lower.Y).Content)
							}
						}
					})
				}
			}
		}
	}
}
