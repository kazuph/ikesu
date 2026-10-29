package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
	"reflect"
	"testing"
)

func TestSidebarSingleBoundaryHoverAndThemeColors(t *testing.T) {
	for _, theme := range []string{"catppuccin", "catppuccin-latte"} {
		for _, hover := range []bool{false, true} {
			s := testState(160, 30)
			c := config.Config{}
			c.Theme.Name = theme
			in := Input{State: s, SidebarConfig: c, ServerUI: true, SidebarHover: hover, RightSidebarHover: hover}
			out := Compose(in)
			for _, right := range []bool{false, true} {
				a := s.SidebarArea()
				x, adj := a.X+a.W-1, a.X+a.W
				if right {
					a = s.RightSidebarArea()
					x, adj = a.X, a.X-1
				}
				if a.W == 0 {
					continue
				}
				want := out.Frame.Palette.Subtle
				if hover {
					want = out.Frame.Palette.Accent
				}
				for y := 2; y < s.Rows-2; y++ {
					cell := out.Frame.Cell(x, y)
					if cell.Content != "│" || !reflect.DeepEqual(cell.Style.Fg, want) {
						t.Fatal(theme, right, hover, y, cell, want)
					}
					if cell := out.Frame.Cell(adj, y); cell.Content == "│" {
						t.Fatal("duplicate boundary", right, y)
					}
				}
			}
			c.Theme.Custom = map[string]string{"sidebar_border": "#123456", "sidebar_border_hover": "#abcdef"}
			in.SidebarConfig = c
			out = Compose(in)
			want, _ := config.ParseThemeColor(c.Theme.Custom["sidebar_border"])
			if hover {
				want, _ = config.ParseThemeColor(c.Theme.Custom["sidebar_border_hover"])
			}
			if !reflect.DeepEqual(out.Frame.Cell(s.SidebarArea().W-1, 5).Style.Fg, want) {
				t.Fatal("custom boundary color ignored")
			}
		}
	}
}

func TestScrollbarOverlayRestoresLastColumnWithoutChangingWidth(t *testing.T) {
	s := testState(160, 30)
	pid := s.FocusedPane()
	inner, _ := model.PaneContentRect(s.DisplayPaneRects()[0].Rect, s.PaneBorders)
	w, h := model.PaneTermSize(s.DisplayPaneRects()[0].Rect, s.PaneBorders)
	if w != inner.W || h != inner.H {
		t.Fatal("overlay reserves terminal width", w, h, inner)
	}
	grid := make([][]uv.Cell, h)
	for y := range grid {
		grid[y] = make([]uv.Cell, w)
		for x := range grid[y] {
			grid[y][x] = uv.Cell{Content: "Z", Width: 1}
		}
	}
	for _, hidden := range []bool{true, false, true} {
		snap := &PaneSnap{Grid: grid, ScrollN: 100, SuppressScrollbar: hidden}
		out := Compose(Input{State: s, Panes: map[int]*PaneSnap{pid: snap}})
		for y := 0; y < h; y++ {
			got := out.Frame.Cell(inner.X+w-1, inner.Y+y).Content
			if hidden && got != "Z" || !hidden && got != "▕" && got != "▐" {
				t.Fatal(hidden, y, got)
			}
			if out.Frame.Cell(inner.X+w-2, inner.Y+y).Content != "Z" {
				t.Fatal("overlay altered neighbor")
			}
		}
	}
}
