package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/layout"
)

func WorktreeMenuRect(m *Menu, cols, rows int) layout.Rect {
	// Height uses the original checkout count even while filtering (FIX-1).
	w, h := min(96, max(0, cols-4)), min(min(26, max(12, m.Total*2+7)), max(0, rows-2))
	return layout.Rect{X: (cols - w) / 2, Y: (rows - h) / 2, W: w, H: h}
}
func WorktreeMenuScroll(m *Menu, rows int) int {
	h := WorktreeMenuRect(m, 0, rows).H
	visible := max(0, (h-7)/2)
	offset := min(max(0, m.Scroll), max(0, len(m.Items)-visible))
	if m.Sel >= 0 && m.Sel < len(m.Items) {
		if m.Sel < offset {
			offset = m.Sel
		}
		if m.Sel >= offset+visible {
			offset = m.Sel - visible + 1
		}
	}
	return min(max(0, offset), max(0, len(m.Items)-visible))
}
func WorktreeMenuItemAt(m *Menu, cols, rows, x, y int) int {
	r := WorktreeMenuRect(m, cols, rows)
	row := y - r.Y - 4
	if x <= r.X || x >= r.X+r.W-1 || row < 0 || row >= max(0, (r.H-7)/2)*2 {
		return -1
	}
	i := WorktreeMenuScroll(m, rows) + row/2
	if i >= len(m.Items) {
		return -1
	}
	return i
}
func drawWorktreeMenu(f *Frame, m *Menu, out *Output) {
	r := WorktreeMenuRect(m, f.W, f.H)
	out.MenuRect = r
	out.MenuItemRows = make([]layout.Rect, len(m.Items))
	text := uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG}
	dim := uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}
	f.Fill(r.X, r.Y, r.W, r.H, " ", text)
	f.Box(r.X, r.Y, r.W, r.H, uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG})
	if r.W < 4 || r.H < 8 {
		return
	}
	f.Text(r.X+1, r.Y+1, "open worktree", text, r.X+r.W-1)
	filter := " / filter worktrees"
	if m.Filtering || m.Filter != "" {
		filter = " / " + m.Filter
	}
	f.Text(r.X+1, r.Y+2, filter, dim, r.X+r.W-1)
	count := fmt.Sprintf("%d checkouts", len(m.Items))
	if m.Filter != "" {
		count = fmt.Sprintf("%d/%d checkouts", len(m.Items), m.Total)
	}
	f.Text(r.X+r.W-1-ansi.StringWidth(count), r.Y+2, count, dim, r.X+r.W-1)
	f.Fill(r.X+1, r.Y+3, r.W-2, 1, "─", dim)
	m.Scroll = WorktreeMenuScroll(m, f.H)
	visible := max(0, (r.H-7)/2)
	for j := 0; j < visible && m.Scroll+j < len(m.Items); j++ {
		i := m.Scroll + j
		it := m.Items[i]
		y := r.Y + 4 + 2*j
		style := text
		marker := "  "
		if i == m.Sel {
			style = uv.Style{Fg: f.Palette.SurfDim, Bg: f.Palette.Accent, Attrs: uv.AttrBold}
			marker = "› "
			f.Fill(r.X+1, y, r.W-2, 1, " ", style)
		}
		f.Text(r.X+1, y, marker+it.Label, style, r.X+r.W-2)
		f.Text(r.X+r.W-2-ansi.StringWidth(it.Status), y, it.Status, style, r.X+r.W-1)
		f.Text(r.X+1, y+1, "  "+it.Detail, dim, r.X+r.W-2)
		out.MenuItemRows[i] = layout.Rect{X: r.X + 1, Y: y, W: r.W - 2, H: 2}
	}
	footer := " ↵ open    esc cancel "
	f.Text(r.X+(r.W-ansi.StringWidth(footer))/2, r.Y+r.H-2, footer, text, r.X+r.W-1)
}
