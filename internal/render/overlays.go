package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/layout"
	"image/color"
	"math"
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
	border := uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG}
	ix, iy, iw, ih := f.Box(r.X, r.Y, r.W, r.H, border)
	f.Fill(ix, iy, iw, ih, " ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG})
	if m.Global {
		f.Fill(ix, iy, iw, ih, " ", uv.Style{Bg: f.Palette.PopupBG})
	}
	out.MenuRect = r
	out.MenuItemRows = nil
	for i, it := range m.Items {
		if i >= ih {
			break
		}
		row := layout.Rect{X: ix, Y: iy + i, W: iw, H: 1}
		out.MenuItemRows = append(out.MenuItemRows, row)
		if it.Separator {
			if m.Global {
				f.Fill(ix, iy+i, iw, 1, "─", uv.Style{Fg: f.Palette.PanelBG, Bg: f.Palette.PopupBG})
			} else {
				f.Text(ix+1, iy+i, " --", uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}, ix+iw)
			}
			continue
		}
		sty := uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG}
		if i == m.Hover || i == m.Sel {
			sty = uv.Style{Fg: f.Palette.SurfDim, Bg: f.Palette.Accent, Attrs: uv.AttrBold}
			if !m.Global {
				f.Fill(ix, iy+i, iw, 1, " ", sty)
			}
		}
		if it.Disabled {
			sty = uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}
		}
		if m.Global {
			f.Text(ix, iy+i, " "+it.Label+" ", sty, ix+iw)
		} else {
			f.Text(ix+1, iy+i, it.Label, sty, ix+iw-1)
		}
	}
}

// --- toast ---

// drawToast renders the notification box at the top-right: a 4-row-high
// bordered box with the status dot + title and one body line.
func drawToast(f *Frame, t *Toast, out *Output) {
	r := ToastRect(t, layout.Rect{W: f.W, H: f.H}, out.ConfigWarningRect)
	out.ToastRect = r
	dot := uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG}
	if t.Kind == "needs_attention" {
		dot.Fg = f.Palette.Red
	}
	ix, iy, iw, _ := f.Box(r.X, r.Y, r.W, r.H, uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG})
	f.Fill(ix, iy, iw, 2, " ", uv.Style{Bg: f.Palette.PopupBG})
	f.Set(ix, iy, "●", dot)
	f.Text(ix+2, iy, t.Title, uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, ix+iw)
	f.Text(ix, iy+1, "  "+t.Body, uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}, ix+iw)
}

// ToastRect anchors to the view rather than the terminal origin. Width is
// intentionally UTF-8 bytes, as required by SPEC G9.
func ToastRect(t *Toast, view, warning layout.Rect) layout.Rect {
	w := min(max(len(t.Title), len(t.Body))+6, max(0, view.W))
	y := view.Y
	if warning.H > 0 {
		y = max(y, warning.Y+warning.H)
	}
	return layout.Rect{X: view.X + view.W - w, Y: y, W: w, H: min(4, max(0, view.Y+view.H-y))}
}

// --- dialog ---

// DialogRects returns the outer rect and the cancel/confirm button rects.
func DialogRects(d *Dialog, area layout.Rect) (outer, cancel, confirm layout.Rect) {
	cols, rows := area.W, area.H
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
	btnRow := ansi.StringWidth("  ↵ "+d.Confirm+"     esc "+d.Cancel+" ") + 4
	if btnRow > w {
		w = btnRow
	}
	if d.Width > w {
		w = d.Width
	}
	// The observed confirmation leaves two columns on each side of the pane area.
	w = min(w, max(cols-4, 0))
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
	outer = layout.Rect{X: area.X + x, Y: area.Y + y, W: w, H: h}
	x, y = outer.X, outer.Y
	by := y + h - 2
	confirmW := ansi.StringWidth(dialogConfirmLabel(d))
	cancelW := ansi.StringWidth(" esc " + d.Cancel + " ")
	confirm = layout.Rect{X: x + (w-confirmW-cancelW-2)/2, Y: by, W: confirmW, H: 1}
	cancel = layout.Rect{X: confirm.X + confirmW + 2, Y: by, W: cancelW, H: 1}
	return
}

func dialogConfirmLabel(d *Dialog) string {
	if d.DimPanesOnly {
		return " ↵ " + d.Confirm + " "
	}
	return "  ↵ " + d.Confirm + "   "
}

// HelpRect follows the same 76 by 22 cell overlay observed for settings and help.
func HelpRect(cols, rows int) layout.Rect {
	return SettingsRect(cols, rows)
}

