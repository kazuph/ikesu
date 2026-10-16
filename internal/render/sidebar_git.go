package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

type GitStatus struct {
	Branch                        string
	Ahead, Behind, Added, Deleted int
}

func drawGitRow(f *Frame, x, y, w int, g GitStatus, base uv.Style) {
	if w <= 0 {
		return
	}
	limit := x + w
	values := []struct {
		mark  string
		n     int
		style uv.Style
	}{{"↑", g.Ahead, uv.Style{Fg: f.Palette.Green, Bg: base.Bg}}, {"↓", g.Behind, uv.Style{Fg: f.Palette.Red, Bg: base.Bg}}, {"+", g.Added, uv.Style{Fg: f.Palette.Green, Bg: base.Bg}}, {"-", g.Deleted, uv.Style{Fg: f.Palette.Red, Bg: base.Bg}}}
	branch := g.Branch
	fg := f.Palette.Dim
	if branch == "" {
		branch = "nogit"
		if base.Attrs&uv.AttrBold != 0 {
			fg = f.Palette.Mauve
		}
	}
	countsWidth := 0
	for _, v := range values {
		if v.n > 0 {
			countsWidth += ansi.StringWidth(fmt.Sprintf(" %s%d", v.mark, v.n))
		}
	}
	x = f.Text(x, y, truncate(branch, max(0, w-countsWidth)), uv.Style{Fg: fg, Bg: base.Bg}, limit)
	for _, v := range values {
		if v.n > 0 {
			x = f.Text(x, y, " ", uv.Style{Fg: fg, Bg: base.Bg}, limit)
			x = f.Text(x, y, fmt.Sprintf("%s%d", v.mark, v.n), v.style, limit)
		}
	}
}
func GitLabel(g GitStatus, w int) string {
	f := NewFrame(w, 1)
	drawGitRow(f, 0, 0, w, g, uv.Style{})
	var s string
	for x := 0; x < w; x++ {
		if c := f.Cell(x, 0); c != nil && c.Width > 0 {
			s += c.Content
		}
	}
	return s
}
