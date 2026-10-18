package render

import (
	"strings"
	"testing"
)

func TestClientVersionGapAndButtons(t *testing.T) {
	for cols := 40; cols <= 160; cols++ {
		for _, notice := range []bool{false, true} {
			base := Compose(Input{State: testState(cols, 24)})
			out := ComposeClient(Input{State: testState(cols, 24)}, nil, "0.1.0", notice)
			bar := base.ActionBar
			text := screenText(t, out, cols, 24)
			left, right := bar.Copy.X+bar.Copy.W, bar.Cycle.X
			fits := !notice && bar.Area.H > 0 && bar.Copy.W > 0 && bar.Cycle.W > 0 && right-left >= 12
			if strings.Contains(text, "gtx v0.1.0") != fits || out.ActionBar != bar {
				t.Fatalf("cols=%d notice=%v: visibility or hit regions changed", cols, notice)
			}
			if !fits && screenText(t, base, cols, 24) != text {
				t.Fatalf("cols=%d: hidden version changed frame", cols)
			}
			if fits {
				x := -1
				for col := left; col < right; col++ {
					if out.Frame.Cell(col, bar.Area.Y).Content == "g" {
						x = col
						break
					}
				}
				l, r := x-left, right-x-len("gtx v0.1.0")
				if l < 1 || r < 1 || l-r > 1 || r-l > 1 {
					t.Fatalf("margins %d,%d", l, r)
				}
				if out.Frame.Cell(x, bar.Area.Y).Style.Fg != out.Frame.Palette.Dim {
					t.Fatal("version is not dim")
				}
			}
		}
	}
}

func TestClientVersionPreservesAbsentBar(t *testing.T) {
	for _, size := range [][2]int{{160, 1}, {160, 2}, {20, 24}, {160, 24}} {
		for _, empty := range []bool{false, true} {
			state := testState(size[0], size[1])
			if empty {
				state.Workspaces = nil
			}
			base := Compose(Input{State: state})
			out := ComposeClient(Input{State: state}, nil, "0.1.0", false)
			if base.ActionBar != out.ActionBar {
				t.Fatal("bar geometry changed")
			}
			if base.ActionBar.Area.H == 0 && screenText(t, base, size[0], size[1]) != screenText(t, out, size[0], size[1]) {
				t.Fatal("absent bar changed")
			}
		}
	}
}
