package render

import "github.com/kazuph/goterrace/internal/layout"

func configWarningBounds(r layout.Rect, sidebar, cols int, out *Output) layout.Rect {
	left := sidebar
	for _, tab := range out.TabHits {
		left = max(left, tab.X+tab.W)
	}
	left = max(left, out.NewTabRect.X+out.NewTabRect.W)
	r.X = max(r.X, left+1)
	r.W = max(0, cols-r.X)
	return r
}
