package render

import (
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"testing"
)

func TestPaneGapsFalseDrawsSingleConnectedDivider(t *testing.T) {
	s := model.New("/tmp", 120, 35)
	s.PaneGaps = false
	l := s.ActiveWorkspace().TabRef().Layout
	l.SplitFocused(layout.Vertical, 2)
	l.SplitFocused(layout.Horizontal, 3)
	f := Compose(Input{State: s}).Frame
	ps := s.DisplayPaneRects()
	x := ps[1].Rect.X
	y := ps[2].Rect.Y
	if f.Cell(x, s.PanesArea().Y).Content != "┬" || f.Cell(x, y).Content != "├" {
		t.Fatal(x, y, f.Cell(x, y), f.Cell(x, s.PanesArea().Y))
	}
	for row := s.PanesArea().Y + 1; row < y; row++ {
		if f.Cell(x, row).Content != "│" {
			t.Fatal(row, f.Cell(x, row))
		}
	}
	s.PaneGaps = true
	g := Compose(Input{State: s}).Frame
	if g.Cell(x-1, s.PanesArea().Y).Content != "┐" || g.Cell(x, s.PanesArea().Y).Content != "┌" {
		t.Fatal("default borders changed")
	}
}
