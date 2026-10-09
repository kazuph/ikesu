package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/layout"
	"image/color"
	"strings"
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
			f.Text(ix, iy+i, " --", uv.Style{Fg: f.Palette.Dim}, ix+iw)
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
	r := ToastRect(t, layout.Rect{W: f.W, H: f.H}, out.ConfigWarningRect)
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

// ToastRect anchors to the view rather than the terminal origin. Width is
// intentionally UTF-8 bytes, as required by SPEC G9.
func ToastRect(t *Toast, view, warning layout.Rect) layout.Rect {
	w := min(max(len(t.Title), len(t.Body))+6, max(0, view.W))
	margin := min(1, max(0, view.W-w))
	y := view.Y + 1
	if warning.H > 0 {
		y = max(y, warning.Y+warning.H)
	}
	return layout.Rect{X: view.X + view.W - w - margin, Y: y, W: w, H: min(4, max(0, view.Y+view.H-y))}
}

// --- dialog ---

// DialogRects returns the outer rect and the cancel/confirm button rects.
func DialogRects(d *Dialog, cols, rows int) (outer, cancel, confirm layout.Rect) {
	if d.InputEdit {
		w, h := min(56, cols), min(7, rows)
		return layout.Rect{X: (cols - w) / 2, Y: (rows - h) / 2, W: w, H: h}, layout.Rect{}, layout.Rect{}
	}
	w := len([]rune(d.Title)) + 4
	if l := len([]rune(d.Detail)) + 4; l > w {
		w = l
	}
	for _, ln := range d.Lines {
		if l := len([]rune(ln)) + 4; l > w {
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
	w = min(w, cols)
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

// HelpRect follows the same 76 by 22 cell overlay observed for settings and help.
func HelpRect(cols, rows int) layout.Rect {
	return SettingsRect(cols, rows)
}

// drawHelp renders a title, subtitle, scrollable bindings and footer.
func drawHelp(f *Frame, h *HelpInput, out *Output) {
	r := HelpRect(f.W, f.H)
	x, y, w, hgt := r.X, r.Y, r.W, r.H
	f.Fill(x, y, w, hgt, " ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG})
	ix, iy, iw, _ := f.Box(x, y, w, hgt, uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG})
	out.HelpRect = r
	f.Text(ix, iy, "keybinds", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, ix+iw)
	f.Text(x+w-11, iy, "esc close", uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}, ix+iw)
	f.Text(ix+1, iy+1, "available commands and configured shortcuts", uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}, ix+iw-1)

	// Distribution observation: title, subtitle, two blank rows, footer and borders.
	body := max(1, hgt-7)
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
		ry := iy + 3 + i
		if row.Group {
			f.Text(ix+1, ry, row.Cmd, uv.Style{Fg: f.Palette.Accent, Attrs: uv.AttrBold}, ix+iw-1)
			continue
		}
		f.Text(ix+1, ry, row.Bind, uv.Style{Fg: Peach}, ix+iw-1)
		f.Text(ix+30, ry, row.Cmd, uv.Style{Fg: f.Palette.Text}, ix+iw-1)
	}

	if maxScroll := len(h.Rows) - body; maxScroll > 0 {
		drawGutter(f, layout.Rect{X: ix + iw - 1, Y: iy + 3, W: 1, H: body}, &PaneSnap{ScrollN: maxScroll}, maxScroll-scroll)
	}

	// Footer hint on the last inner row.
	f.Text(ix+1, y+hgt-2, "scroll wheel ↑↓  ·  jump pgup / pgdn   ·  close esc / enter", uv.Style{Fg: f.Palette.Dim}, ix+iw-1)
}

func drawDialog(f *Frame, d *Dialog, out *Output) {
	if d.InputEdit {
		drawNameInput(f, d, out)
		return
	}
	outer, cancel, confirm := DialogRects(d, f.W, f.H)
	border := uv.Style{Fg: f.Palette.Red}
	ix, iy, iw, ih := f.Box(outer.X, outer.Y, outer.W, outer.H, border)
	f.Fill(ix, iy, iw, ih, " ", uv.Style{Fg: f.Palette.Text})
	f.Text(ix+1, iy, d.Title, uv.Style{Fg: f.Palette.Text, Attrs: uv.AttrBold}, ix+iw)
	f.Text(ix+1, iy+1, d.Detail, uv.Style{Fg: f.Palette.Subtle}, ix+iw)
	shown := min(len(d.Lines), 10)
	for i := 0; i < shown; i++ {
		f.Text(ix+1, iy+2+i, d.Lines[i], uv.Style{Fg: f.Palette.Subtle}, ix+iw)
	}

	f.Text(cancel.X, cancel.Y, "[ "+d.Cancel+"  esc ]", uv.Style{Fg: f.Palette.Subtle}, cancel.X+cancel.W)
	f.Fill(confirm.X, confirm.Y, confirm.W, 1, " ", uv.Style{Fg: color.RGBA{0xff, 0xff, 0xff, 0xff}, Bg: f.Palette.Red, Attrs: uv.AttrBold})
	f.Text(confirm.X, confirm.Y, "[ "+d.Confirm+"  ↵ ]", uv.Style{Fg: color.RGBA{0xff, 0xff, 0xff, 0xff}, Bg: f.Palette.Red, Attrs: uv.AttrBold}, confirm.X+confirm.W)
	out.DialogRect = outer
	out.DialogCancel = cancel
	out.DialogConfirm = confirm
}

// Distribution observations use a 56x7 name box; clicks cancel, Enter saves.
func drawNameInput(f *Frame, d *Dialog, out *Output) {
	r, _, _ := DialogRects(d, f.W, f.H)
	f.Fill(r.X, r.Y, r.W, r.H, " ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG})
	f.Box(r.X, r.Y, r.W, r.H, uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG})
	f.Text(r.X+1, r.Y+1, strings.ToLower(d.Title), uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, r.X+r.W-1)
	f.Text(r.X+2, r.Y+3, d.Input, uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface}, r.X+r.W-2)
	hint := "↵ save    ^c clear    esc cancel"
	f.Text(r.X+(r.W-ansi.StringWidth(hint))/2, r.Y+4, hint, uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}, r.X+r.W-1)
	runes := []rune(d.Input)
	prefix := string(runes[:min(max(0, d.InputCursor), len(runes))])
	out.AnchorX = min(r.X+2+ansi.StringWidth(prefix), r.X+r.W-2)
	out.AnchorY, out.AnchorVisible = r.Y+3, true
	out.DialogRect = r
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
