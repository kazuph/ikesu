package render

import "github.com/kazuph/goterrace/internal/layout"

// TabDragIndex follows horizontal chip midpoints even below the tab bar.
// Hidden chips do not turn the visible arrows into global-end targets.
func TabDragIndex(hits []layout.Rect, x int) int {
	target := -1
	for i, r := range hits {
		if r.W <= 0 {
			continue
		}
		if x < r.X+r.W/2 {
			return i
		}
		target = i + 1
	}
	return target
}

// TabDropIndex follows the observed chip midpoint, independent of drag direction.
// Hit regions remain in model order until release.
func TabDropIndex(hits []layout.Rect, x, y int) int {
	for i, r := range hits {
		if r.W <= 0 || y != r.Y {
			continue
		}
		if x < r.X {
			if i > 0 && hits[i-1].W > 0 && x == hits[i-1].X+hits[i-1].W {
				return i
			}
			return -1
		}
		if x < r.X+r.W {
			if x < r.X+r.W/2 {
				return i
			}
			return i + 1
		}
	}
	return -1
}

// DrawTabDrop preserves the underlying cell's background and attributes, as
// herdr 0.2.50 does. The trailing slot also gets a marker at the last chip's
// right edge, so every valid insertion target has feedback.
func DrawTabDrop(out *Output, index int) {
	if index < 0 || index > len(out.TabHits) || len(out.TabHits) == 0 {
		return
	}
	r := out.TabHits[min(index, len(out.TabHits)-1)]
	if r.W <= 0 {
		return
	}
	x := r.X
	if index == len(out.TabHits) {
		x += r.W - 1
	} else if index > 0 {
		x--
	}
	if cell := out.Frame.Cell(x, r.Y); cell != nil {
		style := cell.Style
		style.Fg = out.Frame.Palette.Accent
		out.Frame.Set(x, r.Y, "│", style)
	}
}
