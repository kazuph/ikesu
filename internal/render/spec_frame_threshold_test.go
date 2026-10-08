package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
	"testing"
)

func TestSPECFrameAllFourEdgesAndTinyContent(t *testing.T) {
	for _, borders := range []bool{false, true} {
		for w := 1; w <= 8; w++ {
			for h := 1; h <= 5; h++ {
				s := testState(model.SidebarWidth+w, h+2)
				s.ShowTabBar = false
				s.PaneBorders = borders
				if h <= 2 {
					s.Rows = h + 1 // action bar is hidden below five screen rows
				}
				area := s.PanesArea()
				pid := s.FocusedPane()
				grid := make([][]uv.Cell, h)
				for y := range grid {
					grid[y] = make([]uv.Cell, w)
					for x := range grid[y] {
						grid[y][x] = uv.Cell{Content: "T", Width: 1}
					}
				}
				f := NewFrame(s.Cols, s.Rows)
				drawPanes(f, s, Input{Panes: map[int]*PaneSnap{int(pid): {Grid: grid, Label: "title"}}}, &Output{})
				framed := borders && w > 2 && h > 2
				for y := area.Y; y < area.Y+h; y++ {
					for x := area.X; x < area.X+w; x++ {
						c := f.Cell(x, y)
						edge := x == area.X || x == area.X+w-1 || y == area.Y || y == area.Y+h-1
						if !framed && c.Content != "T" {
							t.Fatalf("tiny/unframed %v %dx%d at %d,%d: %q", borders, w, h, x, y, c.Content)
						}
						if framed && edge && c.Content == "T" {
							t.Fatalf("missing frame %dx%d at %d,%d", w, h, x, y)
						}
					}
				}
			}
		}
	}
}

func TestSPECHelpScrollbarThumbMatchesScroll(t *testing.T) {
	rows := make([]HelpRow, 50)
	for _, scroll := range []int{0, 3, 30} {
		f := NewFrame(80, 24)
		out := &Output{}
		drawHelp(f, &HelpInput{Rows: rows, Scroll: scroll}, out)
		r := out.HelpRect
		body := r.H - 4
		limit := len(rows) - body
		actual := min(scroll, limit)
		th := body * body / len(rows)
		if th < 1 {
			th = 1
		}
		track := body - th
		top := (actual*track + limit - 1) / limit
		for y := 0; y < body; y++ {
			want := "▕"
			if y >= top && y < top+th {
				want = "▐"
			}
			if got := f.Cell(r.X+r.W-2, r.Y+2+y).Content; got != want {
				t.Fatalf("scroll=%d row=%d got=%q want=%q", scroll, y, got, want)
			}
		}
	}
}
