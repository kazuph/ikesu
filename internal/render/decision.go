package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/layout"
	"strings"
	"time"
)

type DecisionDialog struct {
	Decision     api.Decision
	Machine      string
	Index, Total int
	Selected     int
	Text         string
	Focused      bool
	Scroll       int
}
type DecisionRegions struct {
	Outer, Close, Text, Marker layout.Rect
	Options                    []layout.Rect
	ScrollRows, VisibleRows    int
}

func decisionLines(d *DecisionDialog, width int, now time.Time) []string {
	record := d.Decision
	lines := []string{record.Title, fmt.Sprintf("%s · %d/%d", d.Machine, d.Index+1, d.Total)}
	if record.ExpiresUnixMS != nil {
		lines = append(lines, fmt.Sprintf("残り %ds", max(int64(0), (*record.ExpiresUnixMS-now.UnixMilli()+999)/1000)))
	}
	if record.Body != nil {
		lines = append(lines, *record.Body)
	}
	if o := record.Origin; o != nil {
		for _, v := range []struct{ k, v string }{{"pane", o.PaneID}, {"agent", o.Agent}, {"cwd", o.Cwd}, {"branch", o.Branch}, {"command", o.Command}, {"reason", o.Reason}, {"machine", o.Machine}} {
			if v.v != "" {
				lines = append(lines, v.k+": "+v.v)
			}
		}
	}
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(ansi.Wrap(ansi.Strip(line), max(width, 1), ""), "\n")...)
	}
	return wrapped
}

// ComposeDecision uses the existing modal palette and frame primitives.
func ComposeDecision(out *Output, d *DecisionDialog, pending int, now time.Time) DecisionRegions {
	f := out.Frame
	r := DecisionRegions{}
	if pending == 0 {
		return r
	}
	marker := fmt.Sprintf("! %d", pending)
	x := out.ActionBar.Copy.X + out.ActionBar.Copy.W + 1
	y := out.ActionBar.Area.Y
	end := out.ActionBar.Cycle.X
	if end-x < ansi.StringWidth(marker) {
		x = out.ActionBar.Area.X
	}
	r.Marker = layout.Rect{X: x, Y: y, W: ansi.StringWidth(marker), H: 1}
	f.Text(x, y, marker, uv.Style{Fg: f.Palette.Yellow, Bg: f.Palette.PanelBG}, x+r.Marker.W)
	if d == nil {
		return r
	}
	w := min(56, max(0, f.W-4))
	h := min(f.H-2, max(7, len(d.Decision.Options)+7))
	if w < 8 || h < 5 {
		return r
	}
	r.Outer = layout.Rect{X: (f.W - w) / 2, Y: (f.H - h) / 2, W: w, H: h}
	border := uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG}
	ix, iy, iw, ih := f.Box(r.Outer.X, r.Outer.Y, w, h, border)
	style := uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG}
	f.Fill(ix, iy, iw, ih, " ", style)
	closeLabel := "esc close"
	r.Close = layout.Rect{X: ix + max(0, iw-ansi.StringWidth(closeLabel)), Y: iy + ih - 1, W: min(iw, ansi.StringWidth(closeLabel)), H: 1}
	f.Text(r.Close.X, r.Close.Y, closeLabel, style, ix+iw)
	optionRows := min(len(d.Decision.Options), max(0, ih-3))
	textRows := 0
	if d.Decision.AllowText {
		textRows = 1
	}
	visible := max(1, ih-optionRows-textRows-1)
	lines := decisionLines(d, iw, time.UnixMilli(now.UnixMilli()))
	r.ScrollRows = len(lines)
	r.VisibleRows = visible
	scroll := min(max(d.Scroll, 0), max(0, len(lines)-visible))
	for row := 0; row < visible && scroll+row < len(lines); row++ {
		f.Text(ix, iy+row, lines[scroll+row], style, ix+iw)
	}
	by := iy + visible
	if textRows != 0 {
		r.Text = layout.Rect{X: ix, Y: by, W: iw, H: 1}
		f.Text(ix, by, "text: "+d.Text, uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface}, ix+iw)
		by++
	}
	first := max(0, d.Selected-optionRows+1)
	for j := 0; j < optionRows; j++ {
		i := first + j
		if i >= len(d.Decision.Options) {
			break
		}
		row := layout.Rect{X: ix, Y: by + j, W: iw, H: 1}
		for len(r.Options) <= i {
			r.Options = append(r.Options, layout.Rect{})
		}
		r.Options[i] = row
		sty := style
		if i == d.Selected && d.Focused {
			sty = uv.Style{Fg: f.Palette.SurfDim, Bg: f.Palette.Accent, Attrs: uv.AttrBold}
			f.Fill(ix, by+j, iw, 1, " ", sty)
		}
		f.Text(ix, by+j, d.Decision.Options[i].Label, sty, ix+iw)
	}
	out.AnchorVisible = false
	return r
}
