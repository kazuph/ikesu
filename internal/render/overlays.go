package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/layout"
	"image/color"
)

// --- menu ---

// MenuSize computes the box size for a menu.
func MenuSize(m *Menu) (w, h int) {
	w = 0
	for _, it := range m.Items {
		l := len([]rune(it.Label))
		if it.Separator {
			l = 2
		}
		if l > w {
			w = l
		}
	}
	w += 4 // padding + border
	return w, len(m.Items) + 2
}

// MenuRect positions a menu inside (cols, rows).
func MenuRect(m *Menu, cols, rows int) layout.Rect {
	w, h := MenuSize(m)
	x, y := m.X, m.Y
	if x+w > cols {
		x = cols - w
	}
	if y+h > rows {
		y = rows - h
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return layout.Rect{X: x, Y: y, W: w, H: h}
}

func drawMenu(f *Frame, m *Menu, out *Output) {
	r := MenuRect(m, f.W, f.H)
	border := uv.Style{Fg: f.Palette.Dim}
	ix, iy, iw, ih := f.Box(r.X, r.Y, r.W, r.H, border)
	f.Fill(ix, iy, iw, ih, " ", uv.Style{Fg: f.Palette.Text})
	out.MenuRect = r
	out.MenuItemRows = nil
	for i, it := range m.Items {
		if i >= ih {
			break
		}
		row := layout.Rect{X: ix, Y: iy + i, W: iw, H: 1}
		out.MenuItemRows = append(out.MenuItemRows, row)
		if it.Separator {
			for xx := ix; xx < ix+iw; xx++ {
				f.Set(xx, iy+i, "─", border)
			}
			continue
		}
		sty := uv.Style{Fg: f.Palette.Text}
		if i == m.Hover || i == m.Sel {
			sty = uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface}
			f.Fill(ix, iy+i, iw, 1, " ", sty)
		}
		if it.Disabled {
			sty = uv.Style{Fg: f.Palette.Dim}
		}
		f.Text(ix+1, iy+i, it.Label, sty, ix+iw-1)
	}
}

// --- toast ---

// drawToast renders the notification box at the top-right: a 4-row-high
// bordered box with the status dot + title and one body line.
func drawToast(f *Frame, t *Toast, out *Output) {
	w := len([]rune(t.Title)) + 6
	if l := len([]rune(t.Body)) + 4; l > w {
		w = l
	}
	maxW := f.W / 2
	if maxW < 20 {
		maxW = 20
	}
	if w > maxW {
		w = maxW
	}
	x := f.W - w - 1
	if x < 0 {
		x = 0
	}
	y := 1
	r := layout.Rect{X: x, Y: y, W: w, H: 4}
	out.ToastRect = r
	dot := uv.Style{Fg: f.Palette.Accent}
	if t.Kind == "needs_attention" {
		dot = uv.Style{Fg: f.Palette.Yellow}
	}
	ix, iy, iw, _ := f.Box(r.X, r.Y, r.W, r.H, uv.Style{Fg: f.Palette.Dim})
	f.Fill(ix, iy, iw, 2, " ", uv.Style{Fg: f.Palette.Text})
	f.Set(ix, iy, "●", dot)
	f.Text(ix+2, iy, t.Title, uv.Style{Fg: f.Palette.Text, Attrs: uv.AttrBold}, ix+iw)
	f.Text(ix+1, iy+1, t.Body, uv.Style{Fg: f.Palette.Dim}, ix+iw)
}

// --- dialog ---

// DialogRects returns the outer rect and the cancel/confirm button rects.
func DialogRects(d *Dialog, cols, rows int) (outer, cancel, confirm layout.Rect) {
	w := len([]rune(d.Title)) + 4
	if l := len([]rune(d.Detail)) + 4; l > w {
		w = l
	}
	for _, ln := range d.Lines {
		if l := len([]rune(ln)) + 4; l > w {
			w = l
		}
	}
	if d.InputEdit {
		if l := len([]rune(d.Input)) + 8; l > w {
			w = l
		}
	}
	// "[ <cancel>  esc ]" + gap + "[ <confirm>  ↵ ]" = cancel+9 + 2 + confirm+7.
	btnRow := len([]rune(d.Cancel)) + len([]rune(d.Confirm)) + 18
	if btnRow > w {
		w = btnRow
	}
	if d.Width > w {
		w = d.Width
	}
	shown := len(d.Lines)
	if shown > 10 {
		shown = 10
	}
	h := 5 + shown
	x := (cols - w) / 2
	y := (rows - h) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	outer = layout.Rect{X: x, Y: y, W: w, H: h}
	by := y + h - 2
	// Buttons right-aligned: "[ <confirm>  ↵ ]" then "[ <cancel>  esc ]".
	confirmW := len([]rune(d.Confirm)) + 7
	cancelW := len([]rune(d.Cancel)) + 9
	confirm = layout.Rect{X: x + w - 1 - confirmW, Y: by, W: confirmW, H: 1}
	cancel = layout.Rect{X: confirm.X - 2 - cancelW, Y: by, W: cancelW, H: 1}
	return
}