// drawHelp renders a title, subtitle, scrollable bindings and footer.
func drawHelp(f *Frame, h *HelpInput, out *Output) {
	r := HelpRect(f.W, f.H)
	x, y, w, hgt := r.X, r.Y, r.W, r.H
	f.Fill(x, y, w, hgt, " ", uv.Style{Bg: f.Palette.PopupBG})
	ix, iy, iw, _ := f.Box(x, y, w, hgt, uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG})
	out.HelpRect = r
	if !HelpContentVisible(r) {
		return
	}
	f.Text(ix, iy, "keybinds", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, ix+iw)
	f.Text(x+w-12, iy, " esc close ", uv.Style{Fg: f.Palette.SurfDim, Bg: f.Palette.Accent, Attrs: uv.AttrBold}, ix+iw)
	subtitleStyle := uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.PopupBG}
	f.Fill(ix, iy+1, iw, 1, " ", subtitleStyle)
	f.Text(ix+1, iy+1, "available commands and configured shortcuts", subtitleStyle, ix+iw)

	// Distribution observation: title, subtitle, two blank rows, footer and borders.
	body := max(1, hgt-7)
	lines := HelpLines(h.Rows, iw-1, f.Palette)
	scroll := h.Scroll
	if scroll < 0 {
		scroll = 0
	}
	if max := len(lines) - body; scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	for i := 0; i < body && scroll+i < len(lines); i++ {
		row := lines[scroll+i]
		ry := iy + 3 + i
		for x, cell := range row {
			if cell.Width > 0 {
				f.Set(ix+x, ry, cell.Content, cell.Style)
			}
		}
	}

	if maxScroll := len(lines) - body; maxScroll > 0 {
		thumb, top := HelpThumb(body, len(lines), scroll)
		for i := 0; i < body; i++ {
			glyph, fg := "▕", f.Palette.Dim
			if i >= top && i < top+thumb {
				glyph, fg = "▐", f.Palette.Overlay1
			}
			f.Set(ix+iw-1, iy+3+i, glyph, uv.Style{Fg: fg, Bg: f.Palette.PopupBG})
		}
	}

	// Footer hint on the last inner row.
	footerX := ix
	for _, part := range []struct {
		text string
		fg   color.Color
	}{{" scroll ", f.Palette.Dim}, {"wheel ↑↓", f.Palette.Text},
		{"  ·  jump", f.Palette.Dim}, {" pgup / pgdn ", f.Palette.Text},
		{"  ·  close", f.Palette.Dim}, {" esc / enter ", f.Palette.Text}} {
		footerX = f.Text(footerX, y+hgt-2, part.text, uv.Style{Fg: part.fg, Bg: f.Palette.PopupBG}, ix+iw)
	}
}

// The distribution shows only the frame below 22 columns or 8 rows (help-compact.steps).
func HelpContentVisible(r layout.Rect) bool { return r.W >= 22 && r.H >= 8 }

// HelpThumb follows the observed help pages and narrow wrapped listings.
func HelpThumb(body, total, scroll int) (thumb, top int) {
	thumb = min(body, max(1, int(math.Round(float64(body*body)/float64(max(1, total))))))
	if total > body {
		top = int(math.Round(float64(scroll*(body-thumb)) / float64(total-body)))
	}
	return thumb, top
}

func drawDialog(f *Frame, d *Dialog, out *Output, area layout.Rect) {
	if d.InputEdit {
		drawNameInput(f, d, out)
		return
	}
	outer, cancel, confirm := DialogRects(d, area)
	border := uv.Style{Fg: f.Palette.Red, Bg: f.Palette.PopupBG}
	ix, iy, iw, ih := f.Box(outer.X, outer.Y, outer.W, outer.H, border)
	f.Fill(ix, iy, iw, ih, " ", uv.Style{Bg: f.Palette.PopupBG})
	f.Text(ix, iy, " "+d.Title, uv.Style{Fg: f.Palette.Red, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, ix+iw)
	f.Text(ix, iy+1, " "+d.Detail, uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}, ix+iw)
	if d.DimPanesOnly {
		if at := strings.LastIndex(d.Detail, " — "); at >= 0 {
			f.Text(ix, iy+1, " "+d.Detail[:at], uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, ix+iw)
		}
	}
	shown := min(len(d.Lines), 10)
	for i := 0; i < shown; i++ {
		if d.Lines[i] == "" {
			continue
		}
		f.Text(ix, iy+2+i, " "+d.Lines[i], uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}, ix+iw)
	}

	f.Text(cancel.X, cancel.Y, " esc "+d.Cancel+" ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface, Attrs: uv.AttrBold}, cancel.X+cancel.W)
	f.Text(confirm.X, confirm.Y, dialogConfirmLabel(d), uv.Style{Fg: f.Palette.SurfDim, Bg: f.Palette.Red, Attrs: uv.AttrBold}, confirm.X+confirm.W)
	out.DialogRect = outer
	out.DialogCancel = cancel
	out.DialogConfirm = confirm
}

// Button geometry is shared with mouse handling for the recorded name-input box.
var nameInputButtonLabels = [...]string{" ↵ save ", " ^c clear ", " esc cancel "}

func NameInputButtons(r layout.Rect) [3]layout.Rect {
	var buttons [3]layout.Rect
	width := 2 * (len(buttons) - 1)
	for _, label := range nameInputButtonLabels {
		width += ansi.StringWidth(label)
	}
	x := r.X + (r.W-width)/2
	for i, label := range nameInputButtonLabels {
		buttons[i] = layout.Rect{X: x, Y: r.Y + 4, W: ansi.StringWidth(label), H: 1}
		x += buttons[i].W + 2
	}
	return buttons
}

func drawNameInput(f *Frame, d *Dialog, out *Output) {
	r, _, _ := DialogRects(d, layout.Rect{W: f.W, H: f.H})
	f.Fill(r.X, r.Y, r.W, r.H, " ", uv.Style{Bg: f.Palette.PopupBG})
	f.Box(r.X, r.Y, r.W, r.H, uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG})
	f.Text(r.X+1, r.Y+1, strings.ToLower(d.Title), uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, r.X+r.W-1)
	f.Fill(r.X+1, r.Y+3, r.W-2, 1, " ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface})
	f.Text(r.X+2, r.Y+3, d.Input, uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface}, r.X+r.W-2)
	for i, button := range NameInputButtons(r) {
		style := uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface, Attrs: uv.AttrBold}
		if i == 0 {
			style.Fg, style.Bg = f.Palette.SurfDim, f.Palette.Accent
		}
		f.Text(button.X, button.Y, nameInputButtonLabels[i], style, min(button.X+button.W, r.X+r.W-1))
	}
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
