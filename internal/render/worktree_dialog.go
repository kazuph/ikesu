package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/layout"
	"strings"
)

func WorktreeCreateRect(cols, rows int) layout.Rect {
	w, h := min(68, max(0, cols-4)), min(12, max(0, rows-2))
	return layout.Rect{X: (cols - w) / 2, Y: (rows - h) / 2, W: w, H: h}
}

func drawWorktreeCreate(f *Frame, d *Dialog, out *Output) {
	r := WorktreeCreateRect(f.W, f.H)
	out.DialogRect = r
	f.Fill(r.X, r.Y, r.W, r.H, " ", uv.Style{Bg: f.Palette.PopupBG})
	f.Box(r.X, r.Y, r.W, r.H, uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG})
	if r.W < 4 || r.H < 12 {
		return
	}
	text := uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG}
	dim := uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}
	f.Text(r.X+1, r.Y+1, "new worktree", text, r.X+r.W-1)
	f.Text(r.X+2, r.Y+2, "branch", dim, r.X+r.W-2)
	f.Fill(r.X+2, r.Y+3, r.W-4, 1, " ", uv.Style{Bg: f.Palette.Surface})
	f.Text(r.X+2, r.Y+3, d.Input, text, r.X+r.W-2)
	f.Text(r.X+2, r.Y+4, "checkout", dim, r.X+r.W-2)
	f.Text(r.X+2, r.Y+5, d.Checkout, dim, r.X+r.W-2)
	if d.Detail != "" {
		lines := strings.Split(ansi.Wrap(" "+d.Detail, max(1, r.W-4), ""), "\n")
		for i, line := range lines {
			if i >= 3 {
				break
			}
			f.Text(r.X+1, r.Y+6+i, line, uv.Style{Fg: f.Palette.Red, Bg: f.Palette.PopupBG}, r.X+r.W-2)
		}
	}
	confirm := " ↵ create and open "
	cancel := " esc cancel "
	out.DialogConfirm, out.DialogCancel = WorktreeCreateButtons(r)
	x := out.DialogConfirm.X
	f.Text(x, r.Y+10, confirm, uv.Style{Fg: f.Palette.SurfDim, Bg: f.Palette.Accent, Attrs: uv.AttrBold}, r.X+r.W-1)
	f.Text(out.DialogCancel.X, r.Y+10, cancel, uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface, Attrs: uv.AttrBold}, r.X+r.W-1)
	out.AnchorX = min(r.X+2+ansi.StringWidth(d.Input), r.X+r.W-3)
	out.AnchorY = r.Y + 3
	f.Set(out.AnchorX, out.AnchorY, "█", text)
	out.AnchorVisible = false
}

func WorktreeCreateButtons(r layout.Rect) (confirm, cancel layout.Rect) {
	if r.W < 4 || r.H < 12 {
		return
	}
	cw, ew := ansi.StringWidth(" ↵ create and open "), ansi.StringWidth(" esc cancel ")
	x := r.X + (r.W-cw-ew-2)/2
	return layout.Rect{X: x, Y: r.Y + 10, W: cw, H: 1}, layout.Rect{X: x + cw + 2, Y: r.Y + 10, W: ew, H: 1}
}