// drawHelp renders the keybind-help overlay: a centered bordered box
// with a title, subtitle, scrollable binding rows, and a footer hint
// (SPEC G6).
func drawHelp(f *Frame, h *HelpInput, out *Output) {
	w := f.W * 3 / 4
	if w > 68 {
		w = 68
	}
	if w < 34 {
		w = f.W
	}
	if w > f.W {
		w = f.W
	}
	content := len(h.Rows) + 4 // subtitle + rows + footer inside a border
	hgt := content
	if max := f.H - 2; hgt > max {
		hgt = max
	}
	if hgt < 6 {
		hgt = 6
	}
	x := (f.W - w) / 2
	y := (f.H - hgt) / 2
	border := uv.Style{Fg: f.Palette.Dim}
	ix, iy, iw, ih := f.Box(x, y, w, hgt, border)
	f.Fill(ix, iy, iw, ih, " ", uv.Style{Fg: f.Palette.Text})
	out.HelpRect = layout.Rect{X: x, Y: y, W: w, H: hgt}

	// Title on the top border row, subtitle on the first inner row.
	title := " keybinds · scroll to explore · esc close "
	for i, r := range []rune(title) {
		if x+2+i >= x+w-1 {
			break
		}
		f.Set(x+2+i, y, string(r), uv.Style{Fg: f.Palette.Accent})
	}
	f.Text(ix+1, iy, "available commands and configured shortcuts", uv.Style{Fg: f.Palette.Dim}, ix+iw-1)

	// Scrollable content rows.
	body := ih - 2 // subtitle + footer
	if body < 1 {
		body = 1
	}
	scroll := h.Scroll
	if scroll < 0 {
		scroll = 0
	}
	if max := len(h.Rows) - body; scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	for i := 0; i < body && scroll+i < len(h.Rows); i++ {
		row := h.Rows[scroll+i]
		ry := iy + 1 + i
		if row.Group {
			f.Text(ix+1, ry, row.Cmd, uv.Style{Fg: f.Palette.Accent, Attrs: uv.AttrBold}, ix+iw-1)
			continue
		}
		f.Text(ix+2, ry, row.Bind, uv.Style{Fg: Peach}, ix+iw-1)
		f.Text(ix+20, ry, row.Cmd, uv.Style{Fg: f.Palette.Text}, ix+iw-1)
	}

	if maxScroll := len(h.Rows) - body; maxScroll > 0 {
		drawGutter(f, layout.Rect{X: ix + iw - 1, Y: iy + 1, W: 1, H: body}, &PaneSnap{ScrollN: maxScroll}, maxScroll-scroll)
	}

	// Footer hint on the last inner row.
	f.Text(ix+1, iy+ih-1, "scroll wheel ↑↓ · jump pgup/pgdn · close esc/enter", uv.Style{Fg: f.Palette.Dim}, ix+iw-1)
}

func drawDialog(f *Frame, d *Dialog, out *Output) {
	outer, cancel, confirm := DialogRects(d, f.W, f.H)
	border := uv.Style{Fg: f.Palette.Red}
	ix, iy, iw, ih := f.Box(outer.X, outer.Y, outer.W, outer.H, border)
	f.Fill(ix, iy, iw, ih, " ", uv.Style{Fg: f.Palette.Text})
	f.Text(ix+1, iy, d.Title, uv.Style{Fg: f.Palette.Text, Attrs: uv.AttrBold}, ix+iw)
	if d.InputEdit {
		f.Text(ix+1, iy+1, "> "+d.Input+" ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface}, ix+iw)
	} else {
		f.Text(ix+1, iy+1, d.Detail, uv.Style{Fg: f.Palette.Subtle}, ix+iw)
		shown := len(d.Lines)
		if shown > 10 {
			shown = 10
		}
		for i := 0; i < shown; i++ {
			f.Text(ix+1, iy+2+i, d.Lines[i], uv.Style{Fg: f.Palette.Subtle}, ix+iw)
		}
	}
	f.Text(cancel.X, cancel.Y, "[ "+d.Cancel+"  esc ]", uv.Style{Fg: f.Palette.Subtle}, cancel.X+cancel.W)
	f.Fill(confirm.X, confirm.Y, confirm.W, 1, " ", uv.Style{Fg: color.RGBA{0xff, 0xff, 0xff, 0xff}, Bg: f.Palette.Red, Attrs: uv.AttrBold})
	f.Text(confirm.X, confirm.Y, "[ "+d.Confirm+"  ↵ ]", uv.Style{Fg: color.RGBA{0xff, 0xff, 0xff, 0xff}, Bg: f.Palette.Red, Attrs: uv.AttrBold}, confirm.X+confirm.W)
	out.DialogRect = outer
	out.DialogCancel = cancel
	out.DialogConfirm = confirm
}

// --- helpers ---

func label(snap *PaneSnap) string {
	if snap == nil || snap.Label == "" {
		return "—"
	}
	return snap.Label
}

func boldIf(b bool) uint8 {
	if b {
		return uv.AttrBold
	}
	return 0
}
