package render

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/model"
	"testing"
)

func TestEndpointTabOverflowKeepsObservedTabBoundariesAndControlTargets(t *testing.T) {
	for _, tc := range []struct{ cols, right, add, first int }{
		{80, 74, 77, 14}, {90, 83, 86, 14}, {100, 94, 97, 13}, {110, 104, 107, 12},
		{120, 114, 117, 11}, {130, 124, 127, 10}, {140, 134, 137, 9}, {150, 144, 147, 8},
		{160, 154, 157, 7}, {170, 164, 167, 6}, {180, 174, 177, 5}, {190, 184, 187, 4}, {200, 194, 197, 3},
	} {
		s := testState(tc.cols, 45)
		for i := 2; i <= 18; i++ {
			s.Apply(model.CmdNewTab{Workspace: 0})
			s.ActiveWorkspace().TabRef().Name = fmt.Sprintf("TAB-%d", i)
		}
		out := Compose(Input{State: s, ServerUI: true})
		if out.TabRightRect.X != tc.right || out.NewTabRect.X != tc.add {
			t.Fatalf("width %d: right=%d add=%d", tc.cols, out.TabRightRect.X, out.NewTabRect.X)
		}
		want := fmt.Sprintf("…TAB-%d", tc.first)
		got := ""
		for x := 29; x < 29+len([]rune(want)); x++ {
			got += out.Frame.Cell(x, 0).Content
		}
		if got != want {
			t.Fatalf("width %d: left edge %q want %q", tc.cols, got, want)
		}
		if out.Frame.Cell(29, 0).Style.Fg != out.Frame.Palette.Dim {
			t.Fatal("clipped tab marker must use the measured dim color")
		}
		if tc.cols == 80 && (out.Frame.Cell(73, 0).Content != "…" || out.Frame.Cell(73, 0).Style.Bg != out.Frame.Palette.Accent) {
			t.Fatal("the clipped last tab must retain its accent background and right ellipsis")
		}
		if tc.cols == 80 && out.Frame.Cell(73, 0).Style.Fg != out.Frame.Palette.SurfDim {
			t.Fatal("right clipping retains the selected tab's text color")
		}
		if tc.cols == 160 && out.Frame.Cell(153, 0).Content != "8" {
			t.Fatal("clipping only trailing padding must retain the last visible label character")
		}
	}
}

func TestEndpointTabControlsOmitTrailingGapAtClippedBoundary(t *testing.T) {
	s := testState(80, 25)
	s.ActiveWorkspace().TabRef().Name = "LONG TAB NAME"
	for i := 2; i <= 8; i++ {
		s.Apply(model.CmdNewTab{Workspace: 0})
	}
	s.ActiveWorkspace().ActiveTab = 0
	for _, offset := range []int{0, 18, 27} {
		out := Compose(Input{State: s, ServerUI: true, TabScrollManual: true, TabScroll: offset})
		if out.TabRightRect.X != 73 || out.NewTabRect.X != 76 || out.Frame.Cell(72, 0).Content != "…" {
			t.Fatalf("offset %d: right=%d add=%d marker=%q", offset, out.TabRightRect.X, out.NewTabRect.X, out.Frame.Cell(72, 0).Content)
		}
	}
}
