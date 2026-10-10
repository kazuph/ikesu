package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

func drawTabBar(f *Frame, s *model.AppState, in Input, out *Output) {
	area := s.TabBarArea()
	ws := s.ActiveWorkspace()
	if area.W <= 0 || ws == nil {
		return
	}
	base := uv.Style{Bg: f.Palette.PopupBG}
	if in.ServerUI {
		base.Fg = f.Palette.Text
	}
	f.Fill(area.X, area.Y, area.W, 1, " ", base)
	var starts, widths []int
	var labels []string
	total := 0
	for i, t := range ws.Tabs {
		name := t.Name
		if name == "" {
			name = fmt.Sprint(i + 1)
		}
		if t.Zoomed != 0 {
			name += " Z"
		}
		marker := tabMarker(t, in.Panes)
		if in.ServerUI {
			marker = " "
		}
		width := max(8, ansi.StringWidth(name)+4)
		if marker != " " {
			width = max(8, ansi.StringWidth(name)+6)
			name = marker + name
		}
		labels = append(labels, name)
		starts = append(starts, total)
		widths = append(widths, width)
		total += widths[i]
		if i+1 < len(ws.Tabs) {
			total++
		}
	}
	buttonWidth := ansi.StringWidth(" + ")
	viewport := max(0, area.W-buttonWidth-1)
	x := area.X
	if total > viewport {
		arrowWidth := ansi.StringWidth(" < ")
		viewport = max(0, viewport-2*arrowWidth)
		x += arrowWidth
		out.TabLeftRect = layout.Rect{X: area.X, Y: area.Y, W: arrowWidth, H: 1}
		out.TabRightRect = layout.Rect{X: x + viewport, Y: area.Y, W: arrowWidth, H: 1}
	}
	offset := max(0, min(in.TabScroll, max(0, total-viewport)))
	if !in.TabScrollManual && ws.ActiveTab >= 0 && ws.ActiveTab < len(starts) {
		start, end := starts[ws.ActiveTab], starts[ws.ActiveTab]+widths[ws.ActiveTab]
		if start < offset {
			offset = start
		} else if end > offset+viewport {
			offset = max(0, end-viewport)
		}
	}
	if out.TabLeftRect.W == 0 {
		offset = 0
	}
	out.TabScroll = offset
	out.TabScrollLeft, out.TabScrollRight = offset, offset
	for _, start := range starts {
		if start < offset {
			out.TabScrollLeft = start
		}
		if start > offset && out.TabScrollRight == offset {
			out.TabScrollRight = min(start, max(0, total-viewport))
		}
	}
	strip := NewFrame(max(1, total), 1)
	strip.Palette = f.Palette
	strip.Fill(0, 0, total, 1, " ", base)
	for i, t := range ws.Tabs {
		style := uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.Surface, Attrs: uv.AttrFaint}
		if t.Name != "" {
			style.Fg, style.Attrs = f.Palette.Overlay1, 0
		}
		if i == ws.ActiveTab {
			style = uv.Style{Fg: f.Palette.SurfDim, Bg: f.Palette.Accent}
			if t.Name != "" {
				style.Attrs = uv.AttrBold
			}
		}
		label := " " + labels[i]
		strip.Fill(starts[i], 0, widths[i], 1, " ", style)
		strip.Text(starts[i], 0, label, style, starts[i]+widths[i])
		left, right := max(starts[i], offset), min(starts[i]+widths[i], offset+viewport)
		r := layout.Rect{}
		if left < right {
			r = layout.Rect{X: x + left - offset, Y: area.Y, W: right - left, H: 1}
		}
		out.TabHits = append(out.TabHits, r)
	}
	visible := min(viewport, max(0, total-offset))
	for n := 0; n < visible; n++ {
		cell := *strip.Cell(offset+n, 0)
		if (n == 0 && offset > 0) || (n == visible-1 && offset+visible < total) {
			cell.Content, cell.Width = "…", 1
		}
		if dst := f.Cell(x+n, area.Y); dst != nil {
			*dst = cell
		}
	}
	if out.TabLeftRect.W > 0 {
		for _, arrow := range []struct {
			r       layout.Rect
			text    string
			enabled bool
		}{
			{out.TabLeftRect, " < ", offset > 0}, {out.TabRightRect, " > ", offset+viewport < total},
		} {
			style := uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.Surface, Attrs: uv.AttrFaint}
			if arrow.enabled {
				style.Fg, style.Attrs = f.Palette.Overlay1, 0
			}
			f.Text(arrow.r.X, area.Y, arrow.text, style, min(area.X+area.W, arrow.r.X+arrow.r.W))
		}
		x = out.TabRightRect.X + out.TabRightRect.W
	} else {
		x += visible
	}
	if x+buttonWidth <= area.X+area.W {
		f.Text(x, area.Y, " + ", uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.PopupBG}, x+buttonWidth)
		out.NewTabRect = layout.Rect{X: x, Y: area.Y, W: buttonWidth, H: 1}
	}
}
