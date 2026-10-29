package render

import (
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
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

func TestStage5BorderlessSplitReservesGapAndPTYWidths(t *testing.T) {
	s := testState(100, 30)
	s.RightSidebarMode = "hidden" // Keep the original pane geometry for this content regression.
	s.PaneBorders = false
	tab := s.ActiveWorkspace().TabRef()
	tab.Layout.SplitFocused(layout.Vertical, 2)
	panes := s.DisplayPaneRects()
	if len(panes) != 2 {
		t.Fatal(panes)
	}
	for i, want := range []int{36, 37} {
		w, h := model.PaneTermSize(panes[i].Rect, false)
		if w != want || h != 28 {
			t.Fatalf("pane=%d PTY=%dx%d want=%dx28", i, w, h, want)
		}
	}
	if panes[0].Rect.X+panes[0].Rect.W+1 != panes[1].Rect.X {
		t.Fatal("missing one-cell pane gap", panes)
	}
}
