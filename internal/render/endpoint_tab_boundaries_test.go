package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/model"
)

func TestEndpointTabOverflowKeepsObservedTabBoundariesAndControlTargets(t *testing.T) {
	for cols := 80; cols <= 200; cols += 10 {
		s := testState(cols, 45)
		for i := 2; i <= 18; i++ {
			s.Apply(model.CmdNewTab{Workspace: 0})
			s.ActiveWorkspace().TabRef().Name = fmt.Sprintf("TAB-%d", i)
		}
		out := Compose(Input{State: s, ServerUI: true})
		assertTabOverflowControls(t, out, cols)
		first := -1
		for i, r := range out.TabHits {
			if r.W > 0 {
				first = i
				break
			}
		}
		r := out.TabHits[first]
		if out.Frame.Cell(r.X, r.Y).Content != "…" || out.Frame.Cell(r.X+1, r.Y).Content != "·" || out.Frame.Cell(r.X+3, r.Y).Content != "T" {
			t.Fatalf("cols=%d: boundary lost icon/name", cols)
		}
	}
}

func assertTabOverflowControls(t *testing.T, out *Output, cols int) {
	t.Helper()
	if out.TabLeftRect.W != 3 || out.TabRightRect.W != 3 || out.NewTabRect.W != 3 || out.NewTabRect.X != out.TabRightRect.X+3 || out.NewTabRect.X+3 > cols {
		t.Fatalf("invalid controls at width %d: left=%+v right=%+v add=%+v", cols, out.TabLeftRect, out.TabRightRect, out.NewTabRect)
	}
	for _, r := range out.TabHits {
		if r.W > 0 && (r.X < out.TabLeftRect.X+3 || r.X+r.W > out.TabRightRect.X) {
			t.Fatal("tab hit overlaps controls", r, out.TabRightRect)
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
	for _, offset := range []int{0, 20, 29} {
		out := Compose(Input{State: s, ServerUI: true, TabScrollManual: true, TabScroll: offset})
		assertTabOverflowControls(t, out, 80)
		if out.TabScroll != offset {
			t.Fatalf("manual offset changed: %d -> %d", offset, out.TabScroll)
		}
	}
}

func TestLocalTabOverflowKeepsObservedNameAtLeftBoundary(t *testing.T) {
	s := testState(100, 25)
	for i := 0; i <= 10; i++ {
		s.Apply(model.CmdNewTab{Workspace: 0})
		s.ActiveWorkspace().TabRef().Name = fmt.Sprintf("tab-%d", i)
	}
	out := Compose(Input{State: s})
	assertTabOverflowControls(t, out, 100)
	var row strings.Builder
	for x := out.TabLeftRect.X + 3; x < out.TabRightRect.X; x++ {
		row.WriteString(out.Frame.Cell(x, 0).Content)
	}
	if !strings.HasPrefix(row.String(), "…· tab-") {
		t.Fatal("overflow lost icon or label", row.String())
	}
	if out.TabHits[s.ActiveWorkspace().ActiveTab].W == 0 {
		t.Fatal("active tab hidden")
	}
}
