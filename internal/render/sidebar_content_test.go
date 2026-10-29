package render

import (
	"crypto/sha256"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"reflect"
	"testing"
)

func TestSidebarFullscreenCopyPreservesVerticalTextAndSelection(t *testing.T) {
	for _, theme := range []string{"catppuccin", "catppuccin-latte"} {
		for _, split := range []bool{false, true} {
			for _, zoom := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/split=%t/zoom=%t", theme, split, zoom), func(t *testing.T) {
					s := testState(160, 40)
					if split {
						s.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
					}
					pid := s.FocusedPane()
					if zoom {
						s.Apply(model.CmdZoomPane{Pane: pid, Mode: "on"})
					}
					area := s.PanesArea()
					grid := make([][]uv.Cell, area.H)
					for y := range grid {
						grid[y] = make([]uv.Cell, area.W)
						for x := range grid[y] {
							grid[y][x] = uv.Cell{Content: "│", Width: 1}
						}
					}
					in := Input{State: s, ServerUI: true, Panes: map[int]*PaneSnap{pid: {Grid: grid}}, Copy: &CopyMark{Pane: pid, Fullscreen: true, X: -1, Y: -1}}
					in.SidebarConfig.Theme.Name = theme
					plain := Compose(in)
					in.Copy.Sel = &Selection{Pane: pid, FromX: 0, FromY: 2, ToX: area.W - 1, ToY: 2}
					selected := Compose(in)
					for y := 0; y < area.H; y++ {
						for _, x := range []int{0, area.W - 1} {
							p, c := plain.Frame.Cell(area.X+x, area.Y+y), selected.Frame.Cell(area.X+x, area.Y+y)
							if p.Content != "│" || c.Content != "│" {
								t.Fatal("body glyph erased", x, y, p, c)
							}
							if y == 2 && reflect.DeepEqual(p.Style, c.Style) {
								t.Fatal("selection style lost", x, y)
							}
						}
					}
					in.Copy = nil
					normal := Compose(in)
					for _, pr := range s.DisplayPaneRects() {
						if pr.Rect.X == area.X && normal.Frame.Cell(pr.Rect.X, pr.Rect.Y+2).Content == "│" {
							t.Fatal("duplicate left frame")
						}
						if pr.Rect.X+pr.Rect.W == area.X+area.W && normal.Frame.Cell(pr.Rect.X+pr.Rect.W-1, pr.Rect.Y+2).Content == "│" {
							t.Fatal("duplicate right frame")
						}
					}
				})
			}
		}
	}
}

func BenchmarkSidebarBoundaries(b *testing.B) {
	s := testState(160, 40)
	s.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	s.Apply(model.CmdSplitFocused{Dir: layout.Horizontal})
	in := Input{State: s, ServerUI: true}
	f := Compose(in).Frame
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		drawSidebarBoundaries(f, s, in)
	}
}

func TestSidebarNormalFrameDigest(t *testing.T) {
	s := testState(160, 40)
	s.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	s.Apply(model.CmdSplitFocused{Dir: layout.Horizontal})
	f := Compose(Input{State: s, ServerUI: true}).Frame
	hash := sha256.New()
	for y := 0; y < s.Rows; y++ {
		for x := 0; x < s.Cols; x++ {
			fmt.Fprintf(hash, "%#v", f.Cell(x, y))
		}
	}
	t.Logf("FRAME_SHA256=%x", hash.Sum(nil))
}
