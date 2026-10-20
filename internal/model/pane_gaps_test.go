package model

import (
	"github.com/kazuph/ikesu/internal/layout"
	"testing"
)

func TestSharedPaneDividersKeepLogicalTreeAndContentGeometry(t *testing.T) {
	s := New("/tmp", 120, 35)
	tab := s.ActiveWorkspace().TabRef()
	tab.Layout.SplitFocused(layout.Vertical, 2)
	tab.Layout.SplitFocused(layout.Horizontal, 3)
	area := s.PanesArea()
	normal := tab.PaneRects(area)
	s.PaneGaps = false
	got := s.PaneRectsForDisplay(tab, area)
	for i, p := range got {
		want := normal[i].Rect
		if want.X+want.W < area.X+area.W {
			want.W++
		}
		if want.Y+want.H < area.Y+area.H {
			want.H++
		}
		if p.Rect != want {
			t.Fatal(p, want)
		}
		w, h := PaneTermSize(p.Rect, true)
		if w < 1 || h < 1 {
			t.Fatal(w, h)
		}
	}
	if normal[0].Rect != tab.Layout.Panes(area)[0].Rect {
		t.Fatal("mutated tree")
	}
	tab.Zoomed = 1
	if s.PaneRectsForDisplay(tab, area)[0].Rect != area {
		t.Fatal("zoom")
	}
	s.PaneBorders = false
	tab.Zoomed = 0
	for i, p := range s.PaneRectsForDisplay(tab, area) {
		if p.Rect != normal[i].Rect {
			t.Fatal("unframed geometry changed")
		}
	}
}
