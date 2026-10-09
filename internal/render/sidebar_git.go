package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
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
	for _, v := range []struct {
		mark  string
		n     int
		style uv.Style
	}{{"↑", g.Ahead, uv.Style{Fg: f.Palette.Green, Bg: base.Bg}}, {"↓", g.Behind, uv.Style{Fg: f.Palette.Red, Bg: base.Bg}}, {"+", g.Added, uv.Style{Fg: f.Palette.Green, Bg: base.Bg}}, {"-", g.Deleted, uv.Style{Fg: f.Palette.Red, Bg: base.Bg}}} {
		if v.n > 0 {
			x = f.Text(x, y, fmt.Sprintf("%s%d ", v.mark, v.n), v.style, limit)
		}
	}
	branch := g.Branch
	fg := f.Palette.Dim
	if branch == "" {
		branch = "nogit"
		if base.Attrs&uv.AttrBold != 0 {
			fg = f.Palette.Mauve
		}
	}
	f.Text(x, y, truncate(branch, max(0, limit-x)), uv.Style{Fg: fg, Bg: base.Bg}, limit)
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
