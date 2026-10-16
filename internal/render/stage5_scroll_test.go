package render

import (
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"reflect"
	"testing"
)

func TestStage5BorderlessGutterAndFocus(t *testing.T) {
	for _, borders := range []bool{false, true} {
		s := testState(100, 30)
		s.PaneBorders = borders
		tab := s.ActiveWorkspace().TabRef()
		first := tab.Layout.Focused()
		tab.Layout.SplitFocused(layout.Vertical, first+1)
		snaps := map[int]*PaneSnap{first: {ScrollN: 100}, first + 1: {ScrollN: 100}}
		for _, focus := range []int{first, first + 1} {
			tab.Layout.Focus(focus)
			out := Compose(Input{State: s, ServerUI: true, Panes: snaps})
			for _, pr := range s.DisplayPaneRects() {
				inner, g := model.PaneContentRect(pr.Rect, borders)
				if g != 1 {
					t.Fatalf("borderless gutter lost: borders=%v rect=%+v", borders, inner)
				}
				track, thumb := out.Frame.Cell(inner.X+inner.W-1, inner.Y), out.Frame.Cell(inner.X+inner.W-1, inner.Y+inner.H-1)
				tc, hc, glyph := out.Frame.Palette.SurfDim, out.Frame.Palette.Dim, "▕"
				if pr.IsFocused {
					tc, hc, glyph = out.Frame.Palette.Dim, out.Frame.Palette.Overlay1, "▐"
				}
				if track.Content != "▕" || !reflect.DeepEqual(track.Style.Fg, tc) || thumb.Content != glyph || !reflect.DeepEqual(thumb.Style.Fg, hc) {
					t.Fatalf("borders=%v focus=%v track=%+v thumb=%+v", borders, pr.IsFocused, track, thumb)
				}
			}
		}
	}
}
