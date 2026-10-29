package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/model"
)

// paneFrameRect joins the outside frame to the sidebar line without changing
// the terminal content rectangle. Both box drawing and divider joins use it.
func paneFrameRect(s *model.AppState, r model.Rect) model.Rect {
	left, right := s.SidebarArea(), s.RightSidebarArea()
	if left.W > 0 && r.X == left.X+left.W {
		r.X--
		r.W++
	}
	if right.W > 0 && r.X+r.W == right.X {
		r.W++
	}
	return r
}

// Shared borders combine the incident edges at each split intersection.
func joinPaneDividers(f *Frame, s *model.AppState) {
	if s.PaneGaps || !s.PaneBorders {
		return
	}
	type edge struct {
		mask    int
		focused bool
	}
	edges := map[[2]int]edge{}
	add := func(x, y, mask int, focused bool) {
		p := [2]int{x, y}
		v := edges[p]
		v.mask |= mask
		v.focused = v.focused || focused
		edges[p] = v
	}
	for _, p := range s.DisplayPaneRects() {
		r := p.Rect
		if r.W <= 2 || r.H <= 2 {
			continue
		}
		r = paneFrameRect(s, r)
		x2, y2 := r.X+r.W-1, r.Y+r.H-1
		for x := r.X + 1; x < x2; x++ {
			add(x, r.Y, 3, p.IsFocused)
			add(x, y2, 3, p.IsFocused)
		}
		for y := r.Y + 1; y < y2; y++ {
			add(r.X, y, 12, p.IsFocused)
			add(x2, y, 12, p.IsFocused)
		}
		add(r.X, r.Y, 10, p.IsFocused)
		add(x2, r.Y, 9, p.IsFocused)
		add(r.X, y2, 6, p.IsFocused)
		add(x2, y2, 5, p.IsFocused)
	}
	glyphs := map[int]string{3: "─", 12: "│", 10: "┌", 9: "┐", 6: "└", 5: "┘", 11: "┬", 7: "┴", 14: "├", 13: "┤", 15: "┼"}
	for p, e := range edges {
		c := f.Cell(p[0], p[1])
		if c == nil {
			continue
		}
		switch c.Content {
		case "─", "│", "┌", "┐", "└", "┘":
		default:
			continue
		}
		if glyph := glyphs[e.mask]; glyph != "" {
			style := uv.Style{Fg: f.Palette.Dim}
			if e.focused {
				style.Fg = f.Palette.Accent
			}
			f.Set(p[0], p[1], glyph, style)
		}
	}
}
